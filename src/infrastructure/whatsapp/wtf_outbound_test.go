package whatsapp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

type recordingTransport struct{ urls []string }

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{},
		Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

// Security: webhook delivery goes through the OutboundClient hook when it is
// set (multi-user mode installs netguard), and the per-device TLS option is
// passed through. The host does not exist, so a delivery that succeeds proves
// the hook's client carried it.
func TestWebhookDeliveryUsesTheGuardWhenSet(t *testing.T) {
	saved := OutboundClient
	t.Cleanup(func() { OutboundClient = saved })
	rt := &recordingTransport{}
	var gotTimeout time.Duration
	var gotInsecure bool
	OutboundClient = func(timeout time.Duration, insecureSkipVerify bool) *http.Client {
		gotTimeout, gotInsecure = timeout, insecureSkipVerify
		return &http.Client{Timeout: timeout, Transport: rt}
	}

	cfg := &domainChatStorage.DeviceWebhookConfig{WebhookInsecureSkipVerify: true}
	err := submitWebhook(context.Background(), map[string]any{"k": "v"}, "https://guard.invalid/hook", cfg)
	if err != nil {
		t.Fatalf("delivery through the hook failed: %v", err)
	}
	if len(rt.urls) != 1 || rt.urls[0] != "https://guard.invalid/hook" {
		t.Fatalf("hook client saw %v", rt.urls)
	}
	if gotTimeout <= 0 || !gotInsecure {
		t.Fatalf("hook got timeout %v insecure %v; want the upstream timeout and the device's TLS option", gotTimeout, gotInsecure)
	}
}
