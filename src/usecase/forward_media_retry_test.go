package usecase

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

const forwardedImagePath = "/v/t62.7118-24/forwarded.enc"

// forwardRetryFixture forwards the expired image of newMediaRetryFixture
// through the same fake CDN and phone, and records what gets uploaded for the
// recipient.
type forwardRetryFixture struct {
	*scopeFixture
	cdn      *fakeMediaCDN
	send     serviceSend
	uploaded [][]byte
}

func newForwardRetryFixture(t *testing.T) *forwardRetryFixture {
	t.Helper()
	restorePath := config.PathSendItems
	config.PathSendItems = t.TempDir()
	t.Cleanup(func() { config.PathSendItems = restorePath })

	scope, cdn := newMediaRetryFixture(t)
	f := &forwardRetryFixture{scopeFixture: scope, cdn: cdn}
	f.send = serviceSend{
		chatStorageRepo:        scope.repo,
		extractMediaFn:         scope.service.extractMediaFn,
		requestMediaReuploadFn: scope.service.requestMediaReuploadFn,
		fetchReuploadedMediaFn: scope.service.fetchReuploadedMediaFn,
		uploadMediaFn: func(_ context.Context, _ *whatsmeow.Client, _ whatsmeow.MediaType, media []byte, _ types.JID) (whatsmeow.UploadResponse, error) {
			f.uploaded = append(f.uploaded, media)
			return whatsmeow.UploadResponse{DirectPath: forwardedImagePath, FileLength: uint64(len(media))}, nil
		},
	}
	return f
}

func (f *forwardRetryFixture) forwardExpiredImage(t *testing.T) (*waE2E.Message, error) {
	t.Helper()
	recipient := types.NewJID("5511777777777", types.DefaultUserServer)
	return f.send.reuploadForwardMessage(context.Background(), nil, storedExpiredImage(t, f.scopeFixture), recipient, utils.ForwardBuildOptions{})
}

func TestForwardReuploadRecoversExpiredMediaFromPhone(t *testing.T) {
	f := newForwardRetryFixture(t)

	msg, err := f.forwardExpiredImage(t)

	require.NoError(t, err)
	require.Equal(t, forwardedImagePath, msg.GetImageMessage().GetDirectPath())
	require.Equal(t, [][]byte{[]byte("jpeg")}, f.uploaded, "the phone's copy is what gets forwarded")
	require.Equal(t, 1, f.cdn.reuploadCalls)
	require.Equal(t, f.cdn.freshPath, storedExpiredImage(t, f.scopeFixture).DirectPath)

	// The recovered location is stored, so the next forward downloads it
	// without another phone round trip.
	_, err = f.forwardExpiredImage(t)
	require.NoError(t, err)
	require.Equal(t, 1, f.cdn.reuploadCalls)
	require.Equal(t, 1, f.cdn.fetchCalls)
}

func TestForwardReuploadKeepsOriginalErrorWhenPhoneDoesNotAnswer(t *testing.T) {
	f := newForwardRetryFixture(t)
	f.cdn.reuploadErr = whatsapp.ErrMediaRetryTimeout

	_, err := f.forwardExpiredImage(t)

	require.ErrorIs(t, err, whatsmeow.ErrMediaDownloadFailedWith403)
	require.ErrorIs(t, err, whatsapp.ErrMediaRetryTimeout)
	require.Empty(t, f.uploaded)
	require.Equal(t, f.cdn.expiredPath, storedExpiredImage(t, f.scopeFixture).DirectPath, "a failed recovery must not touch the stored media")
}
