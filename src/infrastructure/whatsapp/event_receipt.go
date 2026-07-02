package whatsapp

import (
	"context"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func getReceiptTypeDescription(evt types.ReceiptType) string {
	switch evt {
	case types.ReceiptTypeDelivered:
		return "means the message was delivered to the device (but the user might not have noticed)."
	case types.ReceiptTypeSender:
		return "sent by your other devices when a message you sent is delivered to them."
	case types.ReceiptTypeRetry:
		return "the message was delivered to the device, but decrypting the message failed."
	case types.ReceiptTypeRead:
		return "the user opened the chat and saw the message."
	case types.ReceiptTypeReadSelf:
		return "the current user read a message from a different device, and has read receipts disabled in privacy settings."
	case types.ReceiptTypePlayed:
		return `This is dispatched for both incoming and outgoing messages when played. If the current user opened the media,
	it means the media should be removed from all devices. If a recipient opened the media, it's just a notification
	for the sender that the media was viewed.`
	case types.ReceiptTypePlayedSelf:
		return `probably means the current user opened a view-once media message from a different device,
	and has read receipts disabled in privacy settings.`
	default:
		return "unknown receipt type"
	}
}

// createReceiptPayload creates a webhook payload for message acknowledgement (receipt) events
func createReceiptPayload(ctx context.Context, evt *events.Receipt, deviceID string, client *whatsmeow.Client) map[string]any {
	body := make(map[string]any)
	payload := make(map[string]any)

	// Add message IDs
	if len(evt.MessageIDs) > 0 {
		payload["ids"] = evt.MessageIDs
	}

	// Add chat_id
	payload["chat_id"] = evt.Chat.ToNonAD().String()

	// Build from/from_lid fields from sender
	senderJID := evt.Sender

	if senderJID.Server == "lid" {
		payload["from_lid"] = senderJID.ToNonAD().String()
	}

	// Resolve sender JID (convert LID to phone number if needed)
	normalizedSenderJID := NormalizeJIDFromLID(ctx, senderJID, client)
	payload["from"] = normalizedSenderJID.ToNonAD().String()

	// Receipt type
	if evt.Type == types.ReceiptTypeDelivered {
		payload["receipt_type"] = "delivered"
	} else {
		payload["receipt_type"] = string(evt.Type)
	}
	payload["receipt_type_description"] = getReceiptTypeDescription(evt.Type)

	// Wrap in body structure
	body["event"] = "message.ack"
	body["timestamp"] = evt.Timestamp.Format(time.RFC3339)
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	body["payload"] = payload

	return body
}

// shouldForwardReceipt reports whether a receipt event should be forwarded to webhooks.
//
// By default only receipts sent from the primary device (Device == 0) are forwarded.
// WhatsApp emits a separate receipt event for each linked device (phone, web, desktop,
// etc.) of a user, so forwarding everything would duplicate webhooks for users whose
// phone is online.
//
// However, when the counterpart uses WhatsApp mainly through linked/companion devices
// (WhatsApp Web, Desktop, another gateway instance) and their primary phone stays
// offline, every receipt they emit comes from a non-zero device and would be dropped —
// delivered/read status then never reaches the webhook at all. Enabling
// --webhook-all-device-receipts (env WHATSAPP_WEBHOOK_ALL_DEVICE_RECEIPTS) forwards
// receipts from every device; downstream consumers should apply receipts idempotently
// (e.g. a monotonic sent < delivered < read merge), which makes duplicates harmless.
func shouldForwardReceipt(evt *events.Receipt) bool {
	return config.WhatsappWebhookAllDeviceReceipts || evt.Sender.Device == 0
}

// forwardReceiptToWebhook forwards message acknowledgement events to the configured webhook URLs.
func forwardReceiptToWebhook(ctx context.Context, evt *events.Receipt, deviceID string, client *whatsmeow.Client) error {
	if !shouldForwardReceipt(evt) {
		logrus.Debugf("Skipping receipt webhook for linked device %d (enable --webhook-all-device-receipts to forward receipts from all devices)", evt.Sender.Device)
		return nil
	}

	payload := createReceiptPayload(ctx, evt, deviceID, client)
	return forwardPayloadToConfiguredWebhooks(ctx, payload, "message.ack")
}
