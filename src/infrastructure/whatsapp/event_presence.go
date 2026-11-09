package whatsapp

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// handlePresence handles incoming global presence (online/offline) events.
// WhatsApp only sends these updates for users the client has explicitly
// subscribed to via SubscribePresence (see POST /user/presence/subscribe),
// and only while the client is marked as available.
func handlePresence(ctx context.Context, evt *events.Presence, deviceID string, client *whatsmeow.Client) {
	if evt.Unavailable {
		if evt.LastSeen.IsZero() {
			log.Infof("%s is now offline", evt.From)
		} else {
			log.Infof("%s is now offline (last seen: %s)", evt.From, evt.LastSeen)
		}
	} else {
		log.Infof("%s is now online", evt.From)
	}

	// Forward presence event to webhook
	go func(e *events.Presence, c *whatsmeow.Client) {
		webhookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := forwardPresenceToWebhook(webhookCtx, e, deviceID, c); err != nil {
			logrus.Errorf("Failed to forward presence event to webhook: %v", err)
		}
	}(evt, client)
}

// createPresencePayload creates a webhook payload for presence (online/offline) events.
func createPresencePayload(ctx context.Context, evt *events.Presence, deviceID string, client *whatsmeow.Client) map[string]any {
	body := make(map[string]any)
	payload := make(map[string]any)

	// Resolve JID (convert LID to phone number if needed)
	fromJID := evt.From
	if fromJID.Server == "lid" {
		payload["from_lid"] = fromJID.ToNonAD().String()
	}
	normalizedFromJID := NormalizeJIDFromLID(ctx, fromJID, client)
	payload["from"] = normalizedFromJID.ToNonAD().String()

	// Online state: "unavailable" (offline) or "available" (online)
	if evt.Unavailable {
		payload["state"] = "unavailable"
	} else {
		payload["state"] = "available"
	}

	// Last seen: RFC3339 timestamp, omitted when unknown (user hides last seen)
	if !evt.LastSeen.IsZero() {
		payload["last_seen"] = evt.LastSeen.UTC().Format(time.RFC3339)
	}

	// Wrap in body structure
	body["event"] = "presence"
	body["timestamp"] = time.Now().Format(time.RFC3339)
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	body["payload"] = payload

	return body
}

// forwardPresenceToWebhook forwards presence events to the configured webhook URLs.
func forwardPresenceToWebhook(ctx context.Context, evt *events.Presence, deviceID string, client *whatsmeow.Client) error {
	payload := createPresencePayload(ctx, evt, deviceID, client)
	return forwardPayloadToConfiguredWebhooks(ctx, payload, "presence")
}
