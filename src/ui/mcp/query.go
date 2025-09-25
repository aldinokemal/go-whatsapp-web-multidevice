package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type QueryHandler struct {
	chatService    domainChat.IChatUsecase
	userService    domainUser.IUserUsecase
	messageService domainMessage.IMessageUsecase
}

func InitMcpQuery(chatService domainChat.IChatUsecase, userService domainUser.IUserUsecase, messageService domainMessage.IMessageUsecase) *QueryHandler {
	return &QueryHandler{
		chatService:    chatService,
		userService:    userService,
		messageService: messageService,
	}
}

func (h *QueryHandler) AddQueryTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolListContacts(), h.handleListContacts)
	mcpServer.AddTool(h.toolListChats(), h.handleListChats)
	mcpServer.AddTool(h.toolGetChatMessages(), h.handleGetChatMessages)
	mcpServer.AddTool(h.toolDownloadMedia(), h.handleDownloadMedia)
}

func (h *QueryHandler) toolListContacts() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_list_contacts",
		mcp.WithDescription("Retrieve all contacts available in the connected WhatsApp account."),
		mcp.WithTitleAnnotation("List Contacts"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
}

func (h *QueryHandler) handleListContacts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resp, err := h.userService.MyListContacts(ctx)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Found %d contacts", len(resp.Data))
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolListChats() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_list_chats",
		mcp.WithDescription("Retrieve recent chats with optional pagination and search filters."),
		mcp.WithTitleAnnotation("List Chats"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of chats to return (default 25, max 100)."),
			mcp.DefaultNumber(25),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of chats to skip from the start (default 0)."),
			mcp.DefaultNumber(0),
		),
		mcp.WithString("search",
			mcp.Description("Filter chats whose name contains this text."),
		),
		mcp.WithBoolean("has_media",
			mcp.Description("If true, return only chats that contain media messages."),
			mcp.DefaultBool(false),
		),
	)
}

func (h *QueryHandler) handleListChats(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var hasMedia bool
	args := request.GetArguments()
	if args != nil {
		if value, ok := args["has_media"]; ok {
			parsed, err := toBool(value)
			if err != nil {
				return nil, err
			}
			hasMedia = parsed
		}
	}

	req := domainChat.ListChatsRequest{
		Limit:    request.GetInt("limit", 25),
		Offset:   request.GetInt("offset", 0),
		Search:   request.GetString("search", ""),
		HasMedia: hasMedia,
	}

	resp, err := h.chatService.ListChats(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf(
		"Retrieved %d chats (offset %d, limit %d)",
		len(resp.Data),
		req.Offset,
		req.Limit,
	)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolGetChatMessages() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_get_chat_messages",
		mcp.WithDescription("Fetch messages from a specific chat, with optional pagination, search, and time filters."),
		mcp.WithTitleAnnotation("Get Chat Messages"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("chat_jid",
			mcp.Description("The chat JID (e.g., 628123456789@s.whatsapp.net or group@g.us)."),
			mcp.Required(),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of messages to return (default 50, max 100)."),
			mcp.DefaultNumber(50),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of messages to skip from the start (default 0)."),
			mcp.DefaultNumber(0),
		),
		mcp.WithString("start_time",
			mcp.Description("Filter messages sent after this RFC3339 timestamp."),
		),
		mcp.WithString("end_time",
			mcp.Description("Filter messages sent before this RFC3339 timestamp."),
		),
		mcp.WithBoolean("media_only",
			mcp.Description("If true, return only messages containing media."),
			mcp.DefaultBool(false),
		),
		mcp.WithBoolean("is_from_me",
			mcp.Description("If provided, filter messages sent by you (true) or others (false)."),
		),
		mcp.WithString("search",
			mcp.Description("Full-text search within the chat history (case-insensitive)."),
		),
	)
}

func (h *QueryHandler) handleGetChatMessages(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()

	var startTimePtr *string
	startTime := strings.TrimSpace(request.GetString("start_time", ""))
	if startTime != "" {
		startTimePtr = &startTime
	}

	var endTimePtr *string
	endTime := strings.TrimSpace(request.GetString("end_time", ""))
	if endTime != "" {
		endTimePtr = &endTime
	}

	mediaOnly := false
	if args != nil {
		if value, ok := args["media_only"]; ok {
			parsed, err := toBool(value)
			if err != nil {
				return nil, err
			}
			mediaOnly = parsed
		}
	}

	var isFromMePtr *bool
	if args != nil {
		if value, ok := args["is_from_me"]; ok {
			parsed, err := toBool(value)
			if err != nil {
				return nil, err
			}
			isFromMePtr = &parsed
		}
	}

	req := domainChat.GetChatMessagesRequest{
		ChatJID:   chatJID,
		Limit:     request.GetInt("limit", 50),
		Offset:    request.GetInt("offset", 0),
		StartTime: startTimePtr,
		EndTime:   endTimePtr,
		MediaOnly: mediaOnly,
		IsFromMe:  isFromMePtr,
		Search:    request.GetString("search", ""),
	}

	resp, err := h.chatService.GetChatMessages(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf(
		"Retrieved %d messages from %s",
		len(resp.Data),
		chatJID,
	)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolDownloadMedia() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_download_message_media",
		mcp.WithDescription("Download media associated with a specific message and return the local file path."),
		mcp.WithTitleAnnotation("Download Message Media"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("message_id",
			mcp.Description("The WhatsApp message ID that contains the media."),
			mcp.Required(),
		),
		mcp.WithString("phone",
			mcp.Description("The target chat phone number or JID associated with the message."),
			mcp.Required(),
		),
	)
}

func (h *QueryHandler) handleDownloadMedia(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	utils.SanitizePhone(&phone)

	req := domainMessage.DownloadMediaRequest{
		MessageID: messageID,
		Phone:     phone,
	}

	resp, err := h.messageService.DownloadMedia(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Media saved to %s (%s)", resp.FilePath, resp.MediaType)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func toBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("unable to parse boolean value %q", v)
		}
		return parsed, nil
	case float64:
		return v != 0, nil
	case int:
		return v != 0, nil
	default:
		return false, fmt.Errorf("unsupported boolean value type %T", value)
	}
}
