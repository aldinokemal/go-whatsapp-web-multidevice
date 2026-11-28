package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type ChatHandler struct {
	chatService domainChat.IChatUsecase
	userService domainUser.IUserUsecase
	resolver    deviceResolver
}

func InitMcpChat(chatService domainChat.IChatUsecase, userService domainUser.IUserUsecase, resolver deviceResolver) *ChatHandler {
	return &ChatHandler{chatService: chatService, userService: userService, resolver: resolver}
}

func (h *ChatHandler) AddChatTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_chat",
		mcpg.WithDescription("Query and manage WhatsApp chats and contacts: list_chats, list_contacts, get_messages (chat history with filters), archive/unarchive, pin/unpin, mute/unmute, set_disappearing (disappearing-message timer), or request_history (ask the phone to sync older messages of a chat)."),
		mcpg.WithTitleAnnotation("Chat Management"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(true),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithRawInputSchema(json.RawMessage(chatSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handleChat)
}

func (h *ChatHandler) handleChat(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	// Same as whatsapp_message/whatsapp_group: a bare number gets @s.whatsapp.net, a bare ID over 15 digits gets @g.us.
	chatJID := request.GetString("chat_jid", "")
	utils.SanitizePhone(&chatJID)

	switch action {
	case "list_chats":
		req := domainChat.ListChatsRequest{
			Limit:    request.GetInt("limit", 25),
			Offset:   request.GetInt("offset", 0),
			Search:   request.GetString("search", ""),
			HasMedia: request.GetBool("has_media", false),
		}
		resp, err := h.chatService.ListChats(ctx, req)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "list_contacts":
		resp, err := h.userService.MyListContacts(ctx)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "get_messages":
		var startTimePtr, endTimePtr *string
		if v := strings.TrimSpace(request.GetString("start_time", "")); v != "" {
			startTimePtr = &v
		}
		if v := strings.TrimSpace(request.GetString("end_time", "")); v != "" {
			endTimePtr = &v
		}
		var isFromMePtr *bool
		if args := request.GetArguments(); args != nil {
			if _, ok := args["is_from_me"]; ok {
				v := request.GetBool("is_from_me", false)
				isFromMePtr = &v
			}
		}
		req := domainChat.GetChatMessagesRequest{
			ChatJID:   chatJID,
			Limit:     request.GetInt("limit", 50),
			Offset:    request.GetInt("offset", 0),
			StartTime: startTimePtr,
			EndTime:   endTimePtr,
			MediaOnly: request.GetBool("media_only", false),
			IsFromMe:  isFromMePtr,
			Search:    request.GetString("search", ""),
		}
		resp, err := h.chatService.GetChatMessages(ctx, req)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "archive":
		req := domainChat.ArchiveChatRequest{
			ChatJID:  chatJID,
			Archived: request.GetBool("archived", false),
		}
		resp, err := h.chatService.ArchiveChat(ctx, req)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, resp.Message), nil
	case "pin":
		resp, err := h.chatService.PinChat(ctx, domainChat.PinChatRequest{
			ChatJID: chatJID,
			Pinned:  request.GetBool("pinned", false),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, resp.Message), nil
	case "mute":
		var muted *bool
		if args := request.GetArguments(); args != nil {
			if _, ok := args["muted"]; ok {
				v := request.GetBool("muted", false)
				muted = &v
			}
		}
		resp, err := h.chatService.MuteChat(ctx, domainChat.MuteChatRequest{
			ChatJID:         chatJID,
			Muted:           muted,
			DurationSeconds: int64(request.GetInt("duration_seconds", 0)),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, resp.Message), nil
	case "set_disappearing":
		// Check the range before converting: 4294967296 would wrap to 0 and silently disable the timer.
		timer := request.GetInt("timer_seconds", 0)
		if timer < 0 || int64(timer) > math.MaxUint32 {
			return mcpg.NewToolResultError("timer_seconds must be one of: 0 (off), 86400 (24h), 604800 (7d), 7776000 (90d)"), nil
		}
		resp, err := h.chatService.SetDisappearingTimer(ctx, domainChat.SetDisappearingTimerRequest{
			ChatJID:      chatJID,
			TimerSeconds: uint32(timer),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, resp.Message), nil
	case "request_history":
		resp, err := h.chatService.RequestChatHistory(ctx, domainChat.RequestChatHistoryRequest{
			ChatJID: chatJID,
			Count:   request.GetInt("count", 0),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, "History sync requested successfully"), nil
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown chat action: %s", action)), nil
	}
}
