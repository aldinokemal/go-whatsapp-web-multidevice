package whatsapp

import (
	"context"
	"time"

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
func createReceiptPayload(evt *events.Receipt) map[string]any {
	body := make(map[string]any)

	// Create payload structure matching the expected format
	payload := make(map[string]any)

	// Add message ID (use first message ID if multiple)
	if len(evt.MessageIDs) > 0 {
		payload["ids"] = evt.MessageIDs
	}

	// Add from field (the chat where the message was sent)
	payload["chat_id"] = evt.Chat
	payload["sender_id"] = evt.Sender
	payload["from"] = evt.SourceString()

	if evt.Type == types.ReceiptTypeDelivered {
		payload["receipt_type"] = "delivered"
	} else {
		payload["receipt_type"] = evt.Type
	}
	payload["receipt_type_description"] = getReceiptTypeDescription(evt.Type)

	// Wrap in payload structure
	body["payload"] = payload

	// Add metadata for webhook processing
	body["event"] = "message.ack"
	body["timestamp"] = evt.Timestamp.Format(time.RFC3339)

	return body
}

// forwardReceiptToWebhook forwards message acknowledgement events to the configured webhook URLs
func forwardReceiptToWebhook(ctx context.Context, evt *events.Receipt) error {
	payload := createReceiptPayload(evt)
	return forwardPayloadToConfiguredWebhooks(ctx, payload, "message ack event")
}
