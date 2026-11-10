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

// mediaRetryRegistry routes *events.MediaRetry notifications, which arrive on
// the device event loop, to the downloads waiting for them.
type mediaRetryRegistry struct {
	mu      sync.Mutex
	waiters map[mediaRetryKey][]chan *events.MediaRetry
}

func newMediaRetryRegistry() *mediaRetryRegistry {
	return &mediaRetryRegistry{waiters: make(map[mediaRetryKey][]chan *events.MediaRetry)}
}

var mediaRetryWaiters = newMediaRetryRegistry()

func (r *mediaRetryRegistry) register(key mediaRetryKey) (<-chan *events.MediaRetry, func()) {
	ch := make(chan *events.MediaRetry, 1)
	r.mu.Lock()
	r.waiters[key] = append(r.waiters[key], ch)
	r.mu.Unlock()

	unregister := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		waiters := r.waiters[key]
		for i, waiter := range waiters {
			if waiter == ch {
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
	return ch, unregister
}

// deliver hands the notification to every download waiting on the message and
// reports whether anyone was waiting. It never blocks the event loop.
func (r *mediaRetryRegistry) deliver(key mediaRetryKey, evt *events.MediaRetry) bool {
	r.mu.Lock()
	waiters := append([]chan *events.MediaRetry(nil), r.waiters[key]...)
	r.mu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- evt:
		default:
		}
	}
	return len(waiters) > 0
}

func handleMediaRetry(client *whatsmeow.Client, evt *events.MediaRetry) {
	if evt == nil || client == nil || client.Store == nil || client.Store.ID == nil {
		return
	}
	key := mediaRetryKey{deviceID: client.Store.ID.ToNonAD().String(), messageID: evt.MessageID}
	if !mediaRetryWaiters.deliver(key, evt) {
		log.Debugf("Media retry notification for message %s arrived with no pending download", evt.MessageID)
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

	// Register before sending so a fast answer is not lost.
	responses, unregister := mediaRetryWaiters.register(mediaRetryKey{deviceID: deviceID, messageID: info.ID})
	defer unregister()

	if err := sender.SendMediaRetryReceipt(ctx, &info, mediaKey); err != nil {
		return "", fmt.Errorf("failed to request media re-upload: %w", err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var evt *events.MediaRetry
	select {
	case evt = <-responses:
	case <-timer.C:
		return "", ErrMediaRetryTimeout
	case <-ctx.Done():
		return "", ctx.Err()
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
