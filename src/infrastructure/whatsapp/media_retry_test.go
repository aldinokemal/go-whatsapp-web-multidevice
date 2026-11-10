package whatsapp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	waLog "go.mau.fi/whatsmeow/util/log"
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

// requestTestReupload asks for a re-upload the way RequestMediaReupload does,
// minus the client that resolves LIDs.
func requestTestReupload(ctx context.Context, sender mediaRetrySender, info types.MessageInfo, mediaKey []byte, timeout time.Duration) (string, error) {
	return requestMediaReupload(ctx, sender, testMediaRetryDevice, info, identityFromMessageInfo(info), mediaKey, timeout)
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

	directPath, err := requestTestReupload(context.Background(), sender, testMediaRetryInfo("MSG-OK"), mediaKey, time.Second)

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

	_, err := requestTestReupload(context.Background(), sender, testMediaRetryInfo("MSG-NOT-FOUND"), mediaKey, time.Second)

	require.ErrorContains(t, err, "NOT_FOUND")
}

func TestRequestMediaReuploadReportsMediaGoneFromPhone(t *testing.T) {
	sender := &fakeMediaRetrySender{answer: func(info *types.MessageInfo) *events.MediaRetry {
		return &events.MediaRetry{MessageID: info.ID, Error: &events.MediaRetryError{Code: 2}}
	}}

	_, err := requestTestReupload(context.Background(), sender, testMediaRetryInfo("MSG-GONE"), []byte("0123456789abcdef0123456789abcdef"), time.Second)

	require.ErrorIs(t, err, whatsmeow.ErrMediaNotAvailableOnPhone)
}

func TestRequestMediaReuploadTimesOutWhenPhoneIsSilent(t *testing.T) {
	sender := &fakeMediaRetrySender{}

	_, err := requestTestReupload(context.Background(), sender, testMediaRetryInfo("MSG-SILENT"), []byte("key"), 20*time.Millisecond)

	require.ErrorIs(t, err, ErrMediaRetryTimeout)
	require.Empty(t, mediaRetryWaiters.waiters, "waiter must be removed after a timeout")
}

func TestRequestMediaReuploadPropagatesSendError(t *testing.T) {
	sender := &fakeMediaRetrySender{sendErr: errors.New("not connected")}

	_, err := requestTestReupload(context.Background(), sender, testMediaRetryInfo("MSG-SEND"), []byte("key"), time.Second)

	require.ErrorContains(t, err, "not connected")
	require.Empty(t, mediaRetryWaiters.waiters)
}

func TestRequestMediaReuploadRequiresMediaKey(t *testing.T) {
	sender := &fakeMediaRetrySender{}

	_, err := requestTestReupload(context.Background(), sender, testMediaRetryInfo("MSG-NOKEY"), nil, time.Second)

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

// phoneThroughEventHandler answers a request through the real event handler,
// echoing the chat and participant it was asked about.
type phoneThroughEventHandler struct {
	client *whatsmeow.Client
	answer *events.MediaRetry
}

func (p phoneThroughEventHandler) SendMediaRetryReceipt(ctx context.Context, info *types.MessageInfo, _ []byte) error {
	evt := *p.answer
	evt.ChatID, evt.SenderID, evt.FromMe = info.Chat, info.Sender, info.IsFromMe
	handleMediaRetry(ctx, p.client, &evt)
	return nil
}

func TestRequestMediaReuploadMatchesSenderStoredBeforeItsLIDWasMapped(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	account := types.NewJID("5511999999999", types.DefaultUserServer)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &account, LIDs: &callOfferLIDStore{
		phoneNumbers: map[types.JID]types.JID{lid: types.NewJID("5511777777777", types.DefaultUserServer)},
	}}}
	// History sync stored the group sender as a LID; its phone number is known by now.
	info := types.MessageInfo{
		ID:            "MSG-LID",
		MessageSource: types.MessageSource{Chat: types.NewJID("120363000000000001", types.GroupServer), Sender: lid, IsGroup: true},
	}
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	phone := phoneThroughEventHandler{client: client, answer: encryptedMediaRetry(t, info.ID, mediaKey, &waMmsRetry.MediaRetryNotification{
		DirectPath: proto.String("/v/t62.7118-24/fresh.enc"),
		Result:     waMmsRetry.MediaRetryNotification_SUCCESS.Enum(),
	})}
	identity := identityFromMessageInfo(info).normalized(context.Background(), client)

	directPath, err := requestMediaReupload(context.Background(), phone, mediaRetryDeviceID(client), info, identity, mediaKey, 50*time.Millisecond)

	require.NoError(t, err)
	require.Equal(t, "/v/t62.7118-24/fresh.enc", directPath)
}

func TestRequestMediaReuploadIgnoresAnswerOnSiblingCompanionSlot(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	// Two companion sessions linked to the same number.
	slotA := types.NewADJID("5511999999999", 0, 11)
	slotB := types.NewADJID("5511999999999", 0, 12)
	clientA := &whatsmeow.Client{Store: &store.Device{ID: &slotA}}
	clientB := &whatsmeow.Client{Store: &store.Device{ID: &slotB}}
	info := testMediaRetryInfo("MSG-SLOT")
	identity := identityFromMessageInfo(info)
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	answer := encryptedMediaRetry(t, info.ID, mediaKey, &waMmsRetry.MediaRetryNotification{
		DirectPath: proto.String("/v/t62.7118-24/fresh.enc"),
		Result:     waMmsRetry.MediaRetryNotification_SUCCESS.Enum(),
	})

	_, err := requestMediaReupload(context.Background(), phoneThroughEventHandler{client: clientB, answer: answer}, mediaRetryDeviceID(clientA), info, identity, mediaKey, 30*time.Millisecond)
	require.ErrorIs(t, err, ErrMediaRetryTimeout, "slot B's answer must not complete slot A's request")

	directPath, err := requestMediaReupload(context.Background(), phoneThroughEventHandler{client: clientA, answer: answer}, mediaRetryDeviceID(clientA), info, identity, mediaKey, time.Second)
	require.NoError(t, err)
	require.Equal(t, "/v/t62.7118-24/fresh.enc", directPath)
}

func TestRequestMediaReuploadTimeoutCoversBlockedSend(t *testing.T) {
	sender := &fakeMediaRetrySender{block: true}

	start := time.Now()
	_, err := requestTestReupload(context.Background(), sender, testMediaRetryInfo("MSG-BLOCKED"), []byte("key"), 30*time.Millisecond)

	require.ErrorIs(t, err, ErrMediaRetryTimeout)
	require.Less(t, time.Since(start), time.Second, "a blocked send must not outlive the retry timeout")
	require.Empty(t, mediaRetryWaiters.waiters)
}

func TestRequestMediaReuploadReturnsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sender := &fakeMediaRetrySender{block: true}

	_, err := requestTestReupload(ctx, sender, testMediaRetryInfo("MSG-CANCELLED"), []byte("key"), time.Second)

	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrMediaRetryTimeout)
}
