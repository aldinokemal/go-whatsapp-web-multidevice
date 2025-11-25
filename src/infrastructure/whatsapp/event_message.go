package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

// forwardMessageToWebhook is a helper function to forward message event to webhook url
func forwardMessageToWebhook(ctx context.Context, evt *events.Message) error {
	payload, err := createMessagePayload(ctx, evt)
	if err != nil {
		return err
	}

	return forwardPayloadToConfiguredWebhooks(ctx, payload, "message event")
}

func createMessagePayload(ctx context.Context, evt *events.Message) (map[string]any, error) {
	message := utils.BuildEventMessage(evt)
	waReaction := utils.BuildEventReaction(evt)
	forwarded := utils.BuildForwarded(evt)

	body := make(map[string]any)

	body["sender_id"] = evt.Info.Sender.User
	body["chat_id"] = evt.Info.Chat.User

	if from := evt.Info.SourceString(); from != "" {
		body["from"] = from

		from_user, from_group := from, ""
		if strings.Contains(from, " in ") {
			from_user = strings.Split(from, " in ")[0]
			from_group = strings.Split(from, " in ")[1]
		}

		if strings.HasSuffix(from_user, "@lid") {
			body["from_lid"] = from_user
			lid, err := types.ParseJID(from_user)
			if err != nil {
				logrus.Errorf("Error when parse jid: %v", err)
			} else {
				pn, err := cli.Store.LIDs.GetPNForLID(ctx, lid)
				if err != nil {
					logrus.Errorf("Error when get pn for lid %s: %v", lid.String(), err)
				}
				if !pn.IsEmpty() {
					if from_group != "" {
						body["from"] = fmt.Sprintf("%s in %s", pn.String(), from_group)
					} else {
						body["from"] = pn.String()
					}
				}
			}
		}
	}
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
	if pushname := evt.Info.PushName; pushname != "" {
		body["pushname"] = pushname
	}
	if waReaction.Message != "" {
		body["reaction"] = waReaction
	}
	if evt.IsViewOnce {
		body["view_once"] = evt.IsViewOnce
	}
	if forwarded {
		body["forwarded"] = forwarded
	}
	if timestamp := evt.Info.Timestamp.Format(time.RFC3339); timestamp != "" {
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
			// Extract the original message ID from the protocol message key
			if key := protocolMessage.GetKey(); key != nil {
				body["original_message_id"] = key.GetID()
			}
			if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
				if editedText := editedMessage.GetExtendedTextMessage(); editedText != nil {
					body["edited_text"] = editedText.GetText()
				} else if editedConv := editedMessage.GetConversation(); editedConv != "" {
					body["edited_text"] = editedConv
				}
			}
		}
	}

	if audioMedia := evt.Message.GetAudioMessage(); audioMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, audioMedia)
			if err != nil {
				logrus.Errorf("Failed to download audio from %s: %v", evt.Info.SourceString(), err)
				return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download audio: %v", err))
			}
			body["audio"] = path
		} else {
			body["audio"] = map[string]any{
				"url": audioMedia.GetURL(),
			}
		}
	}

	if contactMessage := evt.Message.GetContactMessage(); contactMessage != nil {
		body["contact"] = contactMessage
	}

	if documentMedia := evt.Message.GetDocumentMessage(); documentMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, documentMedia)
			if err != nil {
				logrus.Errorf("Failed to download document from %s: %v", evt.Info.SourceString(), err)
				return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download document: %v", err))
			}
			body["document"] = path
		} else {
			body["document"] = map[string]any{
				"url":      documentMedia.GetURL(),
				"filename": documentMedia.GetFileName(),
			}
		}
	}

	if imageMedia := evt.Message.GetImageMessage(); imageMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, imageMedia)
			if err != nil {
				logrus.Errorf("Failed to download image from %s: %v", evt.Info.SourceString(), err)
				return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download image: %v", err))
			}
			body["image"] = path
		} else {
			body["image"] = map[string]any{
				"url":     imageMedia.GetURL(),
				"caption": imageMedia.GetCaption(),
			}
		}
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
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, stickerMedia)
			if err != nil {
				logrus.Errorf("Failed to download sticker from %s: %v", evt.Info.SourceString(), err)
				return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download sticker: %v", err))
			}
			body["sticker"] = path
		} else {
			body["sticker"] = map[string]any{
				"url": stickerMedia.GetURL(),
			}
		}
	}

	if videoMedia := evt.Message.GetVideoMessage(); videoMedia != nil {
		if config.WhatsappAutoDownloadMedia {
			path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, videoMedia)
			if err != nil {
				logrus.Errorf("Failed to download video from %s: %v", evt.Info.SourceString(), err)
				return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download video: %v", err))
			}
			body["video"] = path
		} else {
			body["video"] = map[string]any{
				"url":     videoMedia.GetURL(),
				"caption": videoMedia.GetCaption(),
			}
		}
	}

	return body, nil
}
