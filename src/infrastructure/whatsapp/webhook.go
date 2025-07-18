package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
)

func submitWebhook(_ context.Context, payload map[string]any, url string) error {
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
