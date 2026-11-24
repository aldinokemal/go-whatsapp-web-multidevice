package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// maxInlineImageBytes bounds what download_media will base64-inline into a tool result. Above
// this, the structured response (file_path/file_url) is returned alone - an MCP client that can
// reach the owner-scoped /files/ URL some other way (e.g. a browser session) still has it; one
// that cannot is told the media exists rather than handed a multi-megabyte base64 blob.
const maxInlineImageBytes = 5 * 1024 * 1024

type MessageHandler struct {
	messageService domainMessage.IMessageUsecase
	resolver       deviceResolver
}

func InitMcpMessage(messageService domainMessage.IMessageUsecase, resolver deviceResolver) *MessageHandler {
	return &MessageHandler{messageService: messageService, resolver: resolver}
}

func (h *MessageHandler) AddMessageTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_message",
		mcpg.WithDescription("Operate on an existing WhatsApp message: react, edit, revoke (delete for everyone), delete (for me), mark_read, mark_played, star, unstar, or download_media."),
		mcpg.WithTitleAnnotation("Message Operations"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(true),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithRawInputSchema(json.RawMessage(messageSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handleMessage)
}

func (h *MessageHandler) handleMessage(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	phone, err := request.RequireString("phone")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	utils.SanitizePhone(&phone)
	messageID, err := request.RequireString("message_id")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	switch action {
	case "react":
		resp, err := h.messageService.ReactMessage(ctx, domainMessage.ReactionRequest{
			MessageID: messageID, Phone: phone, Emoji: request.GetString("emoji", ""),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Reaction sent (message ID: %s)", resp.MessageID)), nil
	case "edit":
		resp, err := h.messageService.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
			MessageID: messageID, Phone: phone, Message: request.GetString("message", ""),
			Link: request.GetString("link", ""),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Message edited (ID: %s)", resp.MessageID)), nil
	case "revoke":
		resp, err := h.messageService.RevokeMessage(ctx, domainMessage.RevokeRequest{MessageID: messageID, Phone: phone})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Message revoked (ID: %s)", resp.MessageID)), nil
	case "delete":
		if err := h.messageService.DeleteMessage(ctx, domainMessage.DeleteRequest{MessageID: messageID, Phone: phone}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Message %s deleted", messageID)), nil
	case "mark_read":
		resp, err := h.messageService.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{MessageID: messageID, Phone: phone})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Message marked as read (ID: %s)", resp.MessageID)), nil
	case "mark_played":
		resp, err := h.messageService.MarkAsPlayed(ctx, domainMessage.MarkAsPlayedRequest{MessageID: messageID, Phone: phone})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Message marked as played (ID: %s)", resp.MessageID)), nil
	case "star", "unstar":
		isStarred := action == "star"
		if err := h.messageService.StarMessage(ctx, domainMessage.StarRequest{
			MessageID: messageID, Phone: phone, IsStarred: isStarred,
		}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Message %s star=%t", messageID, isStarred)), nil
	case "download_media":
		resp, err := h.messageService.DownloadMedia(ctx, domainMessage.DownloadMediaRequest{MessageID: messageID, Phone: phone})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		fallback := fmt.Sprintf("Media saved to %s (%s)", resp.FilePath, resp.MediaType)
		// An image is handed back inline (base64, same pattern login_qr already uses for its QR
		// code), not just as a fetchable file_url: that URL sits behind this same owner-scoped
		// auth (files.Mount's own doc comment), which an MCP client has no way to attach when it
		// tries to fetch the link itself - the one real path into the bytes is this same
		// authenticated call, so it has to carry them. Capped and type-checked so a large video or
		// an unrecognised type still falls back to the structured response alone.
		if resp.MediaType == "image" && resp.FileSize > 0 && resp.FileSize <= maxInlineImageBytes {
			if imgBytes, readErr := os.ReadFile(resp.FilePath); readErr == nil {
				mimeType := mime.TypeByExtension(filepath.Ext(resp.Filename))
				if mimeType == "" {
					mimeType = "application/octet-stream"
				}
				result := mcpg.NewToolResultImage(fallback, base64.StdEncoding.EncodeToString(imgBytes), mimeType)
				result.StructuredContent = resp
				return result, nil
			}
		}
		return mcpg.NewToolResultStructured(resp, fallback), nil
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown message action: %s", action)), nil
	}
}
