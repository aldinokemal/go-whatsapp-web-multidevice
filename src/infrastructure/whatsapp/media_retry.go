package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// DefaultMediaRetryTimeout bounds how long a download waits for the phone to
// answer a media re-upload request. The phone must be online to answer.
const DefaultMediaRetryTimeout = 30 * time.Second

// ErrMediaRetryTimeout is returned when the phone does not answer a media
// re-upload request in time, usually because it is offline.
var ErrMediaRetryTimeout = errors.New("timed out waiting for the phone to re-upload the media")

// IsExpiredMediaError reports whether a download failed because the media is
// no longer on WhatsApp's servers (403, 404 or 410), which is the case the
// phone can fix by re-uploading the file.
func IsExpiredMediaError(err error) bool {
	return errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410)
}

// mediaRetrySender is the part of *whatsmeow.Client used to ask the phone for
// a re-upload. It exists so the request/response flow can be tested.
type mediaRetrySender interface {
	SendMediaRetryReceipt(ctx context.Context, message *types.MessageInfo, mediaKey []byte) error
}

type mediaRetryKey struct {
	deviceID  string
	messageID types.MessageID
}

// mediaRetryIdentity is the message a download asked the phone about. A
// notification is only accepted when it names the same message: message IDs
// are not unique across chats, and in groups every participant holds the
// media key, so matching on the ID alone could hand one message's file to
// another.
type mediaRetryIdentity struct {
	chat   types.JID
	sender types.JID
	fromMe bool
}

func identityFromMessageInfo(info types.MessageInfo) mediaRetryIdentity {
	identity := mediaRetryIdentity{chat: info.Chat.ToNonAD(), fromMe: info.IsFromMe}
	if info.IsGroup {
		identity.sender = info.Sender.ToNonAD()
	}
	return identity
}

// matches compares a notification's identity with the request. The sender is
// only compared when both sides carry one, since the phone sends it for group
// and broadcast messages only.
func (identity mediaRetryIdentity) matches(other mediaRetryIdentity) bool {
	if identity.chat != other.chat || identity.fromMe != other.fromMe {
		return false
	}
	if identity.sender.IsEmpty() || other.sender.IsEmpty() {
		return true
	}
	return identity.sender == other.sender
}

type mediaRetryWaiter struct {
	identity  mediaRetryIdentity
	responses chan *events.MediaRetry
}

// mediaRetryRegistry routes *events.MediaRetry notifications, which arrive on
// the device event loop, to the downloads waiting for them.
type mediaRetryRegistry struct {
	mu      sync.Mutex
	waiters map[mediaRetryKey][]*mediaRetryWaiter
}

func newMediaRetryRegistry() *mediaRetryRegistry {
	return &mediaRetryRegistry{waiters: make(map[mediaRetryKey][]*mediaRetryWaiter)}
}

var mediaRetryWaiters = newMediaRetryRegistry()

func (r *mediaRetryRegistry) register(key mediaRetryKey, identity mediaRetryIdentity) (<-chan *events.MediaRetry, func()) {
	waiter := &mediaRetryWaiter{identity: identity, responses: make(chan *events.MediaRetry, 1)}
	r.mu.Lock()
	r.waiters[key] = append(r.waiters[key], waiter)
	r.mu.Unlock()

	unregister := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		waiters := r.waiters[key]
		for i, registered := range waiters {
			if registered == waiter {
				waiters = append(waiters[:i], waiters[i+1:]...)
				break
			}
		}
		if len(waiters) == 0 {
			delete(r.waiters, key)
		} else {
			r.waiters[key] = waiters
		}
	}
	return waiter.responses, unregister
}

// deliver hands the notification to every download waiting on the same
// message and reports whether any did. Waiters for another message with the
// same ID keep waiting. It never blocks the event loop.
func (r *mediaRetryRegistry) deliver(key mediaRetryKey, identity mediaRetryIdentity, evt *events.MediaRetry) bool {
	r.mu.Lock()
	waiters := append([]*mediaRetryWaiter(nil), r.waiters[key]...)
	r.mu.Unlock()

	delivered := false
	for _, waiter := range waiters {
		if !waiter.identity.matches(identity) {
			continue
		}
		delivered = true
		select {
		case waiter.responses <- evt:
		default:
		}
	}
	return delivered
}

func handleMediaRetry(ctx context.Context, client *whatsmeow.Client, evt *events.MediaRetry) {
	if evt == nil || client == nil || client.Store == nil || client.Store.ID == nil {
		return
	}
	key := mediaRetryKey{deviceID: client.Store.ID.ToNonAD().String(), messageID: evt.MessageID}
	// Stored chats and senders use phone-number JIDs; normalize LIDs the same way.
	identity := mediaRetryIdentity{
		chat:   NormalizeJIDFromLID(ctx, evt.ChatID.ToNonAD(), client),
		fromMe: evt.FromMe,
	}
	if !evt.SenderID.IsEmpty() {
		identity.sender = NormalizeJIDFromLID(ctx, evt.SenderID.ToNonAD(), client)
	}
	if !mediaRetryWaiters.deliver(key, identity, evt) {
		log.Debugf("Media retry notification for message %s in chat %s on device %s arrived with no matching pending download", evt.MessageID, evt.ChatID, key.deviceID)
	}
}

// RequestMediaReupload asks the phone to re-upload the media of a message whose
// file expired on WhatsApp's servers and returns the new direct path. The
// phone must be online and still hold the file.
func RequestMediaReupload(ctx context.Context, client *whatsmeow.Client, info types.MessageInfo, mediaKey []byte, timeout time.Duration) (string, error) {
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return "", whatsmeow.ErrNotLoggedIn
	}
	return requestMediaReupload(ctx, client, client.Store.ID.ToNonAD().String(), info, mediaKey, timeout)
}

func requestMediaReupload(ctx context.Context, sender mediaRetrySender, deviceID string, info types.MessageInfo, mediaKey []byte, timeout time.Duration) (string, error) {
	if len(mediaKey) == 0 {
		return "", fmt.Errorf("message %s has no media key to request a re-upload", info.ID)
	}

	// One deadline covers both sending the request and waiting for the answer.
	retryCtx, cancel := context.WithTimeoutCause(ctx, timeout, ErrMediaRetryTimeout)
	defer cancel()

	// Register before sending so a fast answer is not lost.
	responses, unregister := mediaRetryWaiters.register(mediaRetryKey{deviceID: deviceID, messageID: info.ID}, identityFromMessageInfo(info))
	defer unregister()

	if err := sender.SendMediaRetryReceipt(retryCtx, &info, mediaKey); err != nil {
		if retryCtx.Err() != nil {
			return "", context.Cause(retryCtx)
		}
		return "", fmt.Errorf("failed to request media re-upload: %w", err)
	}

	var evt *events.MediaRetry
	select {
	case evt = <-responses:
	case <-retryCtx.Done():
		return "", context.Cause(retryCtx)
	}

	notification, err := whatsmeow.DecryptMediaRetryNotification(evt, mediaKey)
	if err != nil {
		return "", fmt.Errorf("media re-upload failed: %w", err)
	}
	if result := notification.GetResult(); result != waMmsRetry.MediaRetryNotification_SUCCESS {
		return "", fmt.Errorf("media re-upload failed: phone answered %s", result.String())
	}
	if notification.GetDirectPath() == "" {
		return "", errors.New("media re-upload failed: phone returned no direct path")
	}
	return notification.GetDirectPath(), nil
}
