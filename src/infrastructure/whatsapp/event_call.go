package whatsapp

import (
	"context"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// handleCallOffer handles incoming call events and optionally auto-rejects them
func handleCallOffer(ctx context.Context, evt *events.CallOffer, deviceID string, client *whatsmeow.Client) {
	logrus.Infof("Incoming call from %s (CallID: %s)", evt.CallCreator.String(), evt.CallID)

	// Auto-reject call if configured
	autoRejected := false
	if config.WhatsappAutoRejectCall {
		rejectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := client.RejectCall(rejectCtx, evt.CallCreator, evt.CallID); err != nil {
			logrus.Errorf("Failed to reject call from %s: %v", evt.CallCreator.String(), err)
		} else {
			autoRejected = true
			logrus.Infof("Auto-rejected call from %s (CallID: %s)", evt.CallCreator.String(), evt.CallID)
		}
	}

	// Forward call event to webhook if configured
	if len(config.WhatsappWebhook) > 0 {
		go func(e *events.CallOffer, c *whatsmeow.Client, rejected bool) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardCallOfferToWebhook(webhookCtx, e, deviceID, c, rejected); err != nil {
				logrus.Errorf("Failed to forward call event to webhook: %v", err)
			}
		}(evt, client, autoRejected)
	}
}

// createCallOfferPayload creates a webhook payload for incoming call events
func createCallOfferPayload(ctx context.Context, evt *events.CallOffer, deviceID string, client *whatsmeow.Client, autoRejected bool) map[string]any {
	body := make(map[string]any)
	payload := make(map[string]any)

	// Add call details
	payload["call_id"] = evt.CallID
	payload["from"] = evt.CallCreator.ToNonAD().String()
	payload["auto_rejected"] = autoRejected

	// Add caller platform info if available
	if evt.RemotePlatform != "" {
		payload["remote_platform"] = evt.RemotePlatform
	}
	if evt.RemoteVersion != "" {
		payload["remote_version"] = evt.RemoteVersion
	}

	// Add group JID if this is a group call
	if !evt.GroupJID.IsEmpty() {
		payload["group_jid"] = evt.GroupJID.ToNonAD().String()
	}

	// Wrap in body structure
	body["event"] = "call.offer"
	body["timestamp"] = evt.Timestamp.Format(time.RFC3339)
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	body["payload"] = payload

	return body
}

// forwardCallOfferToWebhook forwards incoming call events to the configured webhook URLs
func forwardCallOfferToWebhook(ctx context.Context, evt *events.CallOffer, deviceID string, client *whatsmeow.Client, autoRejected bool) error {
	payload := createCallOfferPayload(ctx, evt, deviceID, client, autoRejected)
	return forwardPayloadToConfiguredWebhooks(ctx, payload, "call.offer")
}
