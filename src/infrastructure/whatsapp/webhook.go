package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
)

func submitWebhook(ctx context.Context, payload map[string]any, url string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	postBody, err := json.Marshal(payload)
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("Failed to marshal body: %v", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
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
		// Create new request body for each attempt
		req.Body = io.NopCloser(bytes.NewBuffer(postBody))
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				logrus.Infof("Successfully submitted webhook on attempt %d", attempt+1)
				return nil
			}
			err = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		}
		logrus.Warnf("Attempt %d to submit webhook failed: %v", attempt+1, err)
		if attempt < maxAttempts-1 {
			time.Sleep(sleepDuration)
			sleepDuration *= 2
		}
	}

	return pkgError.WebhookError(fmt.Sprintf("error when submit webhook after %d attempts: %v", attempt, err))
}
