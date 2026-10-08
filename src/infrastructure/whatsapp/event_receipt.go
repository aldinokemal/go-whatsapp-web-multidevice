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
	addSenderDisplayName(ctx, client, payload, false, "")

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
// Receipts are filtered by shouldForwardReceipt; see that function for the
// per-device rules.
func forwardReceiptToWebhook(ctx context.Context, evt *events.Receipt, deviceID string, client *whatsmeow.Client) error {
	if !shouldForwardReceipt(evt) {
		logrus.Debugf("Skipping %s receipt webhook from linked device %d of %s", evt.Type, evt.Sender.Device, evt.Sender.ToNonAD())
		return nil
	}

	payload := createReceiptPayload(ctx, evt, deviceID, client)
	return forwardPayloadToConfiguredWebhooks(ctx, payload, "message.ack")
}

// shouldForwardReceipt reports whether a receipt event should be forwarded.
//
// WhatsApp sends separate receipt events for each linked device (phone, web,
// desktop, etc.) of a contact. For example, if a contact has 3 devices, you
// would receive 3 "delivered" receipts for the same message. To avoid duplicate
// webhooks we only forward the receipt from the primary device (Device == 0).
//
// The exceptions are read and played receipts. Unlike delivery receipts, only
// the device where the chat was opened (or the media was played) sends them, so
// they are not duplicated per device. Dropping them would hide reads that did
// not happen on the phone:
//   - a contact reading our message on WhatsApp Web, Desktop or another linked
//     device (common for WhatsApp Business accounts that answer from a PC);
//   - the current account reading a chat on one of its own linked devices
//     ("read", or "read-self" when read receipts are disabled).
func shouldForwardReceipt(evt *events.Receipt) bool {
	if evt.Sender.Device == 0 {
		return true
	}
	switch evt.Type {
	case types.ReceiptTypeRead, types.ReceiptTypePlayed:
		return true
	case types.ReceiptTypeReadSelf:
		return evt.IsFromMe
	default:
		return false
	}
}
