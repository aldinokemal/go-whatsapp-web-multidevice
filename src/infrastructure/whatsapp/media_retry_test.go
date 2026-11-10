package whatsapp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	"google.golang.org/protobuf/proto"
)

const testMediaRetryDevice = "5511999999999@s.whatsapp.net"

// fakeMediaRetrySender answers a re-upload request the way the phone would,
// by delivering a notification through the event handler path.
type fakeMediaRetrySender struct {
	sendErr error
	answer  func(info *types.MessageInfo) *events.MediaRetry
	sent    []types.MessageInfo
}

func (f *fakeMediaRetrySender) SendMediaRetryReceipt(_ context.Context, info *types.MessageInfo, _ []byte) error {
	f.sent = append(f.sent, *info)
	if f.sendErr != nil {
		return f.sendErr
	}
	if f.answer != nil {
		if evt := f.answer(info); evt != nil {
			mediaRetryWaiters.deliver(mediaRetryKey{deviceID: testMediaRetryDevice, messageID: info.ID}, evt)
		}
	}
	return nil
}

// encryptedMediaRetry builds a notification encrypted the same way the phone
// does (AES-GCM with the HKDF-derived media retry key, message ID as AAD).
func encryptedMediaRetry(t *testing.T, messageID string, mediaKey []byte, notification *waMmsRetry.MediaRetryNotification) *events.MediaRetry {
	t.Helper()
	plaintext, err := proto.Marshal(notification)
	require.NoError(t, err)
	key := hkdfutil.SHA256(mediaKey, nil, []byte("WhatsApp Media Retry Notification"), 32)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	iv := make([]byte, 12)
	_, err = rand.Read(iv)
	require.NoError(t, err)
	return &events.MediaRetry{
		MessageID:  messageID,
		IV:         iv,
		Ciphertext: gcm.Seal(nil, iv, plaintext, []byte(messageID)),
	}
}

func testMediaRetryInfo(id string) types.MessageInfo {
	return types.MessageInfo{
		ID:            id,
		MessageSource: types.MessageSource{Chat: types.NewJID("5511888888888", types.DefaultUserServer)},
	}
}

func TestRequestMediaReuploadReturnsNewDirectPath(t *testing.T) {
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	sender := &fakeMediaRetrySender{answer: func(info *types.MessageInfo) *events.MediaRetry {
		return encryptedMediaRetry(t, info.ID, mediaKey, &waMmsRetry.MediaRetryNotification{
			StanzaID:   proto.String(info.ID),
			DirectPath: proto.String("/v/t62.7118-24/fresh.enc"),
			Result:     waMmsRetry.MediaRetryNotification_SUCCESS.Enum(),
		})
	}}

	directPath, err := requestMediaReupload(context.Background(), sender, testMediaRetryDevice, testMediaRetryInfo("MSG-OK"), mediaKey, time.Second)

	require.NoError(t, err)
	require.Equal(t, "/v/t62.7118-24/fresh.enc", directPath)
	require.Len(t, sender.sent, 1)
	require.Empty(t, mediaRetryWaiters.waiters, "waiter must be removed after the answer")
}

func TestRequestMediaReuploadReportsPhoneFailure(t *testing.T) {
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	sender := &fakeMediaRetrySender{answer: func(info *types.MessageInfo) *events.MediaRetry {
		return encryptedMediaRetry(t, info.ID, mediaKey, &waMmsRetry.MediaRetryNotification{
			StanzaID: proto.String(info.ID),
			Result:   waMmsRetry.MediaRetryNotification_NOT_FOUND.Enum(),
		})
	}}

	_, err := requestMediaReupload(context.Background(), sender, testMediaRetryDevice, testMediaRetryInfo("MSG-NOT-FOUND"), mediaKey, time.Second)

	require.ErrorContains(t, err, "NOT_FOUND")
}

func TestRequestMediaReuploadReportsMediaGoneFromPhone(t *testing.T) {
	sender := &fakeMediaRetrySender{answer: func(info *types.MessageInfo) *events.MediaRetry {
		return &events.MediaRetry{MessageID: info.ID, Error: &events.MediaRetryError{Code: 2}}
	}}

	_, err := requestMediaReupload(context.Background(), sender, testMediaRetryDevice, testMediaRetryInfo("MSG-GONE"), []byte("0123456789abcdef0123456789abcdef"), time.Second)

	require.ErrorIs(t, err, whatsmeow.ErrMediaNotAvailableOnPhone)
}

func TestRequestMediaReuploadTimesOutWhenPhoneIsSilent(t *testing.T) {
	sender := &fakeMediaRetrySender{}

	_, err := requestMediaReupload(context.Background(), sender, testMediaRetryDevice, testMediaRetryInfo("MSG-SILENT"), []byte("key"), 20*time.Millisecond)

	require.ErrorIs(t, err, ErrMediaRetryTimeout)
	require.Empty(t, mediaRetryWaiters.waiters, "waiter must be removed after a timeout")
}

func TestRequestMediaReuploadPropagatesSendError(t *testing.T) {
	sender := &fakeMediaRetrySender{sendErr: errors.New("not connected")}

	_, err := requestMediaReupload(context.Background(), sender, testMediaRetryDevice, testMediaRetryInfo("MSG-SEND"), []byte("key"), time.Second)

	require.ErrorContains(t, err, "not connected")
	require.Empty(t, mediaRetryWaiters.waiters)
}

func TestRequestMediaReuploadRequiresMediaKey(t *testing.T) {
	sender := &fakeMediaRetrySender{}

	_, err := requestMediaReupload(context.Background(), sender, testMediaRetryDevice, testMediaRetryInfo("MSG-NOKEY"), nil, time.Second)

	require.ErrorContains(t, err, "no media key")
	require.Empty(t, sender.sent)
}

func TestMediaRetryRegistryIgnoresOtherDevices(t *testing.T) {
	registry := newMediaRetryRegistry()
	responses, unregister := registry.register(mediaRetryKey{deviceID: "device-a", messageID: "MSG"})
	defer unregister()

	delivered := registry.deliver(mediaRetryKey{deviceID: "device-b", messageID: "MSG"}, &events.MediaRetry{MessageID: "MSG"})

	require.False(t, delivered)
	select {
	case evt := <-responses:
		t.Fatalf("device A received device B's notification: %v", evt)
	default:
	}
}

func TestIsExpiredMediaError(t *testing.T) {
	for _, err := range []error{
		whatsmeow.ErrMediaDownloadFailedWith403,
		whatsmeow.ErrMediaDownloadFailedWith404,
		whatsmeow.ErrMediaDownloadFailedWith410,
		fmt.Errorf("wrapped: %w", whatsmeow.ErrMediaDownloadFailedWith410),
	} {
		require.True(t, IsExpiredMediaError(err), err.Error())
	}
	require.False(t, IsExpiredMediaError(whatsmeow.ErrInvalidMediaSHA256))
	require.False(t, IsExpiredMediaError(errors.New("other")))
}
