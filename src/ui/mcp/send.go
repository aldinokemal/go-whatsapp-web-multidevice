package mcp

import (
	"context"
	"errors"
	"fmt"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
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
	mcpServer.AddTool(s.toolSendSticker(), s.handleSendSticker)
}

func (s *SendHandler) toolSendText() mcp.Tool {
	sendTextTool := mcp.NewTool("whatsapp_send_text",
		mcp.WithDescription("Send a text message to a WhatsApp contact or group."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send message to"),
		),
		mcp.WithString("message",
			mcp.Required(),
			mcp.Description("The text message to send"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
		mcp.WithString("reply_message_id",
			mcp.Description("Message ID to reply to (optional)"),
		),
	)

	return sendTextTool
}

func (s *SendHandler) handleSendText(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	res, err := s.sendService.SendText(ctx, domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Message:        message,
		ReplyMessageID: &replyMessageId,
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
