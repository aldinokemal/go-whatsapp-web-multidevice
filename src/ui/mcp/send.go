package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type SendHandler struct {
	sendService domainSend.ISendUsecase
	resolver    deviceResolver
}

func InitMcpSend(sendService domainSend.ISendUsecase, resolver deviceResolver) *SendHandler {
	return &SendHandler{sendService: sendService, resolver: resolver}
}

func (s *SendHandler) AddSendTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_send",
		mcpg.WithDescription("Send a WhatsApp message. The `type` field selects what to send: text, image, video, audio, document, sticker, location, contact, poll, link, or forward an existing message."),
		mcpg.WithTitleAnnotation("Send WhatsApp Message"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(false),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithRawInputSchema(json.RawMessage(sendSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, s.handleSend)
}

func (s *SendHandler) handleSend(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, s.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	msgType, err := request.RequireString("type")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	phone, err := request.RequireString("phone")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	base := domainSend.BaseRequest{
		Phone:       phone,
		IsForwarded: request.GetBool("is_forwarded", false),
	}

	var res domainSend.GenericResponse
	switch msgType {
	case "text":
		replyID := request.GetString("reply_message_id", "")
		res, err = s.sendService.SendText(ctx, domainSend.MessageRequest{
			BaseRequest:    base,
			Message:        request.GetString("message", ""),
			ReplyMessageID: &replyID,
			Mentions:       request.GetStringSlice("mentions", nil),
		})
	case "image":
		imageURL := request.GetString("image_url", "")
		res, err = s.sendService.SendImage(ctx, domainSend.ImageRequest{
			BaseRequest: base,
			ImageURL:    &imageURL,
			Caption:     request.GetString("caption", ""),
			ViewOnce:    request.GetBool("view_once", false),
			Compress:    request.GetBool("compress", true),
			HD:          request.GetBool("hd", false),
		})
	case "video":
		videoURL := request.GetString("video_url", "")
		res, err = s.sendService.SendVideo(ctx, domainSend.VideoRequest{
			BaseRequest: base,
			VideoURL:    &videoURL,
			Caption:     request.GetString("caption", ""),
			ViewOnce:    request.GetBool("view_once", false),
			GifPlayback: request.GetBool("gif_playback", false),
			Compress:    request.GetBool("compress", false),
			HD:          request.GetBool("hd", false),
		})
	case "audio":
		audioURL := request.GetString("audio_url", "")
		res, err = s.sendService.SendAudio(ctx, domainSend.AudioRequest{
			BaseRequest: base,
			AudioURL:    &audioURL,
			PTT:         request.GetBool("ptt", false),
		})
	case "document":
		fileURL := request.GetString("file_url", "")
		res, err = s.sendService.SendFile(ctx, domainSend.FileRequest{
			BaseRequest: base,
			FileURL:     &fileURL,
			Caption:     request.GetString("caption", ""),
		})
	case "sticker":
		stickerURL := request.GetString("sticker_url", "")
		res, err = s.sendService.SendSticker(ctx, domainSend.StickerRequest{
			BaseRequest: base,
			StickerURL:  &stickerURL,
		})
	case "location":
		res, err = s.sendService.SendLocation(ctx, domainSend.LocationRequest{
			BaseRequest: base,
			Latitude:    request.GetString("latitude", ""),
			Longitude:   request.GetString("longitude", ""),
		})
	case "contact":
		res, err = s.sendService.SendContact(ctx, domainSend.ContactRequest{
			BaseRequest:  base,
			ContactName:  request.GetString("contact_name", ""),
			ContactPhone: request.GetString("contact_phone", ""),
		})
	case "poll":
		res, err = s.sendService.SendPoll(ctx, domainSend.PollRequest{
			BaseRequest: base,
			Question:    request.GetString("question", ""),
			Options:     request.GetStringSlice("options", nil),
			MaxAnswer:   request.GetInt("max_answer", 1),
		})
	case "link":
		res, err = s.sendService.SendLink(ctx, domainSend.LinkRequest{
			BaseRequest: base,
			Link:        request.GetString("link", ""),
			Caption:     request.GetString("caption", ""),
		})
	case "forward":
		forwardReq := domainSend.ForwardRequest{
			MessageID:     request.GetString("message_id", ""),
			Phone:         phone,
			ForceReupload: request.GetBool("force_reupload", false),
		}
		if args := request.GetArguments(); args != nil {
			if _, ok := args["duration"]; ok {
				duration := request.GetInt("duration", 0)
				forwardReq.Duration = &duration
			}
		}
		res, err = s.sendService.SendForward(ctx, forwardReq)
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown send type: %s", msgType)), nil
	}

	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return mcpg.NewToolResultText(fmt.Sprintf("%s sent successfully with ID %s", msgType, res.MessageID)), nil
}
