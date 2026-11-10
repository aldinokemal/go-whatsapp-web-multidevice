package usecase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func TestMediaRetryMessageInfoDirectChat(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000AA",
		ChatJID: "5511888888888@s.whatsapp.net",
		Sender:  "5511888888888@s.whatsapp.net",
	})

	require.NoError(t, err)
	require.Equal(t, types.MessageID("3A0000000000000000AA"), info.ID)
	require.Equal(t, types.NewJID("5511888888888", types.DefaultUserServer), info.Chat)
	require.False(t, info.IsGroup)
	require.False(t, info.IsFromMe)
	require.True(t, info.Sender.IsEmpty(), "direct chats do not send a participant")
}

func TestMediaRetryMessageInfoGroupCarriesSender(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:       "3A0000000000000000BB",
		ChatJID:  "120363000000000001@g.us",
		Sender:   "123456789012345:7@lid",
		IsFromMe: true,
	})

	require.NoError(t, err)
	require.True(t, info.IsGroup)
	require.True(t, info.IsFromMe)
	require.Equal(t, types.NewJID("123456789012345", types.HiddenUserServer), info.Sender)
}

func TestMediaRetryMessageInfoStatusCarriesAuthor(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000DD",
		ChatJID: "status@broadcast",
		Sender:  "5511888888888@s.whatsapp.net",
	})

	require.NoError(t, err)
	require.True(t, info.IsGroup, "whatsmeow sends the participant for broadcast messages only when IsGroup is set")
	require.Equal(t, types.NewJID("5511888888888", types.DefaultUserServer), info.Sender)
}

func TestMediaRetryMessageInfoGroupWithoutSenderFails(t *testing.T) {
	_, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000CC",
		ChatJID: "120363000000000001@g.us",
	})

	require.ErrorContains(t, err, "no valid sender")
}

func TestMediaRetryMessageInfoInvalidChatFails(t *testing.T) {
	_, err := mediaRetryMessageInfo(&domainChatStorage.Message{ID: "X", ChatJID: ""})

	require.ErrorContains(t, err, "invalid chat JID")
}

// fakeMediaCDN plays the media CDN and the phone for DownloadMedia: the
// original path has expired, the phone re-uploads a copy whose bytes differ
// from the original hashes, and only those new hashes validate afterwards.
type fakeMediaCDN struct {
	expiredPath   string
	freshPath     string
	freshSHA256   []byte
	freshEncSHA   []byte
	reuploadCalls int
	fetchCalls    int
	fetchFailures int
	onReupload    func()
	reuploadErr   error
	fetchErr      error
}

func (c *fakeMediaCDN) install(service *serviceMessage) {
	service.extractMediaFn = func(_ context.Context, _ *whatsmeow.Client, storageLocation string, mediaFile whatsmeow.DownloadableMessage) (utils.ExtractedMedia, error) {
		switch {
		case mediaFile.GetDirectPath() == c.expiredPath:
			return utils.ExtractedMedia{}, whatsmeow.ErrMediaDownloadFailedWith403
		case mediaFile.GetDirectPath() == c.freshPath && bytes.Equal(mediaFile.GetFileEncSHA256(), c.freshEncSHA):
			return writeFakeMedia(storageLocation)
		case mediaFile.GetDirectPath() == c.freshPath:
			return utils.ExtractedMedia{}, fmt.Errorf("failed to download media from last host: %w", whatsmeow.ErrInvalidMediaEncSHA256)
		}
		return utils.ExtractedMedia{}, fmt.Errorf("unexpected direct path %q", mediaFile.GetDirectPath())
	}
	service.requestMediaReuploadFn = func(_ context.Context, _ *whatsmeow.Client, _ types.MessageInfo, _ []byte, _ time.Duration) (string, error) {
		c.reuploadCalls++
		if c.onReupload != nil {
			c.onReupload()
		}
		if c.reuploadErr != nil {
			return "", c.reuploadErr
		}
		return c.freshPath, nil
	}
	service.fetchReuploadedMediaFn = func(_ context.Context, _ *whatsmeow.Client, directPath string, _ whatsmeow.DownloadableMessage, storageLocation string) (utils.ReuploadedMedia, error) {
		c.fetchCalls++
		if c.fetchErr != nil {
			return utils.ReuploadedMedia{}, c.fetchErr
		}
		if c.fetchFailures > 0 {
			c.fetchFailures--
			return utils.ReuploadedMedia{}, context.DeadlineExceeded
		}
		if directPath != c.freshPath {
			return utils.ReuploadedMedia{}, fmt.Errorf("unexpected direct path %q", directPath)
		}
		extracted, err := writeFakeMedia(storageLocation)
		return utils.ReuploadedMedia{ExtractedMedia: extracted, FileSHA256: c.freshSHA256, FileEncSHA256: c.freshEncSHA, FileLength: 4}, err
	}
}

func writeFakeMedia(storageLocation string) (utils.ExtractedMedia, error) {
	path := filepath.Join(storageLocation, "image.jpg")
	return utils.ExtractedMedia{MediaPath: path, MimeType: "image/jpeg"}, os.WriteFile(path, []byte("jpeg"), 0o600)
}

const expiredImageURL = "https://mmg.whatsapp.net/v/t62.7118-24/expired.enc?oh=old"

func newMediaRetryFixture(t *testing.T) (*scopeFixture, *fakeMediaCDN) {
	t.Helper()
	restorePath := config.PathMedia
	config.PathMedia = t.TempDir()
	t.Cleanup(func() { config.PathMedia = restorePath })

	f := newScopeFixture(t)
	f.storeChat(t, scopeDeviceA, 0)
	original := sha256.Sum256([]byte("original"))
	f.storeMessage(t, domainChatStorage.Message{
		ID:            "image-expired",
		DeviceID:      scopeDeviceA,
		Sender:        scopeMember.String(),
		Content:       "original caption",
		MediaType:     "image",
		URL:           expiredImageURL,
		DirectPath:    "/v/t62.7118-24/expired.enc",
		MediaKey:      []byte("0123456789abcdef0123456789abcdef"),
		FileSHA256:    original[:],
		FileEncSHA256: original[:],
		FileLength:    8,
	})

	freshPlain := sha256.Sum256([]byte("phone copy"))
	freshEnc := sha256.Sum256([]byte("phone copy encrypted"))
	cdn := &fakeMediaCDN{
		expiredPath: "/v/t62.7118-24/expired.enc",
		freshPath:   "/v/t62.7118-24/fresh.enc",
		freshSHA256: freshPlain[:],
		freshEncSHA: freshEnc[:],
	}
	cdn.install(&f.service)
	return f, cdn
}

func downloadExpiredImage(f *scopeFixture) (domainMessage.DownloadMediaResponse, error) {
	return f.service.DownloadMedia(scopeContext(scopeDeviceA), domainMessage.DownloadMediaRequest{
		MessageID: "image-expired",
		Phone:     scopeGroup.String(),
	})
}

func storedExpiredImage(t *testing.T, f *scopeFixture) *domainChatStorage.Message {
	t.Helper()
	stored, err := f.repo.GetMessageByIDChatAndDevice(scopeDeviceA, scopeGroup.String(), "image-expired")
	require.NoError(t, err)
	return stored
}

func TestDownloadMediaRecoversChangedCopyAndReusesItOnNextDownload(t *testing.T) {
	f, cdn := newMediaRetryFixture(t)

	first, err := downloadExpiredImage(f)
	require.NoError(t, err)
	require.NotEmpty(t, first.FilePath)
	require.Equal(t, 1, cdn.reuploadCalls)
	require.Equal(t, 1, cdn.fetchCalls, "the re-uploaded file is downloaded once")

	stored := storedExpiredImage(t, f)
	require.Equal(t, cdn.freshPath, stored.DirectPath)
	require.Equal(t, "https://mmg.whatsapp.net"+cdn.freshPath, stored.URL)
	require.Equal(t, cdn.freshSHA256, stored.FileSHA256)
	require.Equal(t, cdn.freshEncSHA, stored.FileEncSHA256)
	require.Equal(t, uint64(4), stored.FileLength)

	// The second download must work through the normal path: no new phone
	// round trip and no authenticated fallback.
	second, err := downloadExpiredImage(f)
	require.NoError(t, err)
	require.NotEmpty(t, second.FilePath)
	require.Equal(t, 1, cdn.reuploadCalls)
	require.Equal(t, 1, cdn.fetchCalls)
}

func TestDownloadMediaRecoveryKeepsConcurrentEdit(t *testing.T) {
	f, cdn := newMediaRetryFixture(t)
	cdn.onReupload = func() {
		// A caption edit lands while the phone is re-uploading.
		edited := storedExpiredImage(t, f)
		edited.Content = "edited caption"
		require.NoError(t, f.repo.StoreMessage(edited))
	}

	_, err := downloadExpiredImage(f)
	require.NoError(t, err)

	stored := storedExpiredImage(t, f)
	require.Equal(t, "edited caption", stored.Content)
	require.Equal(t, cdn.freshPath, stored.DirectPath)
}

func TestDownloadMediaKeepsOriginalErrorWhenReuploadRequestFails(t *testing.T) {
	f, cdn := newMediaRetryFixture(t)
	cdn.reuploadErr = whatsapp.ErrMediaRetryTimeout

	_, err := downloadExpiredImage(f)

	require.ErrorIs(t, err, whatsmeow.ErrMediaDownloadFailedWith403)
	require.ErrorIs(t, err, whatsapp.ErrMediaRetryTimeout)
	stored := storedExpiredImage(t, f)
	require.Equal(t, cdn.expiredPath, stored.DirectPath, "a failed recovery must not touch the stored media")
	require.Equal(t, expiredImageURL, stored.URL)
}

func TestDownloadMediaKeepsOriginalErrorWhenReuploadedFileIsRejected(t *testing.T) {
	f, cdn := newMediaRetryFixture(t)
	cdn.fetchErr = utils.ErrInvalidReuploadedMediaHMAC

	_, err := downloadExpiredImage(f)

	require.ErrorIs(t, err, whatsmeow.ErrMediaDownloadFailedWith403)
	require.ErrorIs(t, err, utils.ErrInvalidReuploadedMediaHMAC)
	require.Equal(t, cdn.expiredPath, storedExpiredImage(t, f).DirectPath, "a rejected file must not be recorded")
}

func TestDownloadMediaRetriesRecoveryAfterFailedDownload(t *testing.T) {
	f, cdn := newMediaRetryFixture(t)
	cdn.fetchFailures = 1

	_, err := downloadExpiredImage(f)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, cdn.expiredPath, storedExpiredImage(t, f).DirectPath)

	// Nothing half-recovered was stored, so the next call asks the phone again
	// and succeeds.
	_, err = downloadExpiredImage(f)
	require.NoError(t, err)
	require.Equal(t, 2, cdn.reuploadCalls)
	require.Equal(t, cdn.freshPath, storedExpiredImage(t, f).DirectPath)
}

func TestDownloadMediaStillRejectsHashMismatchOutsideReupload(t *testing.T) {
	f, cdn := newMediaRetryFixture(t)
	// A stored path that serves bytes not matching the message hashes, with no
	// re-upload involved, stays an integrity error.
	stale := storedExpiredImage(t, f)
	require.NoError(t, f.repo.UpdateMessageMedia(scopeDeviceA, scopeGroup.String(), "image-expired", domainChatStorage.MessageMedia{
		URL:           stale.URL,
		DirectPath:    cdn.freshPath,
		FileSHA256:    stale.FileSHA256,
		FileEncSHA256: stale.FileEncSHA256,
		FileLength:    stale.FileLength,
	}))

	_, err := downloadExpiredImage(f)

	require.ErrorIs(t, err, whatsmeow.ErrInvalidMediaEncSHA256)
	require.Zero(t, cdn.reuploadCalls)
	require.Zero(t, cdn.fetchCalls)
}
