package whatsapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

// forwardToWebhook is a helper function to forward event to webhook url
func forwardToWebhook(evt *events.Message) error {
	logrus.Info("Forwarding event to webhook:", config.WhatsappWebhook)
	payload, err := createPayload(evt)
	if err != nil {
		return err
	}

	if err = submitWebhook(payload); err != nil {
		return err
	}

	logrus.Info("Event forwarded to webhook")
	return nil
}

func createPayload(evt *events.Message) (map[string]interface{}, error) {
	message := buildEventMessage(evt)
	waReaction := buildEventReaction(evt)
	forwarded := buildForwarded(evt)

	imageMedia := evt.Message.GetImageMessage()
	stickerMedia := evt.Message.GetStickerMessage()
	videoMedia := evt.Message.GetVideoMessage()
	audioMedia := evt.Message.GetAudioMessage()
	documentMedia := evt.Message.GetDocumentMessage()

	body := map[string]interface{}{
		"audio":         audioMedia,
		"contact":       evt.Message.GetContactMessage(),
		"document":      documentMedia,
		"from":          evt.Info.SourceString(),
		"image":         imageMedia,
		"list":          evt.Message.GetListMessage(),
		"live_location": evt.Message.GetLiveLocationMessage(),
		"location":      evt.Message.GetLocationMessage(),
		"message":       message,
		"order":         evt.Message.GetOrderMessage(),
		"pushname":      evt.Info.PushName,
		"reaction":      waReaction,
		"sticker":       stickerMedia,
		"video":         videoMedia,
		"view_once":     evt.IsViewOnce,
		"forwarded":     forwarded,
	}

	if imageMedia != nil {
		path, err := ExtractMedia(config.PathMedia, imageMedia)
		if err != nil {
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download image: %v", err))
		}
		body["image"] = path
	}
	if stickerMedia != nil {
		path, err := ExtractMedia(config.PathMedia, stickerMedia)
		if err != nil {
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download sticker: %v", err))
		}
		body["sticker"] = path
	}
	if videoMedia != nil {
		path, err := ExtractMedia(config.PathMedia, videoMedia)
		if err != nil {
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download video: %v", err))
		}
		body["video"] = path
	}
	if audioMedia != nil {
		path, err := ExtractMedia(config.PathMedia, audioMedia)
		if err != nil {
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download audio: %v", err))
		}
		body["audio"] = path
	}
	if documentMedia != nil {
		path, err := ExtractMedia(config.PathMedia, documentMedia)
		if err != nil {
			return nil, pkgError.WebhookError(fmt.Sprintf("Failed to download document: %v", err))
		}
		body["document"] = path
	}

	return body, nil
}

func submitWebhook(payload map[string]interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}

	postBody, err := json.Marshal(payload)
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("Failed to marshal body: %v", err))
	}

	req, err := http.NewRequest(http.MethodPost, config.WhatsappWebhook, bytes.NewBuffer(postBody))
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("error when create http object %v", err))
	}

	secretKey := []byte(config.WhatsappWebhookSecret)
	signature, err := getMessageDigestOrSignature(postBody, secretKey)
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("error when create signature %v", err))
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", fmt.Sprintf("sha256=%s", signature))

	if _, err = client.Do(req); err != nil {
		return pkgError.WebhookError(fmt.Sprintf("error when submit webhook %v", err))
	}
	return nil
}
