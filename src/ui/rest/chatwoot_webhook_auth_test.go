package rest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/gofiber/fiber/v3"
)

// chatwootSign signs a body the way Chatwoot does (lib/webhooks/trigger.rb).
func chatwootSign(secret, timestamp, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestVerifyChatwootSignatureKnownVectors pins the scheme against digests
// computed independently (Python hmac/hashlib), so a change to what is signed
// — separator, field order, encoding — fails here.
func TestVerifyChatwootSignatureKnownVectors(t *testing.T) {
	vectors := []struct {
		name, secret, timestamp, body, digest string
	}{
		{
			name:      "plain payload",
			secret:    "test-secret",
			timestamp: "1700000000",
			body:      `{"event":"message_created"}`,
			digest:    "9dcdcaaadf7652a518bc7d9ec7403c6cbae2abfba9cf20f561683beec1e34b81",
		},
		{
			name:      "non-ascii payload",
			secret:    "whsec_3f9c1a7e",
			timestamp: "1767225600",
			body:      `{"id":42,"event":"message_created","message_type":"outgoing","content":"hello é"}`,
			digest:    "0bc42c6cb3cae772cdf9274cf326a611f42ecf82daec6fe35e80ae1f3da140c9",
		},
		{
			name:      "empty body",
			secret:    "k",
			timestamp: "0",
			body:      "",
			digest:    "6b4a4b8b3c40f1e8f53a3d36682e5f99f7ad2ac1df1c93dfe336f329167641e7",
		},
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			seconds, _ := strconv.ParseInt(v.timestamp, 10, 64)
			now := time.Unix(seconds, 0).Add(30 * time.Second)
			if err := verifyChatwootSignature(v.secret, v.timestamp, "sha256="+v.digest, []byte(v.body), now, 5*time.Minute); err != nil {
				t.Fatalf("known vector rejected: %v", err)
			}
			// Hex case is not significant.
			if err := verifyChatwootSignature(v.secret, v.timestamp, "sha256="+strings.ToUpper(v.digest), []byte(v.body), now, 5*time.Minute); err != nil {
				t.Fatalf("upper-case digest rejected: %v", err)
			}
			if got := chatwootSign(v.secret, v.timestamp, v.body); got != "sha256="+v.digest {
				t.Fatalf("test signer = %s, want the known digest", got)
			}
		})
	}
}

func TestVerifyChatwootSignatureRejections(t *testing.T) {
	const (
		secret    = "test-secret"
		timestamp = "1700000000"
		body      = `{"event":"message_created"}`
	)
	signedAt := time.Unix(1700000000, 0)
	valid := chatwootSign(secret, timestamp, body)
	tolerance := 5 * time.Minute

	tests := []struct {
		name      string
		secret    string
		timestamp string
		signature string
		body      string
		now       time.Time
		want      error
	}{
		{"valid at the edge of the window", secret, timestamp, valid, body, signedAt.Add(tolerance), nil},
		{"valid with the sender's clock ahead", secret, timestamp, valid, body, signedAt.Add(-tolerance), nil},
		{"expired", secret, timestamp, valid, body, signedAt.Add(tolerance + time.Second), errChatwootSignatureExpired},
		{"too far in the future", secret, timestamp, valid, body, signedAt.Add(-tolerance - time.Second), errChatwootSignatureExpired},
		{"wrong secret", "other-secret", timestamp, valid, body, signedAt, errChatwootSignatureMismatch},
		{"tampered body", secret, timestamp, valid, body + " ", signedAt, errChatwootSignatureMismatch},
		// A replay with a fresh timestamp: the timestamp is part of what is signed.
		{"timestamp replaced", secret, "1700000100", valid, body, signedAt.Add(100 * time.Second), errChatwootSignatureMismatch},
		{"missing signature", secret, timestamp, "", body, signedAt, errChatwootSignatureMissing},
		{"missing timestamp", secret, "", valid, body, signedAt, errChatwootSignatureMissing},
		{"no secret configured", "", timestamp, valid, body, signedAt, errChatwootSignatureMissing},
		{"timestamp not a number", secret, "yesterday", valid, body, signedAt, errChatwootSignatureTimestamp},
		{"millisecond timestamp", secret, "1700000000000", chatwootSign(secret, "1700000000000", body), body, signedAt, errChatwootSignatureExpired},
		{"missing sha256 prefix", secret, timestamp, strings.TrimPrefix(valid, "sha256="), body, signedAt, errChatwootSignatureMalformed},
		{"digest not hex", secret, timestamp, "sha256=not-hex", body, signedAt, errChatwootSignatureMalformed},
		{"truncated digest", secret, timestamp, valid[:len(valid)-2], body, signedAt, errChatwootSignatureMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyChatwootSignature(tt.secret, tt.timestamp, tt.signature, []byte(tt.body), tt.now, tolerance)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

type webhookAuthRequest struct {
	path    string
	body    string
	headers map[string]string
}

func postChatwootWebhook(t *testing.T, app *fiber.App, r webhookAuthRequest) int {
	t.Helper()
	path := r.path
	if path == "" {
		path = "/chatwoot/webhook"
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(r.body))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range r.headers {
		req.Header.Set(name, value)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp.StatusCode
}

func nowUnix(offset time.Duration) string {
	return strconv.FormatInt(time.Now().Add(offset).Unix(), 10)
}

// TestChatwootWebhookAcceptsChatwootSignature drives the real handler. The
// body is a conversation event, which the handler acknowledges without any
// device, so the status reflects authentication alone.
func TestChatwootWebhookAcceptsChatwootSignature(t *testing.T) {
	const (
		secret = "channel-secret"
		body   = `{"event":"conversation_updated","account":{"id":1}}`
	)
	withChatwootWebhookSecret(t, secret)

	app := fiber.New()
	app.Post("/chatwoot/webhook", (&ChatwootHandler{}).HandleWebhook)

	signed := func(secret, timestamp, body string) map[string]string {
		return map[string]string{chatwootTimestampHeader: timestamp, chatwootSignatureHeader: chatwootSign(secret, timestamp, body)}
	}
	fresh := nowUnix(0)
	stale := nowUnix(-6 * time.Minute)
	ahead := nowUnix(6 * time.Minute)

	tests := []struct {
		name    string
		request webhookAuthRequest
		want    int
	}{
		{"valid signature", webhookAuthRequest{body: body, headers: signed(secret, fresh, body)}, fiber.StatusOK},
		{"valid signature slightly old", webhookAuthRequest{body: body, headers: signed(secret, nowUnix(-4*time.Minute), body)}, fiber.StatusOK},
		{"expired timestamp", webhookAuthRequest{body: body, headers: signed(secret, stale, body)}, fiber.StatusUnauthorized},
		{"timestamp in the future", webhookAuthRequest{body: body, headers: signed(secret, ahead, body)}, fiber.StatusUnauthorized},
		{"wrong secret", webhookAuthRequest{body: body, headers: signed("other-secret", fresh, body)}, fiber.StatusUnauthorized},
		{"signed for another body", webhookAuthRequest{body: body, headers: signed(secret, fresh, `{"event":"x"}`)}, fiber.StatusUnauthorized},
		{"signature without timestamp", webhookAuthRequest{body: body, headers: map[string]string{chatwootSignatureHeader: chatwootSign(secret, fresh, body)}}, fiber.StatusUnauthorized},
		{"timestamp without signature", webhookAuthRequest{body: body, headers: map[string]string{chatwootTimestampHeader: fresh}}, fiber.StatusUnauthorized},
		{"no credentials at all", webhookAuthRequest{body: body}, fiber.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postChatwootWebhook(t, app, tt.request); got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestChatwootWebhookKeepsOlderSchemes: every way of presenting the secret
// that worked before still works, including next to a Chatwoot signature that
// does not verify (Chatwoot signs with the channel's own secret, which an
// existing deployment authenticating through the URL may not use).
func TestChatwootWebhookKeepsOlderSchemes(t *testing.T) {
	const (
		secret = "shared-secret"
		body   = `{"event":"conversation_updated","account":{"id":1}}`
	)
	withChatwootWebhookSecret(t, secret)

	app := fiber.New()
	app.Post("/chatwoot/webhook", (&ChatwootHandler{}).HandleWebhook)

	hub := func(secret, body string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(body))
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	foreignSignature := map[string]string{
		chatwootTimestampHeader: nowUnix(0),
		chatwootSignatureHeader: chatwootSign("chatwoot-generated-secret", nowUnix(0), body),
	}

	tests := []struct {
		name    string
		request webhookAuthRequest
		want    int
	}{
		{"X-Chatwoot-Webhook-Secret", webhookAuthRequest{body: body, headers: map[string]string{"X-Chatwoot-Webhook-Secret": secret}}, fiber.StatusOK},
		{"X-Gowa-Chatwoot-Secret", webhookAuthRequest{body: body, headers: map[string]string{"X-Gowa-Chatwoot-Secret": secret}}, fiber.StatusOK},
		{"query string", webhookAuthRequest{path: "/chatwoot/webhook?secret=" + secret, body: body}, fiber.StatusOK},
		{"X-Hub-Signature-256", webhookAuthRequest{body: body, headers: map[string]string{"X-Hub-Signature-256": hub(secret, body)}}, fiber.StatusOK},
		{"wrong header secret", webhookAuthRequest{body: body, headers: map[string]string{"X-Chatwoot-Webhook-Secret": "nope"}}, fiber.StatusUnauthorized},
		{"wrong query secret", webhookAuthRequest{path: "/chatwoot/webhook?secret=nope", body: body}, fiber.StatusUnauthorized},
		{"wrong X-Hub-Signature-256", webhookAuthRequest{body: body, headers: map[string]string{"X-Hub-Signature-256": hub("nope", body)}}, fiber.StatusUnauthorized},
		{"query string next to a foreign Chatwoot signature", webhookAuthRequest{path: "/chatwoot/webhook?secret=" + secret, body: body, headers: foreignSignature}, fiber.StatusOK},
		{"foreign Chatwoot signature alone", webhookAuthRequest{body: body, headers: foreignSignature}, fiber.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postChatwootWebhook(t, app, tt.request); got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestChatwootWebhookWithoutSecretRejectsRequests(t *testing.T) {
	withChatwootWebhookSecret(t, "")
	app := fiber.New()
	app.Post("/chatwoot/webhook", (&ChatwootHandler{}).HandleWebhook)

	body := `{"event":"conversation_updated"}`
	for name, headers := range map[string]map[string]string{
		"no headers":        nil,
		"unverifiable sign": {chatwootTimestampHeader: "1", chatwootSignatureHeader: "sha256=00"},
	} {
		if got := postChatwootWebhook(t, app, webhookAuthRequest{body: body, headers: headers}); got != fiber.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401 when no secret is configured", name, got)
		}
	}
}

func TestChatwootWebhookAllowUnsignedAcceptsEmptySecret(t *testing.T) {
	withChatwootWebhookSecret(t, "")
	prev := config.ChatwootWebhookAllowUnsigned
	config.ChatwootWebhookAllowUnsigned = true
	t.Cleanup(func() { config.ChatwootWebhookAllowUnsigned = prev })

	app := fiber.New()
	app.Post("/chatwoot/webhook", (&ChatwootHandler{}).HandleWebhook)
	if got := postChatwootWebhook(t, app, webhookAuthRequest{body: `{"event":"conversation_updated"}`}); got != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 when unsigned webhooks are explicitly enabled", got)
	}
}

func TestChatwootWebhookSignatureToleranceIsConfigurable(t *testing.T) {
	const (
		secret = "channel-secret"
		body   = `{"event":"conversation_updated"}`
	)
	withChatwootWebhookSecret(t, secret)
	prev := config.ChatwootWebhookSignatureTolerance
	t.Cleanup(func() { config.ChatwootWebhookSignatureTolerance = prev })

	app := fiber.New()
	app.Post("/chatwoot/webhook", (&ChatwootHandler{}).HandleWebhook)

	timestamp := nowUnix(-2 * time.Minute)
	request := webhookAuthRequest{body: body, headers: map[string]string{
		chatwootTimestampHeader: timestamp,
		chatwootSignatureHeader: chatwootSign(secret, timestamp, body),
	}}

	config.ChatwootWebhookSignatureTolerance = 5 * time.Minute
	if got := postChatwootWebhook(t, app, request); got != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 inside the default window", got)
	}
	config.ChatwootWebhookSignatureTolerance = time.Minute
	if got := postChatwootWebhook(t, app, request); got != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 once the window is narrower than the request's age", got)
	}
}

// TestChatwootDeviceWebhookUsesDeviceSecret: each Chatwoot API channel has
// its own secret, so a device config can carry one. It replaces the global
// secret on that device's endpoint only.
func TestChatwootDeviceWebhookUsesDeviceSecret(t *testing.T) {
	const (
		globalSecret = "global-secret"
		deviceSecret = "device-channel-secret"
		body         = `{"event":"conversation_updated","account":{"id":1}}`
	)
	withChatwootWebhookSecret(t, globalSecret)

	repo := &deviceWebhookTestRepo{
		cfg: &domainChatStorage.ChatwootDeviceConfig{
			ID: 1, DeviceID: "d", ChatwootURL: "https://chat.example.com", AccountID: 1, InboxID: 2,
			APIToken: "t", Enabled: true, WebhookSecret: deviceSecret,
		},
		count: 1,
	}
	chatwoot.InitClientRegistry(repo)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	app := fiber.New()
	handler := &ChatwootHandler{ChatStorageRepo: repo}
	app.Post("/chatwoot/webhook", handler.HandleWebhook)
	app.Post("/chatwoot/webhook/:device_id", handler.HandleDeviceWebhook)

	signed := func(secret string) map[string]string {
		timestamp := nowUnix(0)
		return map[string]string{chatwootTimestampHeader: timestamp, chatwootSignatureHeader: chatwootSign(secret, timestamp, body)}
	}

	tests := []struct {
		name    string
		request webhookAuthRequest
		want    int
	}{
		{"device endpoint, device secret", webhookAuthRequest{path: "/chatwoot/webhook/d", body: body, headers: signed(deviceSecret)}, fiber.StatusOK},
		{"device endpoint, global secret is not accepted", webhookAuthRequest{path: "/chatwoot/webhook/d", body: body, headers: signed(globalSecret)}, fiber.StatusUnauthorized},
		{"device endpoint, device secret in the old header", webhookAuthRequest{path: "/chatwoot/webhook/d", body: body, headers: map[string]string{"X-Chatwoot-Webhook-Secret": deviceSecret}}, fiber.StatusOK},
		{"device endpoint, no credentials", webhookAuthRequest{path: "/chatwoot/webhook/d", body: body}, fiber.StatusUnauthorized},
		{"another device falls back to the global secret", webhookAuthRequest{path: "/chatwoot/webhook/other", body: body, headers: signed(globalSecret)}, fiber.StatusOK},
		{"another device does not accept this device's secret", webhookAuthRequest{path: "/chatwoot/webhook/other", body: body, headers: signed(deviceSecret)}, fiber.StatusUnauthorized},
		{"shared endpoint keeps the global secret", webhookAuthRequest{body: body, headers: signed(globalSecret)}, fiber.StatusOK},
		{"shared endpoint does not accept a device secret", webhookAuthRequest{body: body, headers: signed(deviceSecret)}, fiber.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postChatwootWebhook(t, app, tt.request); got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}

	// A device without its own secret keeps using the global one.
	repo.cfg.WebhookSecret = ""
	if got := postChatwootWebhook(t, app, webhookAuthRequest{path: "/chatwoot/webhook/d", body: body, headers: signed(globalSecret)}); got != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 with the global secret when the device has none", got)
	}
}

// TestChatwootDeviceSecretProtectsDeviceEndpointWithoutGlobalSecret: with no
// global secret, a device that has its own is still protected.
func TestChatwootDeviceSecretProtectsDeviceEndpointWithoutGlobalSecret(t *testing.T) {
	withChatwootWebhookSecret(t, "")
	repo := &deviceWebhookTestRepo{
		cfg: &domainChatStorage.ChatwootDeviceConfig{
			ID: 1, DeviceID: "d", ChatwootURL: "https://chat.example.com", AccountID: 1, InboxID: 2,
			APIToken: "t", Enabled: true, WebhookSecret: "device-channel-secret",
		},
		count: 1,
	}
	chatwoot.InitClientRegistry(repo)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	app := fiber.New()
	app.Post("/chatwoot/webhook/:device_id", (&ChatwootHandler{ChatStorageRepo: repo}).HandleDeviceWebhook)

	body := `{"event":"conversation_updated"}`
	if got := postChatwootWebhook(t, app, webhookAuthRequest{path: "/chatwoot/webhook/d", body: body}); got != fiber.StatusUnauthorized {
		t.Fatalf("unsigned request status = %d, want 401", got)
	}
	timestamp := nowUnix(0)
	headers := map[string]string{chatwootTimestampHeader: timestamp, chatwootSignatureHeader: chatwootSign("device-channel-secret", timestamp, body)}
	if got := postChatwootWebhook(t, app, webhookAuthRequest{path: "/chatwoot/webhook/d", body: body, headers: headers}); got != fiber.StatusOK {
		t.Fatalf("signed request status = %d, want 200", got)
	}
}

func TestChatwootConfigWebhookSecretIsWriteOnly(t *testing.T) {
	store := newFakeConfigStore()
	app := newConfigTestApp(t, store)
	const base = `"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"token-one"`

	resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config", `{`+base+`,"webhook_secret":" channel-secret "}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(body, "channel-secret") {
		t.Fatalf("response leaks the webhook secret: %s", body)
	}
	if !strings.Contains(body, `"webhook_secret_set":true`) {
		t.Fatalf("response must report that a secret is set: %s", body)
	}
	if got := store.configs["dev"].WebhookSecret; got != "channel-secret" {
		t.Fatalf("stored secret = %q, want it trimmed and stored", got)
	}

	// Omitted on update: the stored secret stays.
	if resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config", `{`+base+`,"enabled":false}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d: %s", resp.StatusCode, body)
	}
	if got := store.configs["dev"].WebhookSecret; got != "channel-secret" {
		t.Fatalf("stored secret = %q after an update that omitted it, want it kept", got)
	}
	for _, path := range []string{"/devices/dev/chatwoot/config", "/chatwoot/configs"} {
		_, body := doJSON(t, app, http.MethodGet, path, "")
		if strings.Contains(body, "channel-secret") || !strings.Contains(body, `"webhook_secret_set":true`) {
			t.Fatalf("GET %s = %s, want the secret hidden and reported as set", path, body)
		}
	}

	// An explicit empty string clears it.
	resp, body = doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config", `{`+base+`,"webhook_secret":""}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"webhook_secret_set":false`) {
		t.Fatalf("clear status = %d: %s", resp.StatusCode, body)
	}
	if got := store.configs["dev"].WebhookSecret; got != "" {
		t.Fatalf("stored secret = %q, want it cleared", got)
	}
}

// failingConfigRepo makes the device config lookup fail.
type failingConfigRepo struct {
	domainChatStorage.IChatStorageRepository
}

func (failingConfigRepo) GetChatwootDeviceConfigByIdentifier(string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	return nil, errors.New("database is locked")
}

// TestChatwootDeviceWebhookFailsClosedOnStorageError: when the device config
// cannot be read it is unknown whether the device has its own secret, so the
// request is refused even with no global secret configured.
func TestChatwootDeviceWebhookFailsClosedOnStorageError(t *testing.T) {
	withChatwootWebhookSecret(t, "")
	app := fiber.New()
	app.Post("/chatwoot/webhook/:device_id", (&ChatwootHandler{ChatStorageRepo: failingConfigRepo{}}).HandleDeviceWebhook)

	if got := postChatwootWebhook(t, app, webhookAuthRequest{path: "/chatwoot/webhook/d", body: `{"event":"conversation_updated"}`}); got != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", got)
	}
}
