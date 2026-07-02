package mcp

import (
	"context"
	"errors"
	"fmt"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	mcpHelpers "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/helpers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type SendHandler struct {
	sendService domainSend.ISendUsecase
}

func InitMcpSend(sendService domainSend.ISendUsecase) *SendHandler {
	return &SendHandler{
		sendService: sendService,
	}
}

func (s *SendHandler) AddSendTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(s.toolSendText(), s.handleSendText)
	mcpServer.AddTool(s.toolSendContact(), s.handleSendContact)
	mcpServer.AddTool(s.toolSendLink(), s.handleSendLink)
	mcpServer.AddTool(s.toolSendLocation(), s.handleSendLocation)
	mcpServer.AddTool(s.toolSendImage(), s.handleSendImage)
	mcpServer.AddTool(s.toolSendVideo(), s.handleSendVideo)
	mcpServer.AddTool(s.toolSendSticker(), s.handleSendSticker)
	mcpServer.AddTool(s.toolSendDocument(), s.handleSendDocument)
	mcpServer.AddTool(s.toolSendAudio(), s.handleSendAudio)
	mcpServer.AddTool(s.toolSendPoll(), s.handleSendPoll)
}

func (s *SendHandler) toolSendText() mcp.Tool {
	sendTextTool := mcp.NewTool("whatsapp_send_text",
		mcp.WithDescription("Send a text message to a WhatsApp contact or group. Supports ghost mentions (mention users without showing @phone in message text)."),
		mcp.WithTitleAnnotation("Send Text Message"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send message to"),
		),
		mcp.WithString("message",
			mcp.Required(),
			mcp.Description("The text message to send."),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
		mcp.WithString("reply_message_id",
			mcp.Description("Message ID to reply to (optional)"),
		),
		mcp.WithArray("mentions",
			mcp.Description("List of phone numbers or JIDs to mention (ghost mentions - users will be notified but @phone won't appear in message text). Use \"@everyone\" to mention all group participants. Example: [\"628123456789\", \"@everyone\"]"),
		),
	)

	return sendTextTool
}

func (s *SendHandler) handleSendText(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	message, err := request.RequireString("message")
	if err != nil {
		return nil, err
	}

	replyMessageId := request.GetString("reply_message_id", "")
	mentions := request.GetStringSlice("mentions", nil)

	res, err := s.sendService.SendText(ctx, domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		Message:        message,
		ReplyMessageID: &replyMessageId,
		Mentions:       mentions,
	})

	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Message sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendContact() mcp.Tool {
	sendContactTool := mcp.NewTool("whatsapp_send_contact",
		mcp.WithDescription("Send a contact card to a WhatsApp contact or group."),
		mcp.WithTitleAnnotation("Send Contact"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send contact to"),
		),
		mcp.WithString("contact_name",
			mcp.Required(),
			mcp.Description("Name of the contact to send"),
		),
		mcp.WithString("contact_phone",
			mcp.Required(),
			mcp.Description("Phone number of the contact to send"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendContactTool
}

func (s *SendHandler) handleSendContact(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	contactName, err := request.RequireString("contact_name")
	if err != nil {
		return nil, err
	}

	contactPhone, err := request.RequireString("contact_phone")
	if err != nil {
		return nil, err
	}

	res, err := s.sendService.SendContact(ctx, domainSend.ContactRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		ContactName:  contactName,
		ContactPhone: contactPhone,
	})

	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Contact sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendLink() mcp.Tool {
	sendLinkTool := mcp.NewTool("whatsapp_send_link",
		mcp.WithDescription("Send a link with caption to a WhatsApp contact or group."),
		mcp.WithTitleAnnotation("Send Link"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send link to"),
		),
		mcp.WithString("link",
			mcp.Required(),
			mcp.Description("URL link to send"),
		),
		mcp.WithString("caption",
			mcp.Required(),
			mcp.Description("Caption or description for the link"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendLinkTool
}

func (s *SendHandler) handleSendLink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	link, err := request.RequireString("link")
	if err != nil {
		return nil, err
	}

	res, err := s.sendService.SendLink(ctx, domainSend.LinkRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		Link:    link,
		Caption: request.GetString("caption", ""),
	})

	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Link sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendLocation() mcp.Tool {
	sendLocationTool := mcp.NewTool("whatsapp_send_location",
		mcp.WithDescription("Send a location coordinates to a WhatsApp contact or group."),
		mcp.WithTitleAnnotation("Send Location"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send location to"),
		),
		mcp.WithString("latitude",
			mcp.Required(),
			mcp.Description("Latitude coordinate (as string)"),
		),
		mcp.WithString("longitude",
			mcp.Required(),
			mcp.Description("Longitude coordinate (as string)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendLocationTool
}

func (s *SendHandler) handleSendLocation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	latitude, err := request.RequireString("latitude")
	if err != nil {
		return nil, err
	}

	longitude, err := request.RequireString("longitude")
	if err != nil {
		return nil, err
	}

	res, err := s.sendService.SendLocation(ctx, domainSend.LocationRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		Latitude:  latitude,
		Longitude: longitude,
	})

	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Location sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendImage() mcp.Tool {
	sendImageTool := mcp.NewTool("whatsapp_send_image",
		mcp.WithDescription("Send an image to a WhatsApp contact or group."),
		mcp.WithTitleAnnotation("Send Image"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send image to"),
		),
		mcp.WithString("image_url",
			mcp.Description("URL of the image to send"),
		),
		mcp.WithString("caption",
			mcp.Description("Caption or description for the image"),
		),
		mcp.WithBoolean("view_once",
			mcp.Description("Whether this image should be viewed only once (default: false)"),
		),
		mcp.WithBoolean("compress",
			mcp.Description("Whether to compress the image (default: true)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendImageTool
}

func (s *SendHandler) handleSendImage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	imageURL, err := request.RequireString("image_url")
	if err != nil {
		return nil, err
	}

	imageRequest := domainSend.ImageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		Caption:  request.GetString("caption", ""),
		ViewOnce: request.GetBool("view_once", false),
		Compress: request.GetBool("compress", true),
	}

	if imageURL != "" {
		imageRequest.ImageURL = &imageURL
	}
	res, err := s.sendService.SendImage(ctx, imageRequest)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Image sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendSticker() mcp.Tool {
	sendStickerTool := mcp.NewTool("whatsapp_send_sticker",
		mcp.WithDescription("Send a sticker to a WhatsApp contact or group. Images are automatically converted to WebP sticker format."),
		mcp.WithTitleAnnotation("Send Sticker"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send sticker to"),
		),
		mcp.WithString("sticker_url",
			mcp.Description("URL of the image to convert to sticker and send"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this is a forwarded sticker"),
		),
	)

	return sendStickerTool
}

func (s *SendHandler) handleSendSticker(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	stickerURL := request.GetString("sticker_url", "")
	if stickerURL == "" {
		return nil, errors.New("sticker_url must be a non-empty string")
	}

	stickerRequest := domainSend.StickerRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		StickerURL: &stickerURL,
	}

	res, err := s.sendService.SendSticker(ctx, stickerRequest)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Sticker sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendVideo() mcp.Tool {
	sendVideoTool := mcp.NewTool("whatsapp_send_video",
		mcp.WithDescription("Send a video to a WhatsApp contact or group via a video URL (fetched server-side). Supports mp4/mkv/avi, max 30MB."),
		mcp.WithTitleAnnotation("Send Video"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send video to"),
		),
		mcp.WithString("video_url",
			mcp.Required(),
			mcp.Description("URL of the video to send (mp4/mkv/avi). The server downloads it before sending."),
		),
		mcp.WithString("caption",
			mcp.Description("Caption or description for the video"),
		),
		mcp.WithBoolean("view_once",
			mcp.Description("Whether this video should be viewed only once (default: false)"),
		),
		mcp.WithBoolean("gif_playback",
			mcp.Description("Whether to play the video as a looping GIF (default: false)"),
		),
		mcp.WithBoolean("compress",
			mcp.Description("Whether to re-encode/compress the video before sending (default: false)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendVideoTool
}

func (s *SendHandler) handleSendVideo(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	videoURL, err := request.RequireString("video_url")
	if err != nil {
		return nil, err
	}
	if videoURL == "" {
		return nil, errors.New("video_url must be a non-empty string")
	}

	videoRequest := domainSend.VideoRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		Caption:     request.GetString("caption", ""),
		ViewOnce:    request.GetBool("view_once", false),
		GifPlayback: request.GetBool("gif_playback", false),
		Compress:    request.GetBool("compress", false),
		VideoURL:    &videoURL,
	}

	res, err := s.sendService.SendVideo(ctx, videoRequest)
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Video sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendDocument() mcp.Tool {
	return mcp.NewTool("whatsapp_send_document",
		mcp.WithDescription("Send a document/file to a WhatsApp contact or group via a URL fetched server-side. The MIME type and filename are derived server-side from the file URL."),
		mcp.WithTitleAnnotation("Send Document"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send the document to"),
		),
		mcp.WithString("file_url",
			mcp.Required(),
			mcp.Description("URL of the file to send. The server downloads and determines the MIME type and filename from this URL."),
		),
		mcp.WithString("caption",
			mcp.Description("Optional caption for the document"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)
}

func (s *SendHandler) handleSendDocument(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	fileURL, err := request.RequireString("file_url")
	if err != nil {
		return nil, err
	}
	if fileURL == "" {
		return nil, errors.New("file_url must be a non-empty string")
	}

	res, err := s.sendService.SendFile(ctx, domainSend.FileRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		FileURL: &fileURL,
		Caption: request.GetString("caption", ""),
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Document sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendAudio() mcp.Tool {
	return mcp.NewTool("whatsapp_send_audio",
		mcp.WithDescription("Send an audio file to a WhatsApp contact or group via a URL fetched server-side."),
		mcp.WithTitleAnnotation("Send Audio"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send the audio to"),
		),
		mcp.WithString("audio_url",
			mcp.Required(),
			mcp.Description("URL of the audio file to send. The server downloads and sends it."),
		),
		mcp.WithBoolean("ptt",
			mcp.Description("Send as a voice note (PTT). Requires ffmpeg server-side; transcodes to ogg/opus. (default: false)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)
}

func (s *SendHandler) handleSendAudio(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	audioURL, err := request.RequireString("audio_url")
	if err != nil {
		return nil, err
	}
	if audioURL == "" {
		return nil, errors.New("audio_url must be a non-empty string")
	}

	res, err := s.sendService.SendAudio(ctx, domainSend.AudioRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: request.GetBool("is_forwarded", false),
		},
		AudioURL: &audioURL,
		PTT:      request.GetBool("ptt", false),
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Audio sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendPoll() mcp.Tool {
	return mcp.NewTool("whatsapp_send_poll",
		mcp.WithDescription("Send a poll to a WhatsApp contact or group. Requires at least 2 options."),
		mcp.WithTitleAnnotation("Send Poll"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send the poll to"),
		),
		mcp.WithString("question",
			mcp.Required(),
			mcp.Description("The poll question"),
		),
		mcp.WithArray("options",
			mcp.Required(),
			mcp.Description("List of poll option strings (min 2). Example: [\"Option A\", \"Option B\", \"Option C\"]"),
		),
		mcp.WithNumber("max_answer",
			mcp.Description("Maximum number of options a recipient can select (default: 1 for single-choice)"),
		),
	)
}

func (s *SendHandler) handleSendPoll(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	question, err := request.RequireString("question")
	if err != nil {
		return nil, err
	}

	// RequireStringSlice rejects non-string entries with an indexed error
	// instead of silently dropping them.
	options, err := request.RequireStringSlice("options")
	if err != nil {
		return nil, err
	}
	if len(options) < 2 {
		return nil, errors.New("options must contain at least 2 items")
	}

	res, err := s.sendService.SendPoll(ctx, domainSend.PollRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone: phone,
		},
		Question:  question,
		Options:   options,
		MaxAnswer: request.GetInt("max_answer", 1),
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Poll sent successfully with ID %s", res.MessageID)), nil
}
