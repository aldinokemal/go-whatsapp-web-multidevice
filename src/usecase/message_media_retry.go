package usecase

import (
	"bytes"
	"context"
	"fmt"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
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

func (service serviceMessage) requestMediaReupload(ctx context.Context, client *whatsmeow.Client, info types.MessageInfo, mediaKey []byte, timeout time.Duration) (string, error) {
	if service.requestMediaReuploadFn != nil {
		return service.requestMediaReuploadFn(ctx, client, info, mediaKey, timeout)
	}
	return whatsapp.RequestMediaReupload(ctx, client, info, mediaKey, timeout)
}

func (service serviceMessage) fetchReuploadedMedia(ctx context.Context, client *whatsmeow.Client, directPath string, mediaFile whatsmeow.DownloadableMessage, storageLocation string) (utils.ReuploadedMedia, error) {
	if service.fetchReuploadedMediaFn != nil {
		return service.fetchReuploadedMediaFn(ctx, client, directPath, mediaFile, storageLocation)
	}
	return utils.DownloadReuploadedMedia(ctx, client, directPath, mediaFile, storageLocation)
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

// recoverExpiredMedia recovers media that expired on WhatsApp's servers by
// asking the phone to re-upload it, then downloads it once from the new direct
// path through whatsmeow's media hosts. The phone can re-upload a slightly
// different copy than the file first sent, so the download is authenticated
// with the message's media key rather than the original hashes, and the new
// location and hashes are stored so later downloads take the normal path.
// Nothing is stored unless the download succeeds, and every failure keeps the
// original download error in the chain.
func (service serviceMessage) recoverExpiredMedia(ctx context.Context, client *whatsmeow.Client, message *domainChatStorage.Message, storageLocation string, downloadErr error) (utils.ExtractedMedia, error) {
	info, err := mediaRetryMessageInfo(message)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("cannot request re-upload: %w; original download error: %w", err, downloadErr)
	}

	logrus.Infof("Media of message %s expired on the server (%v); asking the phone to re-upload it", message.ID, downloadErr)
	directPath, err := service.requestMediaReupload(ctx, client, info, message.MediaKey, whatsapp.DefaultMediaRetryTimeout)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("re-upload request failed: %w; original download error: %w", err, downloadErr)
	}

	downloadableMsg, err := buildStoredDownloadableMessage(message, directPath)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("%w; original download error: %w", err, downloadErr)
	}
	reuploaded, err := service.fetchReuploadedMedia(ctx, client, directPath, downloadableMsg, storageLocation)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("download after re-upload failed: %w; original download error: %w", err, downloadErr)
	}
	if !bytes.Equal(reuploaded.FileEncSHA256, message.FileEncSHA256) {
		logrus.Warnf("Phone re-uploaded a copy of message %s that differs from the original file; accepted after HMAC validation", message.ID)
	}

	service.storeRecoveredMedia(message, domainChatStorage.MessageMedia{
		URL:           reuploadedMediaURL(directPath),
		DirectPath:    directPath,
		FileSHA256:    reuploaded.FileSHA256,
		FileEncSHA256: reuploaded.FileEncSHA256,
		FileLength:    reuploaded.FileLength,
	})
	return reuploaded.ExtractedMedia, nil
}

// reuploadedMediaURL is the canonical CDN URL for a direct path, in the same
// form WhatsApp clients store, so consumers that need a URL keep working.
func reuploadedMediaURL(directPath string) string {
	return "https://mmg.whatsapp.net" + directPath
}

// storeRecoveredMedia records a recovered file's location and hashes on the
// device's own row without touching content or other fields.
func (service serviceMessage) storeRecoveredMedia(message *domainChatStorage.Message, media domainChatStorage.MessageMedia) {
	if service.chatStorageRepo == nil {
		return
	}
	if err := service.chatStorageRepo.UpdateMessageMedia(message.DeviceID, message.ChatJID, message.ID, media); err != nil {
		logrus.Warnf("Could not store the re-uploaded media location of message %s: %v", message.ID, err)
	}
}

// mediaRetryMessageInfo rebuilds the message identity the phone needs to find
// the original message: its ID, chat, direction and, in groups, the sender.
func mediaRetryMessageInfo(message *domainChatStorage.Message) (types.MessageInfo, error) {
	if message == nil {
		return types.MessageInfo{}, fmt.Errorf("message is missing")
	}
	chat, err := types.ParseJID(message.ChatJID)
	if err != nil || chat.User == "" {
		return types.MessageInfo{}, fmt.Errorf("invalid chat JID %q", message.ChatJID)
	}

	info := types.MessageInfo{
		ID: message.ID,
		MessageSource: types.MessageSource{
			Chat:     chat.ToNonAD(),
			IsFromMe: message.IsFromMe,
			IsGroup:  chat.Server == types.GroupServer,
		},
	}
	if info.IsGroup {
		sender, err := types.ParseJID(message.Sender)
		if err != nil || sender.User == "" {
			return types.MessageInfo{}, fmt.Errorf("group message %s has no valid sender", message.ID)
		}
		info.Sender = sender.ToNonAD()
	}
	return info, nil
}
