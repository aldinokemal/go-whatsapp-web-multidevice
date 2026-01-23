package whatsapp

import (
	"context"
	"time"

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

// forwardReceiptToWebhook forwards message acknowledgement events to the configured webhook URLs.
//
// IMPORTANT: We only forward receipts from the primary device (Device == 0).
// WhatsApp sends separate receipt events for each linked device (phone, web, desktop, etc.)
// of a user. For example, if a user has 3 devices, you would receive 3 "delivered" receipts
// for the same message. To avoid duplicate webhooks and simplify downstream processing,
// we only send the receipt from the primary device (Device == 0).
//
// If you need receipts from all devices in the future, remove the Device == 0 check below.
func forwardReceiptToWebhook(ctx context.Context, evt *events.Receipt, deviceID string, client *whatsmeow.Client) error {
	// Only forward receipts from the primary device to avoid duplicates.
	// See function comment above for detailed explanation.
	if evt.Sender.Device != 0 {
		logrus.Debugf("Skipping receipt webhook for linked device %d (only primary device receipts are forwarded)", evt.Sender.Device)
		return nil
	}

	payload := createReceiptPayload(ctx, evt, deviceID, client)
	return forwardPayloadToConfiguredWebhooks(ctx, payload, "message.ack")
}
