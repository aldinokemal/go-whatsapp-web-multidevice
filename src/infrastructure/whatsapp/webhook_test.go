package whatsapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

type capturedRequest struct {
	contentLength    int64
	transferEncoding []string
	body             string
}

// startCapturingWebhookServer returns a server that records every request it
// receives and answers with the given status codes in order (the last status is
// reused once the list is exhausted).
func startCapturingWebhookServer(t *testing.T, statuses ...int) (*httptest.Server, *[]capturedRequest) {
	t.Helper()

	var mu sync.Mutex
	captured := make([]capturedRequest, 0, len(statuses))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}

		mu.Lock()
		idx := len(captured)
		captured = append(captured, capturedRequest{
			contentLength:    r.ContentLength,
			transferEncoding: r.TransferEncoding,
			body:             string(body),
		})
		mu.Unlock()

		status := statuses[len(statuses)-1]
		if idx < len(statuses) {
			status = statuses[idx]
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	return srv, &captured
}

func TestSubmitWebhookSendsContentLength(t *testing.T) {
	srv, captured := startCapturingWebhookServer(t, http.StatusOK)

	payload := map[string]any{"event": "message", "body": "hello"}
	if err := submitWebhook(context.Background(), payload, srv.URL, nil); err != nil {
		t.Fatalf("submitWebhook returned error: %v", err)
	}

	reqs := *captured
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	got := reqs[0]
	if got.body == "" {
		t.Fatal("receiver got an empty body")
	}
	if got.contentLength != int64(len(got.body)) {
		t.Errorf("Content-Length = %d, want %d (receivers relying on Content-Length see an empty body without it)",
			got.contentLength, len(got.body))
	}
	if len(got.transferEncoding) != 0 {
		t.Errorf("Transfer-Encoding = %v, want none (chunked framing breaks some receivers)", got.transferEncoding)
	}
}

func TestSubmitWebhookRetryKeepsFullBody(t *testing.T) {
	srv, captured := startCapturingWebhookServer(t, http.StatusInternalServerError, http.StatusOK)

	payload := map[string]any{"event": "message", "body": "retry me"}
	if err := submitWebhook(context.Background(), payload, srv.URL, nil); err != nil {
		t.Fatalf("submitWebhook returned error: %v", err)
	}

	reqs := *captured
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests (1 failure + 1 retry), got %d", len(reqs))
	}

	if reqs[1].body != reqs[0].body {
		t.Errorf("retry body = %q, want %q (the body must be rewound between attempts)", reqs[1].body, reqs[0].body)
	}
	if reqs[1].contentLength != int64(len(reqs[1].body)) {
		t.Errorf("retry Content-Length = %d, want %d", reqs[1].contentLength, len(reqs[1].body))
	}
}

func TestWebhookTLSDeviceVerifyOverridesGlobalBypass(t *testing.T) {
	// Mutates the process-wide webhook TLS default. This package does not run
	// tests in parallel; restore the previous value before returning.
	prev := config.WhatsappWebhookInsecureSkipVerify
	t.Cleanup(func() { config.WhatsappWebhookInsecureSkipVerify = prev })

	config.WhatsappWebhookInsecureSkipVerify = true
	// WebhookInsecureSkipVerify is a bool and cannot encode "unset". false is
	// the stored "verify certificates" value and must win over the global bypass.
	deviceVerify := &chatstorage.DeviceWebhookConfig{WebhookInsecureSkipVerify: false}
	if webhookInsecureSkipVerify(t, newWebhookHTTPClient(deviceVerify)) {
		t.Fatal("device verify=true yielded InsecureSkipVerify true while the global bypass was enabled")
	}

	config.WhatsappWebhookInsecureSkipVerify = true
	if !webhookInsecureSkipVerify(t, newWebhookHTTPClient(nil)) {
		t.Fatal("unset device config did not keep the global bypass")
	}
	config.WhatsappWebhookInsecureSkipVerify = false
	if webhookInsecureSkipVerify(t, newWebhookHTTPClient(nil)) {
		t.Fatal("unset device config did not keep global certificate verification")
	}

	deviceSkip := &chatstorage.DeviceWebhookConfig{WebhookInsecureSkipVerify: true}
	config.WhatsappWebhookInsecureSkipVerify = false
	if !webhookInsecureSkipVerify(t, newWebhookHTTPClient(deviceSkip)) {
		t.Fatal("explicit device skip=true was replaced by the global verify setting")
	}
}

func TestWebhookTLSDeviceVerifyRejectsUntrustedServer(t *testing.T) {
	prev := config.WhatsappWebhookInsecureSkipVerify
	t.Cleanup(func() { config.WhatsappWebhookInsecureSkipVerify = prev })
	config.WhatsappWebhookInsecureSkipVerify = true

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	payload := map[string]any{"event": "message", "body": "tls"}
	deviceVerify := &chatstorage.DeviceWebhookConfig{WebhookInsecureSkipVerify: false}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := submitWebhook(ctx, payload, srv.URL, deviceVerify); err == nil {
		t.Fatal("device verify=true accepted an untrusted certificate while the global bypass was enabled")
	}

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := submitWebhook(ctx, payload, srv.URL, nil); err != nil {
		t.Fatalf("unset device config did not keep the global bypass: %v", err)
	}
}

func webhookInsecureSkipVerify(t *testing.T, client *http.Client) bool {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil {
		t.Fatal("webhook client is missing a TLS config")
	}
	return transport.TLSClientConfig.InsecureSkipVerify
}
