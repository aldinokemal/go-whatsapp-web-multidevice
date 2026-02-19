package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// forwardHistoryToWebhook is a helper function to forward message history event to webhook url
func forwardHistoryToWebhook(ctx context.Context, evt *events.HistorySync, client *whatsmeow.Client) error {
	logrus.Infof("Forwarding message history event to %d configured webhook(s)", len(config.WhatsappWebhook))
	history := createHistoryMessagePayload(ctx, evt, client)
	if len(history) == 0 {
		return nil
	}

	payload := map[string]any{
		"event":    "message.history",
		"messages": history,
	}

	for _, url := range config.WhatsappWebhook {
		if err := submitWebhook(ctx, payload, url); err != nil {
			return err
		}
	}

	logrus.Info("Message history event forwarded to webhook")
	return nil
}

func createHistoryMessagePayload(ctx context.Context, evt *events.HistorySync, client *whatsmeow.Client) []map[string]any {
	payload := []map[string]any{}
	for _, conversation := range evt.Data.Conversations {
		for _, historySyncMsg := range conversation.Messages {
			if webMessageInfo := historySyncMsg.GetMessage(); webMessageInfo != nil {
				msgHistory, err := createMessagePayloadFromHistory(ctx, webMessageInfo, client)
				if err != nil {
					logrus.Errorf("Error when create message payload from history: %v", err)
				} else {
					payload = append(payload, msgHistory)
				}
			}
		}
	}
	return payload
}

func createMessagePayloadFromHistory(ctx context.Context, evt *waWeb.WebMessageInfo, client *whatsmeow.Client) (map[string]any, error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("panic recovered in createMessagePayloadFromHistory: %v", r)
		}
	}()

	payload := make(map[string]any)

	payload["is_from_me"] = evt.GetKey().GetFromMe()
	chatId := evt.GetKey().GetRemoteJID()

	if strings.HasSuffix(chatId, "@lid") {
		payload["from_lid"] = chatId
		lid, err := types.ParseJID(chatId)
		if err != nil {
			logrus.Errorf("Error when parse jid: %v", err)
		} else {
			if client == nil {
				logrus.Error("client is nil")
			} else if client.Store == nil {
				logrus.Error("client Store is nil")
			} else if client.Store.LIDs == nil {
				logrus.Error("client Store LIDs is nil")
			} else {
				pn, err := client.Store.LIDs.GetPNForLID(ctx, lid)
				if err != nil {
					logrus.Errorf("Error when get pn for lid %s: %v", lid.String(), err)
				}
				if !pn.IsEmpty() {
					if from_group := strings.HasSuffix(chatId, "@g.us"); from_group {
						chatId = fmt.Sprintf("%s in %s", pn.String(), chatId)
					} else {
						chatId = pn.String()
					}
				}
			}
		}
	}

	participantId := evt.GetKey().GetParticipant()

	if strings.HasSuffix(chatId, "@g.us") && chatId == participantId || participantId == "" {
		if participant := evt.GetParticipant(); participant != "" {
			participantId = participant
		}
	}

	if strings.HasSuffix(participantId, "@lid") {
		payload["from_lid"] = participantId
		lid, err := types.ParseJID(participantId)
		if err != nil {
			logrus.Errorf("Error when parse jid: %v", err)
		} else {
			pn, err := client.Store.LIDs.GetPNForLID(ctx, lid)
			if err != nil {
				logrus.Errorf("Error when get pn for lid %s: %v", lid.String(), err)
			}
			if !pn.IsEmpty() {
				participantId = pn.String()
			}
		}
	}

	payload["chat_id"] = chatId

	if strings.HasSuffix(chatId, "@g.us") {
		payload["from"] = participantId
	} else {
		payload["from"] = chatId
	}

	message := utils.BuildEventHistoryMessage(evt)

	if message.ID != "" {
		payload["id"] = message.ID

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
					logrus.Errorf("Error when get pn for lid %s: %v", lid.String(), err)
				}
				if !pn.IsEmpty() {
					message.Text = strings.ReplaceAll(message.Text, tag, fmt.Sprintf("@%s", pn.User))
				}
			}
		}

		if message.Text != "" {
			payload["body"] = message.Text

			if message.QuotedMessage != "" {
				payload["replied_to_id"] = message.RepliedId
				payload["quoted_body"] = message.QuotedMessage
			}
		}
	}

	if pushname := evt.GetPushName(); pushname != "" {
		payload["from_name"] = pushname
	}

	waReaction := utils.BuildEventHistoryReaction(evt)

	if waReaction.Message != "" {
		payload["reacted_message_id"] = waReaction.ID
		payload["reaction"] = waReaction.Message
	}

	if isViewOnce := evt.Message.GetViewOnceMessage(); isViewOnce != nil {
		payload["view_once"] = isViewOnce != nil

	}

	forwarded := utils.BuildEventHistoryForwarded(evt)

	if forwarded {
		payload["forwarded"] = forwarded
	}

	if timestamp := evt.MessageTimestamp; timestamp != nil {
		payload["timestamp"] = timestamp
	}

	// Handle protocol messages (revoke, etc.)
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
		case "MESSAGE_EDIT":
			if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
				payload["original_message_id"] = protocolMessage.Key.ID

				if caption := extractCaption(editedMessage); caption != "" {
					payload["caption"] = caption
				} else if text := extractText(editedMessage); text != "" {
					payload["body"] = text
				}
			}
		}
	}

	if audioMedia := evt.Message.GetAudioMessage(); audioMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, audioMedia)
		if err != nil {
			logrus.Errorf("Failed to download audio: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download audio: %v", err))
		}
		payload["audio"] = path.MediaPath
	}

	if contactMessage := evt.Message.GetContactMessage(); contactMessage != nil {
		payload["contact"] = contactMessage
	}

	if contactsMessage := evt.Message.GetContactsArrayMessage(); contactsMessage != nil {
		payload["contact_list"] = contactsMessage
	}

	if documentMedia := evt.Message.GetDocumentMessage(); documentMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, documentMedia)
		if err != nil {
			logrus.Errorf("Failed to download document: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download document: %v", err))
		}
		payload["document"] = path.MediaPath
		payload["filename"] = path.Title

		if path.Caption != "" {
			payload["caption"] = path.Caption
		}
	}

	if imageMedia := evt.Message.GetImageMessage(); imageMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, imageMedia)
		if err != nil {
			logrus.Errorf("Failed to download image: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download image: %v", err))
		}
		payload["image"] = path.MediaPath

		if path.Caption != "" {
			payload["caption"] = path.Caption
		}
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

	if stickerMedia := evt.Message.GetStickerMessage(); stickerMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, stickerMedia)
		if err != nil {
			logrus.Errorf("Failed to download sticker: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download sticker: %v", err))
		}
		payload["sticker"] = path.MediaPath
	}

	if videoMedia := evt.Message.GetVideoMessage(); videoMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, videoMedia)
		if err != nil {
			logrus.Errorf("Failed to download video: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download video: %v", err))
		}
		payload["video"] = path.MediaPath

		if path.Caption != "" {
			payload["caption"] = path.Caption
		}
	}

	if ptvMedia := evt.Message.GetPtvMessage(); ptvMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, ptvMedia)
		if err != nil {
			logrus.Errorf("Failed to download video_note: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download video_note: %v", err))
		}
		payload["video_note"] = path.MediaPath

		if path.Caption != "" {
			payload["caption"] = path.Caption
		}
	}

	if pollMessage := evt.Message.GetPollCreationMessage(); pollMessage != nil {
		payload["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV2(); pollMessage != nil {
		payload["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV3(); pollMessage != nil {
		payload["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV4(); pollMessage != nil {
		payload["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV5(); pollMessage != nil {
		payload["poll"] = pollMessage
	}

	body := make(map[string]any)
	body["payload"] = payload

	return body, nil
}

func extractCaption(msg *waE2E.Message) string {
	if imgCaption := msg.GetImageMessage().GetCaption(); imgCaption != "" {
		return imgCaption
	} else if vidCaption := msg.GetVideoMessage().GetCaption(); vidCaption != "" {
		return vidCaption
	} else if docCaption := msg.GetDocumentWithCaptionMessage().GetMessage().GetDocumentMessage().GetCaption(); docCaption != "" {
		return docCaption
	} else if ptvCaption := msg.GetPtvMessage().GetCaption(); ptvCaption != "" {
		return ptvCaption
	}

	return ""
}

func extractText(msg *waE2E.Message) string {
	if textMessage := msg.GetExtendedTextMessage(); textMessage != nil {
		return textMessage.GetText()
	} else if convMessage := msg.GetConversation(); convMessage != "" {
		return convMessage
	}

	return ""
}
