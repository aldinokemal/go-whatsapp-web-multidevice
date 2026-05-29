package chatwoot

import (
	"sync"
	"time"
)

// trackedMessageTTL bounds how long a WhatsApp→Chatwoot message mapping is kept
// while waiting for delivery/read receipts. Read receipts can arrive well after
// delivery, so this is generous. Mappings are in-memory and lost on restart.
const trackedMessageTTL = 24 * time.Hour

type trackedMessage struct {
	conversationID int
	chatwootMsgID  int
	storedAt       time.Time
}

// trackedMessages maps a WhatsApp message ID to the Chatwoot message it was
// delivered as, so later WhatsApp receipts can update that message's status.
var trackedMessages sync.Map // key: WhatsApp message ID (string) -> trackedMessage

// TrackOutgoingMessage records the WhatsApp↔Chatwoot mapping for an outgoing
// message so a subsequent delivery/read receipt can update the Chatwoot status.
func TrackOutgoingMessage(waMessageID string, conversationID, chatwootMessageID int) {
	if waMessageID == "" || conversationID == 0 || chatwootMessageID == 0 {
		return
	}
	trackedMessages.Store(waMessageID, trackedMessage{
		conversationID: conversationID,
		chatwootMsgID:  chatwootMessageID,
		storedAt:       time.Now(),
	})
}

// ResolveTrackedMessage returns the Chatwoot conversation and message IDs for a
// WhatsApp message ID, if still tracked and not expired.
func ResolveTrackedMessage(waMessageID string) (conversationID, chatwootMessageID int, ok bool) {
	val, found := trackedMessages.Load(waMessageID)
	if !found {
		return 0, 0, false
	}
	tm := val.(trackedMessage)
	if time.Since(tm.storedAt) > trackedMessageTTL {
		trackedMessages.Delete(waMessageID)
		return 0, 0, false
	}
	return tm.conversationID, tm.chatwootMsgID, true
}

func init() {
	go func() {
		ticker := time.NewTicker(trackedMessageTTL)
		defer ticker.Stop()
		for range ticker.C {
			trackedMessages.Range(func(key, value any) bool {
				if time.Since(value.(trackedMessage).storedAt) > trackedMessageTTL {
					trackedMessages.Delete(key)
				}
				return true
			})
		}
	}()
}
