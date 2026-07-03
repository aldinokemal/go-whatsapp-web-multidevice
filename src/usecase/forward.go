package usecase

import (
	"context"
	"fmt"
	"os"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func (service serviceSend) SendForward(ctx context.Context, request domainSend.ForwardRequest) (response domainSend.GenericResponse, err error) {
	if err = validations.ValidateForwardMessage(ctx, request); err != nil {
		return response, err
	}

	message, err := service.chatStorageRepo.GetMessageByIDAndDevice(deviceIDFromContext(ctx), request.MessageID)
	if err != nil {
		return response, fmt.Errorf("failed to load message %s: %w", request.MessageID, err)
	}
	if message == nil {
		return response, fmt.Errorf("message with ID %s not found", request.MessageID)
	}

	if !utils.IsForwardableStorageMessage(message) {
		return response, pkgError.ValidationError(utils.ErrUnsupportedForwardType)
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	opts := utils.ForwardBuildOptions{Duration: forwardDurationOption(service, request)}
	content := forwardStoredContent(message)

	var msg *waE2E.Message
	if request.ForceReupload && utils.IsForwardMediaMessage(message) {
		msg, err = service.reuploadForwardMessage(ctx, client, message, dataWaRecipient, opts)
	} else {
		msg, err = utils.BuildForwardMessageFromStorage(message, opts)
	}
	if err != nil {
		return response, err
	}

	ts, sendErr := service.wrapSendMessage(ctx, client, dataWaRecipient, msg, content)
	if sendErr != nil && utils.IsForwardMediaMessage(message) && !request.ForceReupload {
		reuploadMsg, reuploadErr := service.reuploadForwardMessage(ctx, client, message, dataWaRecipient, opts)
		if reuploadErr != nil {
			return response, sendErr
		}
		ts, sendErr = service.wrapSendMessage(ctx, client, dataWaRecipient, reuploadMsg, content)
	}
	if sendErr != nil {
		return response, sendErr
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Message forwarded to %s (server timestamp: %s)", request.Phone, ts.Timestamp.String())
	return response, nil
}

func forwardDurationOption(service serviceSend, request domainSend.ForwardRequest) *int {
	if request.Duration != nil {
		return request.Duration
	}
	expiration := service.getDefaultEphemeralExpiration(request.Phone)
	if expiration == 0 {
		return nil
	}
	d := int(expiration)
	return &d
}

func forwardStoredContent(message *domainChatStorage.Message) string {
	if message.Content != "" {
		return message.Content
	}
	switch message.MediaType {
	case "image":
		return "🖼️ Image"
	case "video", "video_note":
		return "🎬 Video"
	case "audio", "ptt":
		return "🎵 Audio"
	case "document":
		return "📄 Document"
	case "sticker":
		return "🎨 Sticker"
	default:
		return message.Content
	}
}

func (service serviceSend) reuploadForwardMessage(ctx context.Context, client *whatsmeow.Client, message *domainChatStorage.Message, recipient types.JID, opts utils.ForwardBuildOptions) (*waE2E.Message, error) {
	directPath := utils.ResolveMediaDirectPath(message.DirectPath, message.URL)
	downloadable, err := utils.BuildDownloadableMessage(
		message.MediaType,
		message.URL,
		directPath,
		message.Filename,
		message.MediaKey,
		message.FileSHA256,
		message.FileEncSHA256,
		message.FileLength,
	)
	if err != nil {
		return nil, err
	}

	extracted, err := utils.ExtractMedia(ctx, client, config.PathSendItems, downloadable)
	if err != nil {
		return nil, fmt.Errorf("failed to download media for re-upload: %w", err)
	}
	defer os.Remove(extracted.MediaPath)

	data, err := os.ReadFile(extracted.MediaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read downloaded media: %w", err)
	}

	waMediaType, err := forwardWhatsmeowMediaType(message.MediaType)
	if err != nil {
		return nil, err
	}

	uploaded, err := service.uploadMedia(ctx, client, waMediaType, data, recipient)
	if err != nil {
		return nil, pkgError.WaUploadMediaError(fmt.Sprintf("failed to re-upload media: %v", err))
	}

	opts.Upload = &uploaded
	opts.MimeType = extracted.MimeType
	return utils.BuildForwardMessageFromStorage(message, opts)
}

func forwardWhatsmeowMediaType(mediaType string) (whatsmeow.MediaType, error) {
	switch mediaType {
	case "image", "sticker":
		return whatsmeow.MediaImage, nil
	case "video", "video_note":
		return whatsmeow.MediaVideo, nil
	case "audio", "ptt":
		return whatsmeow.MediaAudio, nil
	case "document":
		return whatsmeow.MediaDocument, nil
	default:
		return "", fmt.Errorf("unsupported media type: %s", mediaType)
	}
}
