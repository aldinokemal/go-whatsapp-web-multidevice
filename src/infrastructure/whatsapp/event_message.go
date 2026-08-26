package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var reMention = regexp.MustCompile(`\B@\w+`)

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

type webhookContactPayload struct {
	DisplayName string `json:"displayName"`
	VCard       string `json:"vcard"`
	PhoneNumber string `json:"phone_number,omitempty"`
}

// forwardMessageToWebhook is a helper function to forward message event to webhook url
func forwardMessageToWebhook(ctx context.Context, client *whatsmeow.Client, evt *events.Message, preparedPoll ...*webhookPollPayload) error {
	webhookEvent, err := createWebhookEvent(ctx, client, evt, preparedPoll...)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"event":     webhookEvent.Event,
		"device_id": webhookEvent.DeviceID,
		"payload":   webhookEvent.Payload,
	}

	return forwardPayloadToConfiguredWebhooks(ctx, payload, webhookEvent.Event)
}

func isReactionMessage(evt *events.Message) bool {
	if evt == nil || evt.Message == nil {
		return false
	}

	return utils.UnwrapMessage(evt.Message).GetReactionMessage() != nil
}

func createWebhookEvent(ctx context.Context, client *whatsmeow.Client, evt *events.Message, preparedPoll ...*webhookPollPayload) (*WebhookEvent, error) {
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
	eventType, payload, err := buildEventPayload(ctx, client, evt, preparedPoll...)
	if err != nil {
		return nil, err
	}

	webhookEvent.Event = eventType
	webhookEvent.Payload = payload

	return webhookEvent, nil
}

func buildEventPayload(ctx context.Context, client *whatsmeow.Client, evt *events.Message, preparedPoll ...*webhookPollPayload) (string, map[string]any, error) {
	payload := make(map[string]any)

	msg := utils.UnwrapMessage(evt.Message)

	// Common fields for all message types
	payload["id"] = evt.Info.ID
	payload["timestamp"] = evt.Info.Timestamp.Format(time.RFC3339)
	payload["is_from_me"] = evt.Info.IsFromMe

	// Build from/from_lid fields
	buildFromFields(ctx, client, evt, payload)
	addSenderDisplayName(ctx, client, payload, evt.Info.IsFromMe, evt.Info.PushName)

	// Set from_name (pushname)
	if pushname := evt.Info.PushName; pushname != "" {
		payload["from_name"] = pushname
	}

	// Modern WhatsApp clients (LID-migrated accounts on recent app builds) wrap
	// message edits in a SecretEncryptedMessage with encType=MESSAGE_EDIT instead
	// of sending them as a plain ProtocolMessage{MESSAGE_EDIT}. Decrypt those
	// here using whatsmeow's existing helper, then fall through to the standard
	// MESSAGE_EDIT extraction path using the decrypted inner Message.
	if sem := msg.GetSecretEncryptedMessage(); sem != nil &&
		sem.GetSecretEncType() == waE2E.SecretEncryptedMessage_MESSAGE_EDIT &&
		client != nil {
		if decrypted, err := client.DecryptSecretEncryptedMessage(ctx, evt); err != nil {
			logrus.Warnf("Failed to decrypt SecretEncryptedMessage(MESSAGE_EDIT) for %s: %v", evt.Info.ID, err)
		} else if decrypted != nil {
			msg = utils.UnwrapMessage(decrypted)
		}
	}

	// Check for protocol messages (revoke, edit)
	if protocolMessage := msg.GetProtocolMessage(); protocolMessage != nil {
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
	if reactionMessage := msg.GetReactionMessage(); reactionMessage != nil {
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
	if err := buildOptionalFields(ctx, client, evt, msg, payload); err != nil {
		return "", nil, err
	}

	if len(preparedPoll) > 0 && preparedPoll[0] != nil {
		payload["poll"] = preparedPoll[0]
		if _, hasBody := payload["body"]; !hasBody {
			payload["body"] = pollWebhookBody(preparedPoll[0])
		}
	}

	if payloadHasNoRenderableContent(payload) && !hasRecognizedMessageType(msg) {
		// Neither a recognized message type nor any renderable payload field:
		// this is genuinely an unhandled kind (e.g. templates, interactive/
		// native-flow messages, polls, group invites, payment requests).
		// Downstream (Chatwoot) will render it as "(Unsupported message
		// type)" with no way to tell which WhatsApp message kind caused it.
		// Log which proto field is populated — never its value, since that
		// can carry customer message content, media URLs, and decryption
		// keys — so a future occurrence is diagnosable from logs alone.
		logrus.Warnf("Unrecognized message type from %s (id=%s): populated proto fields=%v", evt.Info.Sender.String(), evt.Info.ID, populatedMessageFields(msg))
	}

	return EventTypeMessage, payload, nil
}

// payloadHasNoRenderableContent reports whether none of the fields
// buildMessageBody/buildOptionalFields/buildMediaFields/buildOtherMessageTypes
// populate for a recognized message type are present. Kept in sync with the
// field names those functions write to payload.
//
// This alone is not sufficient to conclude the message type is unrecognized:
// a known media type (image/audio/video/document/sticker/video_note) with a
// failed/expired download leaves its field unset here too, even though
// buildMediaFields correctly identified it. Callers must also check
// hasRecognizedMessageType before treating this as "unhandled type".
func payloadHasNoRenderableContent(payload map[string]any) bool {
	renderableKeys := []string{
		"body",
		"image", "audio", "video", "video_note", "document", "sticker",
		"contact", "contacts_array", "list", "live_location", "location", "order",
	}
	for _, key := range renderableKeys {
		if _, ok := payload[key]; ok {
			return false
		}
	}
	return true
}

// hasRecognizedMessageType reports whether msg is one of the kinds
// buildMediaFields/buildOtherMessageTypes know how to handle, independent of
// whether extraction actually produced payload content (e.g. a media
// download can fail while the type itself is still recognized). Keep this in
// sync with the Get*Message() checks in those two functions.
func hasRecognizedMessageType(msg *waE2E.Message) bool {
	switch {
	case msg.GetAudioMessage() != nil,
		msg.GetDocumentMessage() != nil,
		msg.GetImageMessage() != nil,
		msg.GetStickerMessage() != nil,
		msg.GetVideoMessage() != nil,
		msg.GetPtvMessage() != nil,
		msg.GetContactMessage() != nil,
		msg.GetContactsArrayMessage() != nil,
		msg.GetListMessage() != nil,
		msg.GetLiveLocationMessage() != nil,
		msg.GetLocationMessage() != nil,
		msg.GetOrderMessage() != nil:
		return true
	default:
		return false
	}
}

// populatedMessageFields lists the proto field names set on msg (e.g.
// "interactiveMessage", "templateMessage"), using reflection purely for
// field descriptors — never field values — so this is safe to log at warn
// level even though the message itself may carry customer content.
func populatedMessageFields(msg *waE2E.Message) []string {
	if msg == nil {
		return nil
	}
	var names []string
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		names = append(names, string(fd.Name()))
		return true
	})
	return names
}

func buildFromFields(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) {
	chatJID := evt.Info.Chat.ToNonAD()
	if chatJID.Server == "lid" {
		payload["chat_lid"] = chatJID.String()
		chatJID = NormalizeJIDFromLID(ctx, chatJID, client).ToNonAD()
	}
	payload["chat_id"] = chatJID.String()

	senderJID := evt.Info.Sender
	if senderJID.Server == "lid" {
		payload["from_lid"] = senderJID.ToNonAD().String()
	}

	normalizedSenderJID := NormalizeJIDFromLID(ctx, senderJID, client)
	payload["from"] = normalizedSenderJID.ToNonAD().String()
}

func buildMessageBody(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) error {
	message := utils.BuildEventMessage(evt)

	// Replace LID mentions with phone numbers in text
	if message.Text != "" && client != nil && client.Store != nil && client.Store.LIDs != nil {
		tags := reMention.FindAllString(message.Text, -1)
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

	// Fallback: extract caption from media messages if no text body was set
	if _, hasBody := payload["body"]; !hasBody {
		msg := utils.UnwrapMessage(evt.Message)
		if caption := utils.ExtractMediaCaption(msg); caption != "" {
			payload["body"] = caption
		}
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

func buildOptionalFields(ctx context.Context, client *whatsmeow.Client, evt *events.Message, msg *waE2E.Message, payload map[string]any) error {
	if evt.IsViewOnce {
		payload["view_once"] = true
	}

	if utils.BuildForwarded(evt) {
		payload["forwarded"] = true
	}

	if referral := utils.ExtractExternalAdReply(msg); referral != nil {
		payload["referral"] = referral
	}

	if err := buildMediaFields(ctx, client, msg, payload); err != nil {
		return err
	}

	buildOtherMessageTypes(msg, payload)

	return nil
}

func buildMediaFields(ctx context.Context, client *whatsmeow.Client, msg *waE2E.Message, payload map[string]any) error {
	if audioMedia := msg.GetAudioMessage(); audioMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, audioMedia)
			if err != nil {
				// Media expired/unavailable: skip the attachment but keep
				// forwarding the message so its body/caption still reaches
				// downstream consumers, instead of dropping the whole event.
				logrus.Errorf("Failed to download audio: %v", err)
			} else {
				payload["audio"] = extracted.MediaPath
			}
		} else {
			payload["audio"] = map[string]any{
				"url": audioMedia.GetURL(),
			}
		}
	}

	if documentMedia := msg.GetDocumentMessage(); documentMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, documentMedia)
			if err != nil {
				// Media expired/unavailable: skip the attachment but keep
				// forwarding the message so its body/caption still reaches
				// downstream consumers, instead of dropping the whole event.
				logrus.Errorf("Failed to download document: %v", err)
			} else {
				payload["document"] = buildAutoDownloadPayload(extracted)
			}
		} else {
			payload["document"] = map[string]any{
				"url":      documentMedia.GetURL(),
				"filename": documentMedia.GetFileName(),
			}
		}
	}

	if imageMedia := msg.GetImageMessage(); imageMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, imageMedia)
			if err != nil {
				// Media expired/unavailable: skip the attachment but keep
				// forwarding the message so its body/caption still reaches
				// downstream consumers, instead of dropping the whole event.
				logrus.Errorf("Failed to download image: %v", err)
			} else {
				payload["image"] = buildAutoDownloadPayload(extracted)
			}
		} else {
			payload["image"] = map[string]any{
				"url":     imageMedia.GetURL(),
				"caption": imageMedia.GetCaption(),
			}
		}
	}

	if stickerMedia := msg.GetStickerMessage(); stickerMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, stickerMedia)
			if err != nil {
				// Media expired/unavailable: skip the attachment but keep
				// forwarding the message so its body/caption still reaches
				// downstream consumers, instead of dropping the whole event.
				logrus.Errorf("Failed to download sticker: %v", err)
			} else {
				payload["sticker"] = extracted.MediaPath
			}
		} else {
			payload["sticker"] = map[string]any{
				"url": stickerMedia.GetURL(),
			}
		}
	}

	if videoMedia := msg.GetVideoMessage(); videoMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, videoMedia)
			if err != nil {
				// Media expired/unavailable: skip the attachment but keep
				// forwarding the message so its body/caption still reaches
				// downstream consumers, instead of dropping the whole event.
				logrus.Errorf("Failed to download video: %v", err)
			} else {
				payload["video"] = buildAutoDownloadPayload(extracted)
			}
		} else {
			payload["video"] = map[string]any{
				"url":     videoMedia.GetURL(),
				"caption": videoMedia.GetCaption(),
			}
		}
	}

	if ptvMedia := msg.GetPtvMessage(); ptvMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, ptvMedia)
			if err != nil {
				// Media expired/unavailable: skip the attachment but keep
				// forwarding the message so its body/caption still reaches
				// downstream consumers, instead of dropping the whole event.
				logrus.Errorf("Failed to download video note: %v", err)
			} else {
				payload["video_note"] = buildAutoDownloadPayload(extracted)
			}
		} else {
			payload["video_note"] = map[string]any{
				"url":     ptvMedia.GetURL(),
				"caption": ptvMedia.GetCaption(),
			}
		}
	}

	if mediaPaths := collectInteractiveMedia(ctx, client, msg.GetInteractiveMessage()); len(mediaPaths) > 0 {
		// Stored separately from the singular image/video/document fields
		// above (rather than reusing them) because a carousel can carry
		// media on *every* card: reusing those fields would have each card's
		// extraction silently overwrite the previous one, dropping all but
		// the last image. []string (not a proto or map) survives the JSON
		// round-trip the Chatwoot forward retry queue performs, same
		// reasoning as the "interactive" field below.
		payload["interactive_media"] = mediaPaths
	}

	return nil
}

// collectInteractiveMedia extracts every image/video/document reachable from
// an InteractiveMessage: the root header (common for marketing CTA messages —
// a product photo above the button) and, recursively, each carousel card's
// own header (a carousel's cards are themselves full InteractiveMessage
// values). Without this, buildMediaFields only looks at the top-level
// message — which is empty for InteractiveMessage, since it's a distinct
// oneof case from GetImageMessage()/GetVideoMessage()/GetDocumentMessage() —
// so header media silently never reached Chatwoot. Returns nil when
// WHATSAPP_AUTO_DOWNLOAD_MEDIA is disabled: as with top-level media, a
// URL-only reference isn't something Chatwoot can fetch itself, so there's
// nothing usable to attach (see extractMediaPath in webhook_forward.go).
// header.GetLocationMessage()/GetProductMessage()/GetJPEGThumbnail() are not
// handled: no existing payload field/attachment path covers them here.
func collectInteractiveMedia(ctx context.Context, client *whatsmeow.Client, im *waE2E.InteractiveMessage) []string {
	if im == nil || !config.WhatsappAutoDownloadMedia {
		return nil
	}

	var paths []string
	paths = append(paths, extractInteractiveHeaderMediaPaths(ctx, client, im.GetHeader())...)
	for _, card := range im.GetCarouselMessage().GetCards() {
		paths = append(paths, collectInteractiveMedia(ctx, client, card)...)
	}
	return paths
}

func extractInteractiveHeaderMediaPaths(ctx context.Context, client *whatsmeow.Client, header *waE2E.InteractiveMessage_Header) []string {
	if header == nil {
		return nil
	}

	var paths []string
	if imageMedia := header.GetImageMessage(); imageMedia != nil {
		if extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, imageMedia); err != nil {
			logrus.Errorf("Failed to download interactive header image: %v", err)
		} else {
			paths = append(paths, extracted.MediaPath)
		}
	}
	if videoMedia := header.GetVideoMessage(); videoMedia != nil {
		if extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, videoMedia); err != nil {
			logrus.Errorf("Failed to download interactive header video: %v", err)
		} else {
			paths = append(paths, extracted.MediaPath)
		}
	}
	if documentMedia := header.GetDocumentMessage(); documentMedia != nil {
		if extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, documentMedia); err != nil {
			logrus.Errorf("Failed to download interactive header document: %v", err)
		} else {
			paths = append(paths, extracted.MediaPath)
		}
	}
	return paths
}

// buildAutoDownloadPayload builds the media payload for auto-downloaded media.
// Returns just the path string if no caption (backward compatible), or a map with path+caption.
func buildAutoDownloadPayload(extracted utils.ExtractedMedia) any {
	if extracted.Caption != "" {
		return map[string]any{
			"path":    extracted.MediaPath,
			"caption": extracted.Caption,
		}
	}
	return extracted.MediaPath
}

func buildOtherMessageTypes(msg *waE2E.Message, payload map[string]any) {
	if contactMessage := msg.GetContactMessage(); contactMessage != nil {
		payload["contact"] = buildWebhookContactPayload(contactMessage)
	}

	if contactsArrayMessage := msg.GetContactsArrayMessage(); contactsArrayMessage != nil {
		payload["contacts_array"] = buildWebhookContactsArrayPayload(contactsArrayMessage.GetContacts())
	}

	if listMessage := msg.GetListMessage(); listMessage != nil {
		payload["list"] = listMessage
	}

	if liveLocationMessage := msg.GetLiveLocationMessage(); liveLocationMessage != nil {
		payload["live_location"] = liveLocationMessage
	}

	if locationMessage := msg.GetLocationMessage(); locationMessage != nil {
		payload["location"] = locationMessage
	}

	if orderMessage := msg.GetOrderMessage(); orderMessage != nil {
		payload["order"] = orderMessage
	}

	if interactiveMessage := msg.GetInteractiveMessage(); interactiveMessage != nil {
		// Business/Cloud API messages with native buttons (cta_url "visit
		// website", cta_call, single/multi-select, etc.) arrive as this type
		// instead of Conversation/ExtendedTextMessage, so they carried no
		// body text and rendered as "(Unsupported message type)" in Chatwoot.
		//
		// Rendered to a string here, not stored as the raw proto: a failed
		// live forward gets re-marshaled through JSON for the retry queue
		// (see enqueueChatwootForwardRetry/replayChatwootForwardEvent), which
		// turns *waE2E.InteractiveMessage into a generic map[string]any —
		// the type assertion in extractStructuredMessageContent would then
		// miss on retry and silently downgrade to the generic sentinel,
		// losing the CTA label/URL/phone/code the live path just extracted.
		payload["interactive"] = formatInteractiveMessageSummary(interactiveMessage)
	}
}

func buildWebhookContactPayload(contact *waE2E.ContactMessage) webhookContactPayload {
	if contact == nil {
		return webhookContactPayload{}
	}

	vcard := contact.GetVcard()
	return webhookContactPayload{
		DisplayName: contact.GetDisplayName(),
		VCard:       vcard,
		PhoneNumber: utils.ExtractPhoneFromVCard(vcard),
	}
}

func buildWebhookContactsArrayPayload(contacts []*waE2E.ContactMessage) []webhookContactPayload {
	result := make([]webhookContactPayload, 0, len(contacts))
	for _, contact := range contacts {
		if contact == nil {
			continue
		}
		result = append(result, buildWebhookContactPayload(contact))
	}
	return result
}
