package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

type serviceNewsletter struct{}

func NewNewsletterService() domainNewsletter.INewsletterUsecase {
	return &serviceNewsletter{}
}

func (service serviceNewsletter) Unfollow(ctx context.Context, request domainNewsletter.UnfollowRequest) (err error) {
	if err = validations.ValidateUnfollowNewsletter(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	JID, err := utils.ValidateJidWithLogin(client, request.NewsletterID)
	if err != nil {
		return err
	}

	return client.UnfollowNewsletter(ctx, JID)
}

func (service serviceNewsletter) GetMessages(ctx context.Context, request domainNewsletter.GetMessagesRequest) (response domainNewsletter.GetMessagesResponse, err error) {
	if err = validations.ValidateGetNewsletterMessages(ctx, &request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	JID, err := utils.ValidateJidWithLogin(client, request.NewsletterID)
	if err != nil {
		return response, err
	}

	params := &whatsmeow.GetNewsletterMessagesParams{
		Count: request.Count,
	}
	if request.Before != 0 {
		params.Before = types.MessageServerID(request.Before)
	}

	messages, err := client.GetNewsletterMessages(ctx, JID, params)
	if err != nil {
		return response, err
	}

	response.Data = make([]domainNewsletter.Message, 0, len(messages))
	for _, msg := range messages {
		response.Data = append(response.Data, domainNewsletter.Message{
			ServerID:       int(msg.MessageServerID),
			MessageID:      string(msg.MessageID),
			Type:           msg.Type,
			Timestamp:      msg.Timestamp.Format(time.RFC3339),
			ViewsCount:     msg.ViewsCount,
			ReactionCounts: msg.ReactionCounts,
			Text:           utils.ExtractMessageTextFromProto(msg.Message),
		})
	}

	return response, nil
}

func (service serviceNewsletter) DownloadMedia(ctx context.Context, request domainNewsletter.DownloadMediaRequest) (response domainNewsletter.DownloadMediaResponse, err error) {
	if err = validations.ValidateDownloadNewsletterMedia(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	JID, err := utils.ValidateJidWithLogin(client, request.NewsletterID)
	if err != nil {
		return response, err
	}

	messages, err := client.GetNewsletterMessages(ctx, JID, &whatsmeow.GetNewsletterMessagesParams{
		Count:  1,
		Before: types.MessageServerID(request.ServerID + 1),
	})
	if err != nil {
		return response, fmt.Errorf("failed to get newsletter message: %w", err)
	}

	message, err := findNewsletterMessage(messages, request.ServerID)
	if err != nil {
		return response, err
	}

	downloadable, mediaType, err := newsletterDownloadableMedia(message.Message)
	if err != nil {
		return response, fmt.Errorf("newsletter message %d does not contain downloadable media", request.ServerID)
	}

	newsletterDir := utils.ExtractPhoneNumber(JID.String())
	dateDir := filepath.Join(config.PathMedia, newsletterDir, message.Timestamp.Format("2006-01-02"))
	if err = os.MkdirAll(dateDir, 0755); err != nil {
		return response, fmt.Errorf("failed to create media directory: %w", err)
	}

	extractedMedia, err := utils.ExtractMedia(ctx, client, dateDir, downloadable)
	if err != nil {
		return response, fmt.Errorf("failed to download newsletter media: %w", err)
	}

	fileInfo, statErr := os.Stat(extractedMedia.MediaPath)
	if statErr != nil {
		logrus.Warnf("Could not get file size for %s: %v", extractedMedia.MediaPath, statErr)
	}

	response.ServerID = request.ServerID
	response.MessageID = string(message.MessageID)
	response.Status = fmt.Sprintf("Media downloaded successfully to %s", extractedMedia.MediaPath)
	response.MediaType = mediaType
	response.MimeType = extractedMedia.MimeType
	response.Filename = filepath.Base(extractedMedia.MediaPath)
	response.FilePath = extractedMedia.MediaPath
	if fileInfo != nil {
		response.FileSize = fileInfo.Size()
	}

	return response, nil
}

func findNewsletterMessage(messages []*types.NewsletterMessage, serverID int) (*types.NewsletterMessage, error) {
	for _, message := range messages {
		if message != nil && int(message.MessageServerID) == serverID {
			return message, nil
		}
	}

	return nil, fmt.Errorf("newsletter message with server ID %d not found", serverID)
}

func newsletterDownloadableMedia(message *waE2E.Message) (whatsmeow.DownloadableMessage, string, error) {
	if message != nil {
		switch {
		case message.GetImageMessage() != nil:
			return message.GetImageMessage(), "image", nil
		case message.GetVideoMessage() != nil:
			return message.GetVideoMessage(), "video", nil
		case message.GetPtvMessage() != nil:
			return message.GetPtvMessage(), "video_note", nil
		case message.GetAudioMessage() != nil:
			return message.GetAudioMessage(), "audio", nil
		case message.GetDocumentMessage() != nil:
			return message.GetDocumentMessage(), "document", nil
		case message.GetStickerMessage() != nil:
			return message.GetStickerMessage(), "sticker", nil
		}
	}

	return nil, "", fmt.Errorf("newsletter message does not contain downloadable media")
}
