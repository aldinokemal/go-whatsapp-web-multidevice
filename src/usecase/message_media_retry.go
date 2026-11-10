package usecase

import (
	"context"
	"fmt"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// downloadReuploadedMedia recovers media that expired on WhatsApp's servers by
// asking the phone to re-upload it, then downloads it from the new direct
// path. The new path is stored so later downloads skip the round trip. When
// recovery fails, the original download error is kept and the reason added.
func (service serviceMessage) downloadReuploadedMedia(ctx context.Context, client *whatsmeow.Client, message *domainChatStorage.Message, storageLocation string, downloadErr error) (utils.ExtractedMedia, error) {
	info, err := mediaRetryMessageInfo(message)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("%w; cannot request re-upload: %v", downloadErr, err)
	}

	logrus.Infof("Media of message %s expired on the server (%v); asking the phone to re-upload it", message.ID, downloadErr)
	directPath, err := whatsapp.RequestMediaReupload(ctx, client, info, message.MediaKey, whatsapp.DefaultMediaRetryTimeout)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("%w; re-upload request failed: %v", downloadErr, err)
	}

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
		return utils.ExtractedMedia{}, fmt.Errorf("unsupported media type: %s", message.MediaType)
	}

	extractedMedia, err := utils.ExtractMedia(ctx, client, storageLocation, downloadableMsg)
	if err != nil && utils.IsMediaHashMismatch(err) {
		// The phone re-uploaded a copy that differs from the file first sent.
		// Accept it only if it is authenticated by the message's media key.
		logrus.Warnf("Re-uploaded media of message %s does not match the original hashes (%v); accepting it after HMAC validation", message.ID, err)
		extractedMedia, err = utils.DownloadReuploadedMedia(ctx, directPath, downloadableMsg, storageLocation)
	}
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("download after re-upload failed: %w", err)
	}

	if service.chatStorageRepo != nil {
		updated := *message
		updated.DirectPath = directPath
		if storeErr := service.chatStorageRepo.StoreMessage(&updated); storeErr != nil {
			logrus.Warnf("Downloaded re-uploaded media of message %s but could not store its new direct path: %v", message.ID, storeErr)
		}
	}
	return extractedMedia, nil
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
