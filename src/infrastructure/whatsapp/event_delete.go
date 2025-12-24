package whatsapp

import (
	"context"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// forwardDeleteToWebhook sends a delete event to webhook
func forwardDeleteToWebhook(ctx context.Context, evt *events.DeleteForMe, message *domainChatStorage.Message, deviceID string, client *whatsmeow.Client) error {
	payload, err := createDeletePayload(ctx, evt, message, deviceID, client)
	if err != nil {
		return err
	}

	return forwardPayloadToConfiguredWebhooks(ctx, payload, "delete event")
}

// createDeletePayload creates a webhook payload for delete events
func createDeletePayload(ctx context.Context, evt *events.DeleteForMe, message *domainChatStorage.Message, deviceID string, client *whatsmeow.Client) (map[string]any, error) {
	body := make(map[string]any)

	// Basic delete event information
	body["action"] = "event.delete_for_me"
	body["deleted_message_id"] = evt.MessageID
	body["timestamp"] = time.Now().Format(time.RFC3339)
	if deviceID != "" {
		body["device_id"] = deviceID
	}

	// Resolve sender JID (convert LID to phone number if needed)
	normalizedSenderJID := NormalizeJIDFromLID(ctx, evt.SenderJID, client)
	body["from"] = normalizedSenderJID.ToNonAD().String()
	body["sender_id"] = normalizedSenderJID.User

	// Include original message information if available
	if message != nil {
		body["chat_id"] = message.ChatJID
		body["original_content"] = message.Content
		body["original_sender"] = message.Sender
		body["original_timestamp"] = message.Timestamp.Format(time.RFC3339)
		body["was_from_me"] = message.IsFromMe

		if message.MediaType != "" {
			body["original_media_type"] = message.MediaType
			body["original_filename"] = message.Filename
		}
	}

	return body, nil
}
