package saas

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

const (
	heartbeatInterval = 60 * time.Second
	heartbeatHTTPTimeout = 10 * time.Second
	heartbeatBootDelay = 5 * time.Second
)

type statusPayload struct {
	Status    string `json:"status"`
	PhoneE164 string `json:"phone_e164,omitempty"`
	PairedAt  string `json:"paired_at,omitempty"`
}

// StartStatusReporter launches a background goroutine that POSTs the bot's
// WhatsApp connection state to the SaaS once at boot and then every minute.
// This is how the SaaS detects unpairing / disconnects (signals it cannot infer
// from inbound traffic alone). No-op when SaaS integration is disabled.
//
// getClient returns the live whatsmeow client; it may be nil before login, and
// we tolerate that (reported as "disconnected"). The function is safe to call
// repeatedly and never panics.
func StartStatusReporter(getClient func() *whatsmeow.Client) {
	if !Enabled() {
		return
	}
	go func() {
		time.Sleep(heartbeatBootDelay)
		reportOnce(getClient)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for range ticker.C {
			reportOnce(getClient)
		}
	}()
}

func reportOnce(getClient func() *whatsmeow.Client) {
	status, phone := deriveStatus(getClient())

	payload := statusPayload{Status: status, PhoneE164: phone}
	if status == "active" {
		// Stamp paired_at once (the first time we observe a live session).
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

// deriveStatus maps the whatsmeow client state to the SaaS status enum using
// only stable client methods. Banned vs disconnected is intentionally NOT
// distinguished here (both mean "not working, needs re-pair"); a future
// event-handler refinement (events.LoggedOut reason) can split them.
func deriveStatus(client *whatsmeow.Client) (status string, phone string) {
	if client == nil {
		return "disconnected", ""
	}
	if client.Store != nil && client.Store.ID != nil {
		phone = "+" + client.Store.ID.User
		if client.IsLoggedIn() && client.IsConnected() {
			return "active", phone
		}
		return "disconnected", phone
	}
	// No stored device → never paired (or fully logged out).
	return "pairing", ""
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
