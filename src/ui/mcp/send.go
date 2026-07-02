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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	message, ok := request.GetArguments()["message"].(string)
	if !ok {
		return nil, errors.New("message must be a string")
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	replyMessageId, ok := request.GetArguments()["reply_message_id"].(string)
	if !ok {
		replyMessageId = ""
	}

	// Parse mentions array (ghost mentions)
	var mentions []string
	if mentionsRaw, ok := request.GetArguments()["mentions"].([]any); ok {
		for _, m := range mentionsRaw {
			if mentionStr, ok := m.(string); ok {
				mentions = append(mentions, mentionStr)
			}
		}
	}

	res, err := s.sendService.SendText(ctx, domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	contactName, ok := request.GetArguments()["contact_name"].(string)
	if !ok {
		return nil, errors.New("contact_name must be a string")
	}

	contactPhone, ok := request.GetArguments()["contact_phone"].(string)
	if !ok {
		return nil, errors.New("contact_phone must be a string")
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendContact(ctx, domainSend.ContactRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	link, ok := request.GetArguments()["link"].(string)
	if !ok {
		return nil, errors.New("link must be a string")
	}

	caption, ok := request.GetArguments()["caption"].(string)
	if !ok {
		caption = ""
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendLink(ctx, domainSend.LinkRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Link:    link,
		Caption: caption,
	})

	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Link sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendLocation() mcp.Tool {
	sendLocationTool := mcp.NewTool("whatsapp_send_location",
		mcp.WithDescription("Send a location coordinates to a WhatsApp contact or group."),
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	latitude, ok := request.GetArguments()["latitude"].(string)
	if !ok {
		return nil, errors.New("latitude must be a string")
	}

	longitude, ok := request.GetArguments()["longitude"].(string)
	if !ok {
		return nil, errors.New("longitude must be a string")
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendLocation(ctx, domainSend.LocationRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	imageURL, imageURLOk := request.GetArguments()["image_url"].(string)
	if !imageURLOk {
		return nil, errors.New("image_url must be a string")
	}

	caption, ok := request.GetArguments()["caption"].(string)
	if !ok {
		caption = ""
	}

	viewOnce, ok := request.GetArguments()["view_once"].(bool)
	if !ok {
		viewOnce = false
	}

	compress, ok := request.GetArguments()["compress"].(bool)
	if !ok {
		compress = true
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	// Create image request
	imageRequest := domainSend.ImageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Caption:  caption,
		ViewOnce: viewOnce,
		Compress: compress,
	}

	if imageURLOk && imageURL != "" {
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	stickerURL, stickerURLOk := request.GetArguments()["sticker_url"].(string)
	if !stickerURLOk || stickerURL == "" {
		return nil, errors.New("sticker_url must be a non-empty string")
	}

	isForwarded := false
	if val, ok := request.GetArguments()["is_forwarded"].(bool); ok {
		isForwarded = val
	}

	stickerRequest := domainSend.StickerRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	videoURL, videoURLOk := request.GetArguments()["video_url"].(string)
	if !videoURLOk || videoURL == "" {
		return nil, errors.New("video_url must be a non-empty string")
	}

	caption, ok := request.GetArguments()["caption"].(string)
	if !ok {
		caption = ""
	}

	viewOnce, ok := request.GetArguments()["view_once"].(bool)
	if !ok {
		viewOnce = false
	}

	gifPlayback, ok := request.GetArguments()["gif_playback"].(bool)
	if !ok {
		gifPlayback = false
	}

	compress, ok := request.GetArguments()["compress"].(bool)
	if !ok {
		compress = false
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	// Create video request (URL-sourced, mirrors send_image)
	videoRequest := domainSend.VideoRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Caption:     caption,
		ViewOnce:    viewOnce,
		GifPlayback: gifPlayback,
		Compress:    compress,
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	fileURL, ok := request.GetArguments()["file_url"].(string)
	if !ok || fileURL == "" {
		return nil, errors.New("file_url must be a non-empty string")
	}

	caption, ok := request.GetArguments()["caption"].(string)
	if !ok {
		caption = ""
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendFile(ctx, domainSend.FileRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		FileURL: &fileURL,
		Caption: caption,
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Document sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendAudio() mcp.Tool {
	return mcp.NewTool("whatsapp_send_audio",
		mcp.WithDescription("Send an audio file to a WhatsApp contact or group via a URL fetched server-side."),
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	audioURL, ok := request.GetArguments()["audio_url"].(string)
	if !ok || audioURL == "" {
		return nil, errors.New("audio_url must be a non-empty string")
	}

	ptt, ok := request.GetArguments()["ptt"].(bool)
	if !ok {
		ptt = false
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendAudio(ctx, domainSend.AudioRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		AudioURL: &audioURL,
		PTT:      ptt,
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Audio sent successfully with ID %s", res.MessageID)), nil
}

func (s *SendHandler) toolSendPoll() mcp.Tool {
	return mcp.NewTool("whatsapp_send_poll",
		mcp.WithDescription("Send a poll to a WhatsApp contact or group. Requires at least 2 options."),
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

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	question, ok := request.GetArguments()["question"].(string)
	if !ok {
		return nil, errors.New("question must be a string")
	}

	var options []string
	if optionsRaw, ok := request.GetArguments()["options"].([]any); ok {
		for _, o := range optionsRaw {
			if optStr, ok := o.(string); ok {
				options = append(options, optStr)
			}
		}
	}
	if len(options) < 2 {
		return nil, errors.New("options must contain at least 2 items")
	}

	maxAnswer := request.GetInt("max_answer", 1)

	res, err := s.sendService.SendPoll(ctx, domainSend.PollRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone: phone,
		},
		Question:  question,
		Options:   options,
		MaxAnswer: maxAnswer,
	})
	if err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Poll sent successfully with ID %s", res.MessageID)), nil
}
