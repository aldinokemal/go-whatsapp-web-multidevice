package usecase

import (
	"context"
	"fmt"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type extractMediaFunc func(ctx context.Context, client *whatsmeow.Client, storageLocation string, mediaFile whatsmeow.DownloadableMessage) (utils.ExtractedMedia, error)

type requestMediaReuploadFunc func(ctx context.Context, client *whatsmeow.Client, info types.MessageInfo, mediaKey []byte, timeout time.Duration) (string, error)

type fetchReuploadedMediaFunc func(ctx context.Context, client *whatsmeow.Client, directPath string, mediaFile whatsmeow.DownloadableMessage, storageLocation string) (utils.ReuploadedMedia, error)

func (service serviceMessage) extractMedia(ctx context.Context, client *whatsmeow.Client, storageLocation string, mediaFile whatsmeow.DownloadableMessage) (utils.ExtractedMedia, error) {
	if service.extractMediaFn != nil {
		return service.extractMediaFn(ctx, client, storageLocation, mediaFile)
	}
	return utils.ExtractMedia(ctx, client, storageLocation, mediaFile)
}

// buildStoredDownloadableMessage builds the whatsmeow media message for a
// stored row, downloading from directPath.
func buildStoredDownloadableMessage(message *domainChatStorage.Message, directPath string) (whatsmeow.DownloadableMessage, error) {
	downloadableMsg, err := utils.BuildDownloadableMessage(
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
		return nil, fmt.Errorf("unsupported media type: %s", message.MediaType)
	}
	return downloadableMsg, nil
}

// recoverExpiredMedia re-requests media that expired on WhatsApp's servers
// from the phone; see whatsapp.ExpiredMediaRecovery.
func (service serviceMessage) recoverExpiredMedia(ctx context.Context, client *whatsmeow.Client, message *domainChatStorage.Message, storageLocation string, downloadErr error) (utils.ExtractedMedia, error) {
	return whatsapp.ExpiredMediaRecovery{
		Repo:            service.chatStorageRepo,
		RequestReupload: service.requestMediaReuploadFn,
		FetchReuploaded: service.fetchReuploadedMediaFn,
	}.Recover(ctx, client, message, storageLocation, downloadErr)
}
