package chatwoot

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// saveProvisionConfig snapshots every config global EnsureInbox reads or writes
// and returns a restore func. Provisioning mutates package globals (notably
// ChatwootInboxID), so without this a leaked value would make sibling tests —
// and even later cases in the same test — observe the wrong state.
func saveProvisionConfig(t *testing.T) func() {
	t.Helper()
	origAuto := config.ChatwootAutoCreate
	origInboxID := config.ChatwootInboxID
	origInboxName := config.ChatwootInboxName
	origWebhook := config.ChatwootWebhookURL
	return func() {
		config.ChatwootAutoCreate = origAuto
		config.ChatwootInboxID = origInboxID
		config.ChatwootInboxName = origInboxName
		config.ChatwootWebhookURL = origWebhook
	}
}

func TestEnsureInbox_NoopWhenAutoCreateDisabled(t *testing.T) {
	// Auto-create off is a hard no-op: EnsureInbox must return nil without ever
	// touching the network. A server that fails the test on any request proves
	// no HTTP call is made.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = false
	config.ChatwootInboxID = 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request when auto-create disabled: %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := EnsureInbox(c); err != nil {
		t.Fatalf("EnsureInbox: %v", err)
	}
}

func TestEnsureInbox_NoopWhenNotMinimallyConfigured(t *testing.T) {
	// Even with auto-create on, a client missing URL/token/account id can't
	// provision anything, so EnsureInbox returns nil without any HTTP. Each
	// case zeroes exactly one required field to prove all three are checked,
	// plus a nil client.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request for unconfigured client: %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	tests := []struct {
		name   string
		client *Client
	}{
		{"nil client", nil},
		{"empty BaseURL", &Client{APIToken: "t", AccountID: 1, HTTPClient: server.Client()}},
		{"empty APIToken", &Client{BaseURL: server.URL, AccountID: 1, HTTPClient: server.Client()}},
		{"zero AccountID", &Client{BaseURL: server.URL, APIToken: "t", HTTPClient: server.Client()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := EnsureInbox(tc.client); err != nil {
				t.Fatalf("EnsureInbox: %v", err)
			}
		})
	}
}

func TestEnsureInbox_SkipsWhenInboxIDAlreadySet(t *testing.T) {
	// An explicit CHATWOOT_INBOX_ID is treated as the operator's override:
	// EnsureInbox skips provisioning entirely (no HTTP) and leaves the id as-is.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 42

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected HTTP request when inbox id already set: %s %s", r.Method, r.URL.String())
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := EnsureInbox(c); err != nil {
		t.Fatalf("EnsureInbox: %v", err)
	}
	if config.ChatwootInboxID != 42 {
		t.Errorf("ChatwootInboxID = %d, want 42 (unchanged)", config.ChatwootInboxID)
	}
}

func TestEnsureInbox_ReusesExistingInboxByName(t *testing.T) {
	// When an inbox already exists whose name matches ChatwootInboxName
	// case-insensitively, EnsureInbox reuses it: it sets both config and the
	// live client's InboxID to that inbox and makes NO CreateInbox POST. The
	// case-insensitive match (config "whatsapp" vs server "WhatsApp") guards
	// against restarts piling up near-duplicate inboxes.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0
	config.ChatwootInboxName = "whatsapp"

	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/inboxes":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{
					{ID: 5, Name: "Email"},
					{ID: 9, Name: "WhatsApp", ChannelType: "Channel::Api"}, // differs only in case -> must match
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/inboxes":
			createCalls++
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 100})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := EnsureInbox(c); err != nil {
		t.Fatalf("EnsureInbox: %v", err)
	}
	if createCalls != 0 {
		t.Errorf("create calls = %d, want 0 (reused existing inbox)", createCalls)
	}
	if config.ChatwootInboxID != 9 {
		t.Errorf("config.ChatwootInboxID = %d, want 9", config.ChatwootInboxID)
	}
	if c.InboxID != 9 {
		t.Errorf("client.InboxID = %d, want 9", c.InboxID)
	}
}

func TestEnsureInbox_SkipsSameNameNonAPIInbox(t *testing.T) {
	// A same-name inbox that is NOT an API channel (e.g. a native WhatsApp
	// channel also called "WhatsApp") must NOT be reused — binding to it would
	// silently break agent-reply delivery. EnsureInbox skips it and creates a
	// dedicated API inbox instead.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0
	config.ChatwootInboxName = "WhatsApp"

	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/inboxes":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{
					{ID: 9, Name: "WhatsApp", ChannelType: "Channel::Whatsapp"}, // native WA -> must NOT reuse
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/inboxes":
			createCalls++
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 100, "name": "WhatsApp"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := EnsureInbox(c); err != nil {
		t.Fatalf("EnsureInbox: %v", err)
	}
	if createCalls != 1 {
		t.Errorf("create calls = %d, want 1 (non-API same-name inbox skipped)", createCalls)
	}
	if config.ChatwootInboxID != 100 || c.InboxID != 100 {
		t.Errorf("inbox id config=%d client=%d, want 100 (newly created API inbox)", config.ChatwootInboxID, c.InboxID)
	}
}

func TestEnsureInbox_ReusesAPIInboxOverSameNameNonAPI(t *testing.T) {
	// When both a non-API and an API inbox share the configured name, the API
	// one is reused and no inbox is created.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0
	config.ChatwootInboxName = "WhatsApp"

	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/inboxes":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{
					{ID: 7, Name: "WhatsApp", ChannelType: "Channel::Whatsapp"}, // skipped
					{ID: 9, Name: "WhatsApp", ChannelType: "Channel::Api"},      // reused
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/inboxes":
			createCalls++
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 100})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := EnsureInbox(c); err != nil {
		t.Fatalf("EnsureInbox: %v", err)
	}
	if createCalls != 0 {
		t.Errorf("create calls = %d, want 0 (reused same-name API inbox)", createCalls)
	}
	if config.ChatwootInboxID != 9 || c.InboxID != 9 {
		t.Errorf("inbox id config=%d client=%d, want 9 (API inbox)", config.ChatwootInboxID, c.InboxID)
	}
}

func TestEnsureInbox_CreatesWhenNoNameMatch(t *testing.T) {
	// No existing inbox matches the configured name, so EnsureInbox creates a
	// fresh one and adopts the created id into both config and the client. We
	// also confirm the name and webhook from config flow into the POST body.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0
	config.ChatwootInboxName = "Support"
	config.ChatwootWebhookURL = "https://hook.example/webhook"

	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/inboxes":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{{ID: 5, Name: "Email"}}, // no "Support" -> create
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/inboxes":
			createCalls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if body["name"] != "Support" {
				t.Errorf("create name = %v, want Support", body["name"])
			}
			ch, _ := body["channel"].(map[string]any)
			if ch["webhook_url"] != "https://hook.example/webhook" {
				t.Errorf("webhook_url = %v, want config value", ch["webhook_url"])
			}
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 123, "name": "Support"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := EnsureInbox(c); err != nil {
		t.Fatalf("EnsureInbox: %v", err)
	}
	if createCalls != 1 {
		t.Errorf("create calls = %d, want 1", createCalls)
	}
	if config.ChatwootInboxID != 123 {
		t.Errorf("config.ChatwootInboxID = %d, want 123", config.ChatwootInboxID)
	}
	if c.InboxID != 123 {
		t.Errorf("client.InboxID = %d, want 123", c.InboxID)
	}
}

func TestEnsureInbox_DefaultsNameToWhatsAppWhenBlank(t *testing.T) {
	// A blank/whitespace ChatwootInboxName falls back to "WhatsApp" before the
	// name match runs, so an existing "WhatsApp" inbox is still reused without
	// creating a duplicate.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0
	config.ChatwootInboxName = "   "

	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/inboxes":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{{ID: 8, Name: "WhatsApp", ChannelType: "Channel::Api"}},
			})
		case r.Method == http.MethodPost:
			createCalls++
			writeJSON(t, w, http.StatusOK, map[string]any{"id": 100})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := EnsureInbox(c); err != nil {
		t.Fatalf("EnsureInbox: %v", err)
	}
	if createCalls != 0 {
		t.Errorf("create calls = %d, want 0 (matched default WhatsApp name)", createCalls)
	}
	if config.ChatwootInboxID != 8 || c.InboxID != 8 {
		t.Errorf("inbox id config=%d client=%d, want 8", config.ChatwootInboxID, c.InboxID)
	}
}

func TestEnsureInbox_WrapsListInboxesError(t *testing.T) {
	// A ListInboxes failure aborts provisioning with a wrapped error so startup
	// surfaces the cause. The wrapped HTTPStatusError must still be unwrappable.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0
	config.ChatwootInboxName = "WhatsApp"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/accounts/1/inboxes" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		writeJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "boom"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	err := EnsureInbox(c)
	if err == nil {
		t.Fatal("expected error from ListInboxes failure, got nil")
	}
	if !strings.Contains(err.Error(), "list inboxes") {
		t.Errorf("err = %v, want it to mention 'list inboxes'", err)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want wrapped *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", httpErr.StatusCode)
	}
	// Provisioning failed, so the inbox id must remain unset.
	if config.ChatwootInboxID != 0 {
		t.Errorf("config.ChatwootInboxID = %d, want 0 (unchanged on error)", config.ChatwootInboxID)
	}
}

func TestEnsureInbox_WrapsCreateInboxError(t *testing.T) {
	// When no inbox matches and CreateInbox fails, EnsureInbox returns a wrapped
	// "create inbox" error and leaves the inbox id unset.
	defer saveProvisionConfig(t)()
	config.ChatwootAutoCreate = true
	config.ChatwootInboxID = 0
	config.ChatwootInboxName = "Support"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/inboxes":
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": []Inbox{}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/inboxes":
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"error": "taken"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	err := EnsureInbox(c)
	if err == nil {
		t.Fatal("expected error from CreateInbox failure, got nil")
	}
	if !strings.Contains(err.Error(), "create inbox") {
		t.Errorf("err = %v, want it to mention 'create inbox'", err)
	}
	if config.ChatwootInboxID != 0 {
		t.Errorf("config.ChatwootInboxID = %d, want 0 (unchanged on error)", config.ChatwootInboxID)
	}
}
