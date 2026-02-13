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
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// forwardHistoryToWebhook is a helper function to forward message history event to webhook url
func forwardHistoryToWebhook(ctx context.Context, evt *events.HistorySync) error {
	logrus.Infof("Forwarding message history event to %d configured webhook(s)", len(config.WhatsappWebhook))
	history := createHistoryMessagePayload(ctx, evt)
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

func createHistoryMessagePayload(ctx context.Context, evt *events.HistorySync) []map[string]any {
	payload := []map[string]any{}
	for _, conversation := range evt.Data.Conversations {
		for _, historySyncMsg := range conversation.Messages {
			if webMessageInfo := historySyncMsg.GetMessage(); webMessageInfo != nil {
				msgHistory, err := createMessagePayloadFromHistory(ctx, webMessageInfo)
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

func createMessagePayloadFromHistory(ctx context.Context, evt *waWeb.WebMessageInfo) (map[string]any, error) {
	body := make(map[string]any)
	chatId := evt.GetKey().GetRemoteJID()
	participantId := evt.GetKey().GetParticipant()

	if strings.HasSuffix(chatId, "@lid") {
		body["from_lid"] = chatId
		lid, err := types.ParseJID(chatId)
		if err != nil {
			logrus.Errorf("Error when parse jid: %v", err)
		} else {
			pn, err := cli.Store.LIDs.GetPNForLID(ctx, lid)
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

	body["chat_id"] = chatId
	body["sender_id"] = chatId

	if strings.HasSuffix(participantId, "@lid") {
		body["from_lid"] = participantId
		lid, err := types.ParseJID(participantId)
		if err != nil {
			logrus.Errorf("Error when parse jid: %v", err)
		} else {
			pn, err := cli.Store.LIDs.GetPNForLID(ctx, lid)
			if err != nil {
				logrus.Errorf("Error when get pn for lid %s: %v", lid.String(), err)
			}
			if !pn.IsEmpty() {
				participantId = pn.String()
			}
		}
	}

	body["from"] = chatId

	if participantId != "" {
		body["from"] = chatId + " in " + participantId
		body["sender_id"] = participantId
	}

	message := utils.BuildEventHistoryMessage(evt)

	if message.ID != "" {
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
				pn, err := cli.Store.LIDs.GetPNForLID(ctx, lid)
				if err != nil {
					logrus.Errorf("Error when get pn for lid %s: %v", lid.String(), err)
				}
				if !pn.IsEmpty() {
					message.Text = strings.Replace(message.Text, tag, fmt.Sprintf("@%s", pn.User), -1)
				}
			}
		}
		body["message"] = message
	}

	waReaction := utils.BuildEventHistoryReaction(evt)
	forwarded := utils.BuildEventHistoryForwarded(evt)

	if pushname := evt.GetPushName(); pushname != "" {
		body["pushname"] = pushname
	}
	if waReaction.Message != "" {
		body["reaction"] = waReaction
	}
	if isViewOnce := evt.Message.GetViewOnceMessage(); isViewOnce != nil {
		body["view_once"] = isViewOnce != nil
	}
	if forwarded {
		body["forwarded"] = forwarded
	}
	if timestamp := evt.MessageTimestamp; timestamp != nil {
		body["timestamp"] = timestamp
	}

	// Handle protocol messages (revoke, etc.)
	if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		protocolType := protocolMessage.GetType().String()

		switch protocolType {
		case "REVOKE":
			body["action"] = "message_revoked"
			if key := protocolMessage.GetKey(); key != nil {
				body["revoked_message_id"] = key.GetID()
				body["revoked_from_me"] = key.GetFromMe()
				if key.GetRemoteJID() != "" {
					body["revoked_chat"] = key.GetRemoteJID()
				}
			}
		case "MESSAGE_EDIT":
			body["action"] = "message_edited"
			if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
				body["edited_id"] = protocolMessage.Key.ID

				// histórico de mensagem - mensagem editada
				// if caption := extractCaption(editedMessage); caption != "" {
				// 	body["edited_caption"] = caption
				// } else if text := extractText(editedMessage); text != "" {
				// 	body["edited_text"] = text
				// }
			}
		}
	}

	if audioMedia := evt.Message.GetAudioMessage(); audioMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, audioMedia)
		if err != nil {
			logrus.Errorf("Failed to download audio: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download audio: %v", err))
		}
		body["audio"] = path
	}

	if contactMessage := evt.Message.GetContactMessage(); contactMessage != nil {
		body["contact"] = contactMessage
	}

	if contactsMessage := evt.Message.GetContactsArrayMessage(); contactsMessage != nil {
		body["contact_list"] = contactsMessage
	}

	if documentMedia := evt.Message.GetDocumentMessage(); documentMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, documentMedia)
		if err != nil {
			logrus.Errorf("Failed to download document: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download document: %v", err))
		}
		body["document"] = path
	}

	if imageMedia := evt.Message.GetImageMessage(); imageMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, imageMedia)
		if err != nil {
			logrus.Errorf("Failed to download image: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download image: %v", err))
		}
		body["image"] = path
	}

	if listMessage := evt.Message.GetListMessage(); listMessage != nil {
		body["list"] = listMessage
	}

	if liveLocationMessage := evt.Message.GetLiveLocationMessage(); liveLocationMessage != nil {
		body["live_location"] = liveLocationMessage
	}

	if locationMessage := evt.Message.GetLocationMessage(); locationMessage != nil {
		body["location"] = locationMessage
	}

	if orderMessage := evt.Message.GetOrderMessage(); orderMessage != nil {
		body["order"] = orderMessage
	}

	if stickerMedia := evt.Message.GetStickerMessage(); stickerMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, stickerMedia)
		if err != nil {
			logrus.Errorf("Failed to download sticker: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download sticker: %v", err))
		}
		body["sticker"] = path
	}

	if videoMedia := evt.Message.GetVideoMessage(); videoMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, videoMedia)
		if err != nil {
			logrus.Errorf("Failed to download video: %v", err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download video: %v", err))
		}
		body["video"] = path
	}

	if pollMessage := evt.Message.GetPollCreationMessage(); pollMessage != nil {
		body["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV2(); pollMessage != nil {
		body["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV3(); pollMessage != nil {
		body["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV4(); pollMessage != nil {
		body["poll"] = pollMessage
	}

	if pollMessage := evt.Message.GetPollCreationMessageV5(); pollMessage != nil {
		body["poll"] = pollMessage
	}

	return body, nil
}
