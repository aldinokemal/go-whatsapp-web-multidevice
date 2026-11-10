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
	block   bool
	sendErr error
	answer  func(info *types.MessageInfo) *events.MediaRetry
	sent    []types.MessageInfo
}

func (f *fakeMediaRetrySender) SendMediaRetryReceipt(ctx context.Context, info *types.MessageInfo, _ []byte) error {
	f.sent = append(f.sent, *info)
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.sendErr != nil {
		return f.sendErr
	}
	if f.answer != nil {
		if evt := f.answer(info); evt != nil {
			// The phone echoes the message identity it was asked about.
			mediaRetryWaiters.deliver(mediaRetryKey{deviceID: testMediaRetryDevice, messageID: info.ID}, identityFromMessageInfo(*info), evt)
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
	identity := identityFromMessageInfo(testMediaRetryInfo("MSG"))
	responses, unregister := registry.register(mediaRetryKey{deviceID: "device-a", messageID: "MSG"}, identity)
	defer unregister()

	delivered := registry.deliver(mediaRetryKey{deviceID: "device-b", messageID: "MSG"}, identity, &events.MediaRetry{MessageID: "MSG"})

	require.False(t, delivered)
	select {
	case evt := <-responses:
		t.Fatalf("device A received device B's notification: %v", evt)
	default:
	}
}

func TestMediaRetryRegistryKeepsWaitingOnSameIDFromAnotherChat(t *testing.T) {
	registry := newMediaRetryRegistry()
	key := mediaRetryKey{deviceID: "device-a", messageID: "MSG"}
	group := types.NewJID("120363000000000001", types.GroupServer)
	author := types.NewJID("5511777777777", types.DefaultUserServer)
	wanted := mediaRetryIdentity{chat: group, sender: author}
	responses, unregister := registry.register(key, wanted)
	defer unregister()

	// A private chat message reusing the same ID (and possibly the same media
	// key) must not satisfy the group download.
	private := mediaRetryIdentity{chat: types.NewJID("5511666666666", types.DefaultUserServer)}
	require.False(t, registry.deliver(key, private, &events.MediaRetry{MessageID: "MSG"}))
	// Another author in the same group is a different message too.
	otherAuthor := mediaRetryIdentity{chat: group, sender: types.NewJID("5511555555555", types.DefaultUserServer)}
	require.False(t, registry.deliver(key, otherAuthor, &events.MediaRetry{MessageID: "MSG"}))
	select {
	case evt := <-responses:
		t.Fatalf("download received another message's notification: %v", evt)
	default:
	}

	answer := &events.MediaRetry{MessageID: "MSG"}
	require.True(t, registry.deliver(key, wanted, answer))
	require.Same(t, answer, <-responses)
}

func TestMediaRetryIdentityMatchesWithoutEchoedSender(t *testing.T) {
	group := types.NewJID("120363000000000001", types.GroupServer)
	requested := mediaRetryIdentity{chat: group, sender: types.NewJID("5511777777777", types.DefaultUserServer)}

	require.True(t, requested.matches(mediaRetryIdentity{chat: group}))
	require.False(t, requested.matches(mediaRetryIdentity{chat: group, fromMe: true}))
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

func TestRequestMediaReuploadTimeoutCoversBlockedSend(t *testing.T) {
	sender := &fakeMediaRetrySender{block: true}

	start := time.Now()
	_, err := requestMediaReupload(context.Background(), sender, testMediaRetryDevice, testMediaRetryInfo("MSG-BLOCKED"), []byte("key"), 30*time.Millisecond)

	require.ErrorIs(t, err, ErrMediaRetryTimeout)
	require.Less(t, time.Since(start), time.Second, "a blocked send must not outlive the retry timeout")
	require.Empty(t, mediaRetryWaiters.waiters)
}

func TestRequestMediaReuploadReturnsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sender := &fakeMediaRetrySender{block: true}

	_, err := requestMediaReupload(ctx, sender, testMediaRetryDevice, testMediaRetryInfo("MSG-CANCELLED"), []byte("key"), time.Second)

	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrMediaRetryTimeout)
}
