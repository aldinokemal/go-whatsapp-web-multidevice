package saas

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	heartbeatInterval    = 60 * time.Second
	heartbeatHTTPTimeout = 10 * time.Second
	heartbeatBootDelay   = 5 * time.Second
)

type statusPayload struct {
	Status    string `json:"status"`
	PhoneE164 string `json:"phone_e164,omitempty"`
	PairedAt  string `json:"paired_at,omitempty"`
}

// StartStatusReporter launches a background goroutine that POSTs the bot's
// WhatsApp connection state to the SaaS at boot and then every minute. This is
// how the SaaS detects pairing / unpairing / disconnect without a manual flip.
//
// The getStatus callback is supplied by the wiring layer (cmd), which can read
// the device manager. Keeping it as a callback leaves this package free of the
// infrastructure/whatsapp import — that package already imports saas, so
// importing it back would create a cycle.
//
// getStatus returns (status, phoneE164) where status ∈
// {active, disconnected, banned, pairing}. No-op when SaaS is disabled.
func StartStatusReporter(getStatus func() (status string, phone string)) {
	if !Enabled() {
		return
	}
	go func() {
		time.Sleep(heartbeatBootDelay)
		reportOnce(getStatus)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for range ticker.C {
			reportOnce(getStatus)
		}
	}()
}

func reportOnce(getStatus func() (string, string)) {
	status, phone := getStatus()

	payload := statusPayload{Status: status, PhoneE164: phone}
	if status == "active" {
		if Load().PairedAt.IsZero() {
			MarkPaired()
		}
		if pa := Load().PairedAt; !pa.IsZero() {
			payload.PairedAt = pa.UTC().Format(time.RFC3339)
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	postStatus(body)
}

func postStatus(body []byte) {
	statusURL := Load().WebhookURL + "/status"
	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost, statusURL, bytes.NewReader(body),
	)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	SignOutboundRequest(req, body)

	httpClient := &http.Client{Timeout: heartbeatHTTPTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		logrus.Warnf("saas: status heartbeat failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logrus.Warnf("saas: status heartbeat rejected: HTTP %d", resp.StatusCode)
	}
}
