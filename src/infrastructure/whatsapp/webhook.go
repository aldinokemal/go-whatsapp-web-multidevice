package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// forwardToWebhook is a helper function to forward event to webhook url
func forwardToWebhook(ctx context.Context, evt *events.Message) error {
	logrus.Info("Forwarding event to webhook:", config.WhatsappWebhook)
	payload, err := createPayload(ctx, evt)
	if err != nil {
		return err
	}

	for _, url := range config.WhatsappWebhook {
		if err = submitWebhook(payload, url); err != nil {
			return err
		}
	}

	logrus.Info("Event forwarded to webhook")
	return nil
}

func createPayload(ctx context.Context, evt *events.Message) (map[string]interface{}, error) {
	message := utils.BuildEventMessage(evt)
	waReaction := utils.BuildEventReaction(evt)
	forwarded := utils.BuildForwarded(evt)

	body := make(map[string]interface{})

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
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, audioMedia)
		if err != nil {
			logrus.Errorf("Failed to download audio from %s: %v", evt.Info.SourceString(), err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download audio: %v", err))
		}
		body["audio"] = path
	}

	if contactMessage := evt.Message.GetContactMessage(); contactMessage != nil {
		body["contact"] = contactMessage
	}

	if documentMedia := evt.Message.GetDocumentMessage(); documentMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, documentMedia)
		if err != nil {
			logrus.Errorf("Failed to download document from %s: %v", evt.Info.SourceString(), err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download document: %v", err))
		}
		body["document"] = path
	}

	if imageMedia := evt.Message.GetImageMessage(); imageMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, imageMedia)
		if err != nil {
			logrus.Errorf("Failed to download image from %s: %v", evt.Info.SourceString(), err)
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
			logrus.Errorf("Failed to download sticker from %s: %v", evt.Info.SourceString(), err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download sticker: %v", err))
		}
		body["sticker"] = path
	}

	if videoMedia := evt.Message.GetVideoMessage(); videoMedia != nil {
		path, err := utils.ExtractMedia(ctx, cli, config.PathMedia, videoMedia)
		if err != nil {
			logrus.Errorf("Failed to download video from %s: %v", evt.Info.SourceString(), err)
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download video: %v", err))
		}
		body["video"] = path
	}

	return body, nil
}

func submitWebhook(payload map[string]interface{}, url string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	postBody, err := json.Marshal(payload)
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("Failed to marshal body: %v", err))
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(postBody))
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("error when create http object %v", err))
	}

	secretKey := []byte(config.WhatsappWebhookSecret)
	signature, err := utils.GetMessageDigestOrSignature(postBody, secretKey)
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("error when create signature %v", err))
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", fmt.Sprintf("sha256=%s", signature))

	var attempt int
	var maxAttempts = 5
	var sleepDuration = 1 * time.Second

	for attempt = 0; attempt < maxAttempts; attempt++ {
		if _, err = client.Do(req); err == nil {
			logrus.Infof("Successfully submitted webhook on attempt %d", attempt+1)
			return nil
		}
		logrus.Warnf("Attempt %d to submit webhook failed: %v", attempt+1, err)
		time.Sleep(sleepDuration)
		sleepDuration *= 2
	}

	return pkgError.WebhookError(fmt.Sprintf("error when submit webhook after %d attempts: %v", attempt, err))
}
