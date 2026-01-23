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

	return forwardPayloadToConfiguredWebhooks(ctx, payload, "message.deleted")
}

// createDeletePayload creates a webhook payload for delete events
func createDeletePayload(ctx context.Context, evt *events.DeleteForMe, message *domainChatStorage.Message, deviceID string, client *whatsmeow.Client) (map[string]any, error) {
	body := make(map[string]any)
	payload := make(map[string]any)

	payload["deleted_message_id"] = evt.MessageID
	payload["timestamp"] = time.Now().Format(time.RFC3339)

	// Resolve sender JID (convert LID to phone number if needed)
	normalizedSenderJID := NormalizeJIDFromLID(ctx, evt.SenderJID, client)
	payload["from"] = normalizedSenderJID.ToNonAD().String()

	// Include original message information if available
	if message != nil {
		payload["chat_id"] = message.ChatJID
		payload["original_content"] = message.Content
		payload["original_sender"] = message.Sender
		payload["original_timestamp"] = message.Timestamp.Format(time.RFC3339)
		payload["was_from_me"] = message.IsFromMe

		if message.MediaType != "" {
			payload["original_media_type"] = message.MediaType
			payload["original_filename"] = message.Filename
		}
	}

	body["event"] = "message.deleted"
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	body["payload"] = payload

	return body, nil
}
