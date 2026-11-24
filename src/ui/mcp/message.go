package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

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

// maxInlineAudioBytes bounds what download_media will base64-inline for a voice note or audio
// file. Generous relative to a typical WhatsApp voice note (usually tens of KB of OGG/Opus) so
// an ordinary one always qualifies, bounded so this stays a reasonable MCP payload.
const maxInlineAudioBytes = 16 * 1024 * 1024

// audioMimeTypes names the handful of codecs a WhatsApp voice note or audio file actually
// arrives in. mime.TypeByExtension (used for images) is not reused here: it depends on the
// host OS's registered types, and audio/ogg in particular is not reliably one of them.
var audioMimeTypes = map[string]string{
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/ogg",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".wav":  "audio/wav",
}

// audioMimeTypeFor takes the media's own mediaType alongside the downloaded filename: a
// real voice note has been seen to download with a generated name carrying no extension at
// all (WhatsApp's own AudioMessage.Mimetype can be empty, and pkg/utils's own ExtractMedia
// has nothing else to derive one from for a non-document message), so the extension lookup
// alone is not reliable.
//
// mediaType is never actually "ptt" in practice, confirmed by the live re-test that caught
// this: pkg/utils's ExtractMediaInfo is the only place that reads AudioMessage.GetPTT(), and
// both branches return the identical hardcoded string "audio" regardless of it - "ptt" is
// handled defensively throughout this codebase (forwarding, Chatwoot sync) as if it could
// occur, but nothing ever produces it. A genuine WhatsApp AudioMessage - "ptt" or a regular
// audio attachment alike, as distinct from a DocumentMessage, which keeps its own format and
// extension - is always Opus-in-Ogg: WhatsApp transcodes both into the same container, which
// is also why ExtractMediaInfo itself hardcodes the "ogg" extension for every AudioMessage it
// sees. So when no extension is available this falls back to audio/ogg for either value,
// rather than leaving a real voice note mislabelled application/octet-stream.
func audioMimeTypeFor(mediaType, filename string) string {
	if mt, ok := audioMimeTypes[strings.ToLower(filepath.Ext(filename))]; ok {
		return mt
	}
	if mediaType == "ptt" || mediaType == "audio" {
		return "audio/ogg"
	}
	return "application/octet-stream"
}

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
		// A voice note ("ptt") or audio file is the same problem as an image:
		// an MCP client has no ears. This server does no speech-to-text of
		// its own - no third-party API key, no model bundled onto a modest
		// instance that already serves everything else - so the audio bytes
		// themselves are the payload, inlined the same way an image is.
		// Whatever transcription capability the connected agent has (a local
		// Whisper install, another tool, or none) is its own to use.
		if (resp.MediaType == "ptt" || resp.MediaType == "audio") && resp.FileSize > 0 && resp.FileSize <= maxInlineAudioBytes {
			if audioBytes, readErr := os.ReadFile(resp.FilePath); readErr == nil {
				note := fmt.Sprintf(
					"%s\n\nThe audio is attached inline. Nothing transcribed it - if you have a way to turn speech into text (a local Whisper install, another tool), use it on these bytes; otherwise ask the sender to type the gist, or save the file and listen to it yourself.",
					fallback,
				)
				result := mcpg.NewToolResultAudio(note, base64.StdEncoding.EncodeToString(audioBytes), audioMimeTypeFor(resp.MediaType, resp.Filename))
				result.StructuredContent = resp
				return result, nil
			}
		}
		return mcpg.NewToolResultStructured(resp, fallback), nil
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown message action: %s", action)), nil
	}
}
