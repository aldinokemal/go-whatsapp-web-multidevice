package whatsapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// ErrMediaRetrySkipped is returned by BulkMediaRecovery once the phone has
// left a re-upload request of the same run unanswered.
var ErrMediaRetrySkipped = errors.New("the phone left an earlier re-upload request unanswered; not asking again in this run")

// ExpiredMediaRecovery recovers media that expired on WhatsApp's servers by
// asking the phone to re-upload it, then downloads it once from the new direct
// path through whatsmeow's media hosts. The phone can re-upload a slightly
// different copy than the file first sent, so the download is authenticated
// with the message's media key rather than the original hashes, and the new
// location and hashes are stored so later downloads take the normal path.
//
// Recover waits for the phone for up to DefaultMediaRetryTimeout. The answer
// arrives on the device event loop, so Recover must not be called from an
// event handler: it would block the loop that delivers the answer.
type ExpiredMediaRecovery struct {
	// Repo stores the recovered location and hashes; nil skips storing.
	Repo domainChatStorage.IChatStorageRepository

	// Seams; nil uses RequestMediaReupload and utils.DownloadReuploadedMedia.
	RequestReupload func(ctx context.Context, client *whatsmeow.Client, info types.MessageInfo, mediaKey []byte, timeout time.Duration) (string, error)
	FetchReuploaded func(ctx context.Context, client *whatsmeow.Client, directPath string, mediaFile whatsmeow.DownloadableMessage, storageLocation string) (utils.ReuploadedMedia, error)
}

// Recover re-requests the media of a stored message after downloadErr, an
// expired-media download error (see utils.IsExpiredMediaError), and stores the
// file in storageLocation. Nothing is stored unless the download succeeds, and
// every failure keeps downloadErr in the chain.
func (r ExpiredMediaRecovery) Recover(ctx context.Context, client *whatsmeow.Client, message *domainChatStorage.Message, storageLocation string, downloadErr error) (utils.ExtractedMedia, error) {
	info, err := mediaRetryMessageInfo(message)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("cannot request re-upload: %w; original download error: %w", err, downloadErr)
	}

	requestReupload := r.RequestReupload
	if requestReupload == nil {
		requestReupload = RequestMediaReupload
	}
	logrus.Infof("Media of message %s expired on the server (%v); asking the phone to re-upload it", message.ID, downloadErr)
	directPath, err := requestReupload(ctx, client, info, message.MediaKey, DefaultMediaRetryTimeout)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("re-upload request failed: %w; original download error: %w", err, downloadErr)
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
		return utils.ExtractedMedia{}, fmt.Errorf("%w; original download error: %w", err, downloadErr)
	}
	fetchReuploaded := r.FetchReuploaded
	if fetchReuploaded == nil {
		fetchReuploaded = utils.DownloadReuploadedMedia
	}
	reuploaded, err := fetchReuploaded(ctx, client, directPath, downloadableMsg, storageLocation)
	if err != nil {
		return utils.ExtractedMedia{}, fmt.Errorf("download after re-upload failed: %w; original download error: %w", err, downloadErr)
	}
	if !bytes.Equal(reuploaded.FileEncSHA256, message.FileEncSHA256) {
		logrus.Warnf("Phone re-uploaded a copy of message %s that differs from the original file; accepted after HMAC validation", message.ID)
	}

	r.storeRecoveredMedia(message, domainChatStorage.MessageMedia{
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
func (r ExpiredMediaRecovery) storeRecoveredMedia(message *domainChatStorage.Message, media domainChatStorage.MessageMedia) {
	if r.Repo == nil {
		return
	}
	if err := r.Repo.UpdateMessageMedia(message.DeviceID, message.ChatJID, message.ID, media); err != nil {
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
			// whatsmeow treats broadcast chats (including status@broadcast)
			// like groups: the phone needs the author to find the message.
			IsGroup: chat.Server == types.GroupServer || chat.Server == types.BroadcastServer,
		},
	}
	if info.IsGroup {
		sender, err := types.ParseJID(message.Sender)
		if err != nil || sender.User == "" {
			return types.MessageInfo{}, fmt.Errorf("group or broadcast message %s has no valid sender", message.ID)
		}
		info.Sender = sender.ToNonAD()
	}
	return info, nil
}

// BulkMediaRecovery is an ExpiredMediaRecovery for one run over many stored
// messages, such as a history import. A phone that is offline would hold every
// expired file for the full DefaultMediaRetryTimeout, so the first request the
// phone leaves unanswered ends recovery for the rest of the run. Requests the
// phone does answer return quickly, also when it no longer has the file, and
// do not end it.
type BulkMediaRecovery struct {
	ExpiredMediaRecovery
	phoneUnanswered atomic.Bool
}

// NewBulkMediaRecovery returns the recovery for one run; use a new one per run.
func NewBulkMediaRecovery(repo domainChatStorage.IChatStorageRepository) *BulkMediaRecovery {
	return &BulkMediaRecovery{ExpiredMediaRecovery: ExpiredMediaRecovery{Repo: repo}}
}

// Recover is ExpiredMediaRecovery.Recover until the phone leaves a request
// unanswered; after that it fails at once with ErrMediaRetrySkipped.
func (r *BulkMediaRecovery) Recover(ctx context.Context, client *whatsmeow.Client, message *domainChatStorage.Message, storageLocation string, downloadErr error) (utils.ExtractedMedia, error) {
	if r.phoneUnanswered.Load() {
		return utils.ExtractedMedia{}, fmt.Errorf("%w; original download error: %w", ErrMediaRetrySkipped, downloadErr)
	}
	extracted, err := r.ExpiredMediaRecovery.Recover(ctx, client, message, storageLocation, downloadErr)
	if err != nil {
		logrus.Warnf("Could not recover the expired media of message %s: %v", message.ID, err)
	}
	if errors.Is(err, ErrMediaRetryTimeout) && r.phoneUnanswered.CompareAndSwap(false, true) {
		logrus.Warn("The phone is not answering re-upload requests; expired media is not re-requested for the rest of this run")
	}
	return extracted, err
}
