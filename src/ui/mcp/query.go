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
	mcpHelpers "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/helpers"
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
	mcpServer.AddTool(h.toolArchiveChat(), h.handleArchiveChat)
	mcpServer.AddTool(h.toolReactMessage(), h.handleReactMessage)
	mcpServer.AddTool(h.toolEditMessage(), h.handleEditMessage)
	mcpServer.AddTool(h.toolRevokeMessage(), h.handleRevokeMessage)
	mcpServer.AddTool(h.toolDeleteMessage(), h.handleDeleteMessage)
	mcpServer.AddTool(h.toolMarkAsRead(), h.handleMarkAsRead)
	mcpServer.AddTool(h.toolStarMessage(), h.handleStarMessage)
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
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

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
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

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
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

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
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

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

func (h *QueryHandler) toolArchiveChat() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_archive_chat",
		mcp.WithDescription("Archive or unarchive a WhatsApp chat. Archived chats are hidden from the main chat list."),
		mcp.WithTitleAnnotation("Archive/Unarchive Chat"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("chat_jid",
			mcp.Description("The chat JID (e.g., 628123456789@s.whatsapp.net or group@g.us)."),
			mcp.Required(),
		),
		mcp.WithBoolean("archived",
			mcp.Description("Set to true to archive the chat, false to unarchive it."),
			mcp.Required(),
		),
	)
}

func (h *QueryHandler) handleArchiveChat(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("missing required argument: archived")
	}

	archivedValue, ok := args["archived"]
	if !ok {
		return nil, fmt.Errorf("missing required argument: archived")
	}

	archived, err := toBool(archivedValue)
	if err != nil {
		return nil, err
	}

	req := domainChat.ArchiveChatRequest{
		ChatJID:  chatJID,
		Archived: archived,
	}

	resp, err := h.chatService.ArchiveChat(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := resp.Message
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolReactMessage() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_react_message",
		mcp.WithDescription("React to a WhatsApp message with an emoji. Send an empty string as emoji to remove an existing reaction."),
		mcp.WithTitleAnnotation("React to Message"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group JID of the chat containing the message"),
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The WhatsApp message ID to react to"),
		),
		mcp.WithString("emoji",
			mcp.Description("Emoji to react with. Pass an empty string to remove an existing reaction."),
		),
	)
}

func (h *QueryHandler) handleReactMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}
	utils.SanitizePhone(&phone)

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	emoji := request.GetString("emoji", "")

	resp, err := h.messageService.ReactMessage(ctx, domainMessage.ReactionRequest{
		MessageID: messageID,
		Phone:     phone,
		Emoji:     emoji,
	})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Reaction sent (message ID: %s)", resp.MessageID)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolEditMessage() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_edit_message",
		mcp.WithDescription("Edit a previously sent WhatsApp message. Note: only works within approximately 15 minutes of the original send (WhatsApp protocol limit)."),
		mcp.WithTitleAnnotation("Edit Message"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group JID of the chat containing the message"),
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The WhatsApp message ID to edit"),
		),
		mcp.WithString("message",
			mcp.Required(),
			mcp.Description("The new text content to replace the original message with"),
		),
	)
}

func (h *QueryHandler) handleEditMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}
	utils.SanitizePhone(&phone)

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	message, err := request.RequireString("message")
	if err != nil {
		return nil, err
	}

	resp, err := h.messageService.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: messageID,
		Phone:     phone,
		Message:   message,
	})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Message edited (ID: %s)", resp.MessageID)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolRevokeMessage() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_revoke_message",
		mcp.WithDescription("Delete a WhatsApp message for EVERYONE in the chat (destructive — cannot be undone)."),
		mcp.WithTitleAnnotation("Revoke Message (Delete for Everyone)"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group JID of the chat containing the message"),
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The WhatsApp message ID to delete for everyone"),
		),
	)
}

func (h *QueryHandler) handleRevokeMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}
	utils.SanitizePhone(&phone)

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	resp, err := h.messageService.RevokeMessage(ctx, domainMessage.RevokeRequest{
		MessageID: messageID,
		Phone:     phone,
	})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Message revoked (ID: %s)", resp.MessageID)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolDeleteMessage() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_delete_message",
		mcp.WithDescription("Delete a WhatsApp message for ME only (local delete — the other party can still see it). Destructive."),
		mcp.WithTitleAnnotation("Delete Message (For Me)"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group JID of the chat containing the message"),
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The WhatsApp message ID to delete locally"),
		),
	)
}

func (h *QueryHandler) handleDeleteMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}
	utils.SanitizePhone(&phone)

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	if err := h.messageService.DeleteMessage(ctx, domainMessage.DeleteRequest{
		MessageID: messageID,
		Phone:     phone,
	}); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Message %s deleted", messageID)), nil
}

func (h *QueryHandler) toolMarkAsRead() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_mark_as_read",
		mcp.WithDescription("Mark a WhatsApp message as read, sending a read receipt to the sender."),
		mcp.WithTitleAnnotation("Mark Message as Read"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group JID of the chat containing the message"),
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The WhatsApp message ID to mark as read"),
		),
	)
}

func (h *QueryHandler) handleMarkAsRead(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}
	utils.SanitizePhone(&phone)

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	resp, err := h.messageService.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: messageID,
		Phone:     phone,
	})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Message marked as read (ID: %s)", resp.MessageID)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *QueryHandler) toolStarMessage() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_star_message",
		mcp.WithDescription("Star or unstar a WhatsApp message."),
		mcp.WithTitleAnnotation("Star/Unstar Message"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group JID of the chat containing the message"),
		),
		mcp.WithString("message_id",
			mcp.Required(),
			mcp.Description("The WhatsApp message ID to star or unstar"),
		),
		mcp.WithBoolean("is_starred",
			mcp.Required(),
			mcp.Description("Set to true to star the message, false to unstar it"),
		),
	)
}

func (h *QueryHandler) handleStarMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}
	utils.SanitizePhone(&phone)

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("missing required argument: is_starred")
	}
	isStarredValue, ok := args["is_starred"]
	if !ok {
		return nil, fmt.Errorf("missing required argument: is_starred")
	}
	isStarred, err := toBool(isStarredValue)
	if err != nil {
		return nil, err
	}

	if err := h.messageService.StarMessage(ctx, domainMessage.StarRequest{
		MessageID: messageID,
		Phone:     phone,
		IsStarred: isStarred,
	}); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Message %s star=%t", messageID, isStarred)), nil
}
