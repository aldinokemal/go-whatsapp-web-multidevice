package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

// Event types for webhook payload
const (
	EventTypeMessage         = "message"
	EventTypeMessageReaction = "message.reaction"
	EventTypeMessageRevoked  = "message.revoked"
	EventTypeMessageEdited   = "message.edited"
)

// WebhookEvent is the top-level structure for webhook payloads
type WebhookEvent struct {
	Event    string         `json:"event"`
	DeviceID string         `json:"device_id"`
	Payload  map[string]any `json:"payload"`
}

// forwardMessageToWebhook is a helper function to forward message event to webhook url
func forwardMessageToWebhook(ctx context.Context, client *whatsmeow.Client, evt *events.Message) error {
	webhookEvent, err := createWebhookEvent(ctx, client, evt)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"event":     webhookEvent.Event,
		"device_id": webhookEvent.DeviceID,
		"payload":   webhookEvent.Payload,
	}

	return forwardPayloadToConfiguredWebhooks(ctx, payload, "message event")
}

func createWebhookEvent(ctx context.Context, client *whatsmeow.Client, evt *events.Message) (*WebhookEvent, error) {
	webhookEvent := &WebhookEvent{
		Event:   EventTypeMessage,
		Payload: make(map[string]any),
	}

	// Set device_id
	if client != nil && client.Store != nil && client.Store.ID != nil {
		deviceJID := NormalizeJIDFromLID(ctx, client.Store.ID.ToNonAD(), client)
		webhookEvent.DeviceID = deviceJID.ToNonAD().String()
	}

	// Determine event type and build payload
	eventType, payload, err := buildEventPayload(ctx, client, evt)
	if err != nil {
		return nil, err
	}

	webhookEvent.Event = eventType
	webhookEvent.Payload = payload

	return webhookEvent, nil
}

func buildEventPayload(ctx context.Context, client *whatsmeow.Client, evt *events.Message) (string, map[string]any, error) {
	payload := make(map[string]any)

	// Common fields for all message types
	payload["id"] = evt.Info.ID
	payload["timestamp"] = evt.Info.Timestamp.Format(time.RFC3339)

	// Build from/from_lid fields
	buildFromFields(ctx, client, evt, payload)

	// Set from_name (pushname)
	if pushname := evt.Info.PushName; pushname != "" {
		payload["from_name"] = pushname
	}

	// Check for protocol messages (revoke, edit)
	if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		protocolType := protocolMessage.GetType().String()

		switch protocolType {
		case "REVOKE":
			if key := protocolMessage.GetKey(); key != nil {
				payload["revoked_message_id"] = key.GetID()
				payload["revoked_from_me"] = key.GetFromMe()
				if key.GetRemoteJID() != "" {
					payload["revoked_chat"] = key.GetRemoteJID()
				}
			}
			return EventTypeMessageRevoked, payload, nil

		case "MESSAGE_EDIT":
			if key := protocolMessage.GetKey(); key != nil {
				payload["original_message_id"] = key.GetID()
			}
			if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
				if editedText := editedMessage.GetExtendedTextMessage(); editedText != nil {
					payload["body"] = editedText.GetText()
				} else if editedConv := editedMessage.GetConversation(); editedConv != "" {
					payload["body"] = editedConv
				}
			}
			return EventTypeMessageEdited, payload, nil
		}
	}

	// Check for reaction message
	if reactionMessage := evt.Message.GetReactionMessage(); reactionMessage != nil {
		payload["reaction"] = reactionMessage.GetText()
		if key := reactionMessage.GetKey(); key != nil {
			payload["reacted_message_id"] = key.GetID()
		}
		return EventTypeMessageReaction, payload, nil
	}

	// Regular message - build body and media fields
	if err := buildMessageBody(ctx, client, evt, payload); err != nil {
		return "", nil, err
	}

	// Add optional fields
	if err := buildOptionalFields(ctx, client, evt, payload); err != nil {
		return "", nil, err
	}

	return EventTypeMessage, payload, nil
}

func buildFromFields(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) {
	// Always set chat_id from evt.Info.Chat (works for both private and group)
	payload["chat_id"] = evt.Info.Chat.ToNonAD().String()

	// Try to get from_lid from sender
	senderJID := evt.Info.Sender
	if senderJID.Server == "lid" {
		payload["from_lid"] = senderJID.ToNonAD().String()
	}

	// Resolve sender JID (convert LID to phone number if needed)
	normalizedSenderJID := NormalizeJIDFromLID(ctx, senderJID, client)
	payload["from"] = normalizedSenderJID.ToNonAD().String()
}

func buildMessageBody(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) error {
	message := utils.BuildEventMessage(evt)

	// Replace LID mentions with phone numbers in text
	if message.Text != "" && client != nil && client.Store != nil && client.Store.LIDs != nil {
		tags := regexp.MustCompile(`\B@\w+`).FindAllString(message.Text, -1)
		tagsMap := make(map[string]bool)
		for _, tag := range tags {
			tagsMap[tag] = true
		}
		for tag := range tagsMap {
			lid, err := types.ParseJID(tag[1:] + "@lid")
			if err != nil {
				logrus.Errorf("Error when parse jid: %v", err)
			} else {
				pn, err := client.Store.LIDs.GetPNForLID(ctx, lid)
				if err != nil {
					logrus.Errorf("Error when get pn for lid %s: %v", lid.ToNonAD().String(), err)
				}
				if !pn.IsEmpty() {
					message.Text = strings.Replace(message.Text, tag, fmt.Sprintf("@%s", pn.User), -1)
				}
			}
		}
		payload["body"] = message.Text
	} else if message.Text != "" {
		payload["body"] = message.Text
	}

	// Add reply context if present
	if message.RepliedId != "" {
		payload["replied_to_id"] = message.RepliedId
	}
	if message.QuotedMessage != "" {
		payload["quoted_body"] = message.QuotedMessage
	}

	return nil
}

func buildOptionalFields(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) error {
	if evt.IsViewOnce {
		payload["view_once"] = true
	}

	if utils.BuildForwarded(evt) {
		payload["forwarded"] = true
	}

	// Handle media types
	if err := buildMediaFields(ctx, client, evt, payload); err != nil {
		return err
	}

	// Handle other message types
	buildOtherMessageTypes(evt, payload)

	return nil
}

func buildMediaFields(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) error {
	if audioMedia := evt.Message.GetAudioMessage(); audioMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, client, config.PathMedia, audioMedia)
			if err != nil {
				logrus.Errorf("Failed to download audio from %s: %v", evt.Info.SourceString(), err)
				return pkgError.WebhookError(fmt.Sprintf("Failed to download audio: %v", err))
			}
			payload["audio"] = path
		} else {
			payload["audio"] = map[string]any{
				"url": audioMedia.GetURL(),
			}
		}
	}

	if documentMedia := evt.Message.GetDocumentMessage(); documentMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, client, config.PathMedia, documentMedia)
			if err != nil {
				logrus.Errorf("Failed to download document from %s: %v", evt.Info.SourceString(), err)
				return pkgError.WebhookError(fmt.Sprintf("Failed to download document: %v", err))
			}
			payload["document"] = path
		} else {
			payload["document"] = map[string]any{
				"url":      documentMedia.GetURL(),
				"filename": documentMedia.GetFileName(),
			}
		}
	}

	if imageMedia := evt.Message.GetImageMessage(); imageMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, client, config.PathMedia, imageMedia)
			if err != nil {
				logrus.Errorf("Failed to download image from %s: %v", evt.Info.SourceString(), err)
				return pkgError.WebhookError(fmt.Sprintf("Failed to download image: %v", err))
			}
			payload["image"] = path
		} else {
			payload["image"] = map[string]any{
				"url":     imageMedia.GetURL(),
				"caption": imageMedia.GetCaption(),
			}
		}
	}

	if stickerMedia := evt.Message.GetStickerMessage(); stickerMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, client, config.PathMedia, stickerMedia)
			if err != nil {
				logrus.Errorf("Failed to download sticker from %s: %v", evt.Info.SourceString(), err)
				return pkgError.WebhookError(fmt.Sprintf("Failed to download sticker: %v", err))
			}
			payload["sticker"] = path
		} else {
			payload["sticker"] = map[string]any{
				"url": stickerMedia.GetURL(),
			}
		}
	}

	if videoMedia := evt.Message.GetVideoMessage(); videoMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, client, config.PathMedia, videoMedia)
			if err != nil {
				logrus.Errorf("Failed to download video from %s: %v", evt.Info.SourceString(), err)
				return pkgError.WebhookError(fmt.Sprintf("Failed to download video: %v", err))
			}
			payload["video"] = path
		} else {
			payload["video"] = map[string]any{
				"url":     videoMedia.GetURL(),
				"caption": videoMedia.GetCaption(),
			}
		}
	}

	if ptvMedia := evt.Message.GetPtvMessage(); ptvMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, client, config.PathMedia, ptvMedia)
			if err != nil {
				logrus.Errorf("Failed to download video note from %s: %v", evt.Info.SourceString(), err)
				return pkgError.WebhookError(fmt.Sprintf("Failed to download video note: %v", err))
			}
			payload["video_note"] = path
		} else {
			payload["video_note"] = map[string]any{
				"url":     ptvMedia.GetURL(),
				"caption": ptvMedia.GetCaption(),
			}
		}
	}

	return nil
}

func buildOtherMessageTypes(evt *events.Message, payload map[string]any) {
	if contactMessage := evt.Message.GetContactMessage(); contactMessage != nil {
		payload["contact"] = contactMessage
	}

	if listMessage := evt.Message.GetListMessage(); listMessage != nil {
		payload["list"] = listMessage
	}

	if liveLocationMessage := evt.Message.GetLiveLocationMessage(); liveLocationMessage != nil {
		payload["live_location"] = liveLocationMessage
	}

	if locationMessage := evt.Message.GetLocationMessage(); locationMessage != nil {
		payload["location"] = locationMessage
	}

	if orderMessage := evt.Message.GetOrderMessage(); orderMessage != nil {
		payload["order"] = orderMessage
	}
}
