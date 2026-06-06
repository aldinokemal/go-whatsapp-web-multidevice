package chatwoot

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// --- ListInboxes -----------------------------------------------------------

func TestListInboxes_DecodesPayload(t *testing.T) {
	// ListInboxes hits the account-level inboxes endpoint and decodes the
	// {"payload":[]Inbox} envelope. We assert the path/method and the fields
	// provisioning uses to match by name and API channel type.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/accounts/1/inboxes" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []Inbox{
				{ID: 2, Name: "WhatsApp", ChannelType: "Channel::Api"},
				{ID: 3, Name: "Email"},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	inboxes, err := c.ListInboxes()
	if err != nil {
		t.Fatalf("ListInboxes: %v", err)
	}
	if len(inboxes) != 2 {
		t.Fatalf("len(inboxes) = %d, want 2", len(inboxes))
	}
	if inboxes[0].ID != 2 || inboxes[0].Name != "WhatsApp" {
		t.Errorf("inboxes[0] = %+v, want id 2 name WhatsApp", inboxes[0])
	}
	if inboxes[0].ChannelType != "Channel::Api" {
		t.Errorf("inboxes[0] = %+v, want API channel decoded", inboxes[0])
	}
}

func TestListInboxes_EmptyPayload(t *testing.T) {
	// An account with no inboxes decodes to an empty slice, not an error —
	// provisioning treats this as "no match, create one".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"payload": []Inbox{}})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	inboxes, err := c.ListInboxes()
	if err != nil {
		t.Fatalf("ListInboxes: %v", err)
	}
	if len(inboxes) != 0 {
		t.Fatalf("len(inboxes) = %d, want 0", len(inboxes))
	}
}

func TestListInboxes_Non200(t *testing.T) {
	// Non-200 is surfaced as *HTTPStatusError with Op "list inboxes" so the
	// caller (EnsureInbox) can wrap it and abort provisioning.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnauthorized, map[string]any{"error": "bad token"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	inboxes, err := c.ListInboxes()
	if inboxes != nil {
		t.Fatalf("inboxes = %+v, want nil", inboxes)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized || httpErr.Op != "list inboxes" {
		t.Errorf("err = %+v, want 401 'list inboxes'", httpErr)
	}
}

// --- CreateInbox -----------------------------------------------------------

func TestCreateInbox_PostsNameAndChannelWithWebhook(t *testing.T) {
	// The create body must carry name at the root and a nested channel object
	// of {type:"api", webhook_url:...}. When a webhook URL is given it rides in
	// the channel; this is what makes Chatwoot POST agent replies back to us.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/1/inboxes" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["name"] != "WhatsApp" {
			t.Errorf("name = %v, want WhatsApp", body["name"])
		}
		ch, ok := body["channel"].(map[string]any)
		if !ok {
			t.Fatalf("channel missing or wrong type: %v", body["channel"])
		}
		if ch["type"] != "api" {
			t.Errorf("channel.type = %v, want api", ch["type"])
		}
		if ch["webhook_url"] != "https://hook.example/webhook" {
			t.Errorf("channel.webhook_url = %v, want the passed URL", ch["webhook_url"])
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 42, "name": "WhatsApp"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	inbox, err := c.CreateInbox("WhatsApp", "https://hook.example/webhook")
	if err != nil {
		t.Fatalf("CreateInbox: %v", err)
	}
	if inbox.ID != 42 {
		t.Fatalf("inbox.ID = %d, want 42", inbox.ID)
	}
}

func TestCreateInbox_OmitsWebhookWhenEmpty(t *testing.T) {
	// webhook_url has the ,omitempty tag: an empty URL must drop the key
	// entirely (not send "webhook_url":""), so Chatwoot creates the inbox
	// without a callback rather than registering a blank one.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		ch, _ := body["channel"].(map[string]any)
		if _, ok := ch["webhook_url"]; ok {
			t.Errorf("webhook_url should be omitted when empty, got %v", ch["webhook_url"])
		}
		if ch["type"] != "api" {
			t.Errorf("channel.type = %v, want api", ch["type"])
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 7})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	inbox, err := c.CreateInbox("WhatsApp", "")
	if err != nil {
		t.Fatalf("CreateInbox: %v", err)
	}
	if inbox.ID != 7 {
		t.Fatalf("inbox.ID = %d, want 7", inbox.ID)
	}
}

func TestCreateInbox_DecodesBothShapesAndStatuses(t *testing.T) {
	// Chatwoot returns the inbox at the JSON root for this endpoint, but we
	// also tolerate a {"payload":Inbox} wrapper for forward-compatibility. Both
	// 200 and 201 are accepted (Chatwoot versions differ on which they return).
	tests := []struct {
		name   string
		status int
		body   any
	}{
		{"root-level inbox, 200", http.StatusOK, map[string]any{"id": 11}},
		{"root-level inbox, 201", http.StatusCreated, map[string]any{"id": 12}},
		{"payload-wrapped inbox, 200", http.StatusOK, map[string]any{"payload": map[string]any{"id": 13}}},
		{"payload-wrapped inbox, 201", http.StatusCreated, map[string]any{"payload": map[string]any{"id": 14}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tc.status, tc.body)
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			inbox, err := c.CreateInbox("WhatsApp", "")
			if err != nil {
				t.Fatalf("CreateInbox: %v", err)
			}
			if inbox == nil || inbox.ID == 0 {
				t.Fatalf("inbox = %+v, want non-zero ID", inbox)
			}
		})
	}
}

func TestCreateInbox_Non2xx(t *testing.T) {
	// Any status outside {200,201} is an HTTPStatusError with Op "create inbox".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"error": "name taken"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	inbox, err := c.CreateInbox("WhatsApp", "")
	if inbox != nil {
		t.Fatalf("inbox = %+v, want nil", inbox)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusUnprocessableEntity || httpErr.Op != "create inbox" {
		t.Errorf("err = %+v, want 422 'create inbox'", httpErr)
	}
}

func TestCreateInbox_ZeroIDOrUndecodable(t *testing.T) {
	// A 2xx whose body yields no valid (non-zero) ID in either shape is a plain
	// error (not HTTPStatusError) — there is nothing usable to provision with.
	tests := []struct {
		name string
		body string
	}{
		{"both shapes give zero id", `{"id":0,"payload":{"id":0}}`},
		{"unrelated json", `{"unexpected":"field"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			inbox, err := c.CreateInbox("WhatsApp", "")
			if inbox != nil {
				t.Fatalf("inbox = %+v, want nil", inbox)
			}
			if err == nil {
				t.Fatal("expected error for zero/undecodable id, got nil")
			}
			var httpErr *HTTPStatusError
			if errors.As(err, &httpErr) {
				t.Fatalf("err should not be HTTPStatusError, got %v", err)
			}
		})
	}
}

// --- FindLatestConversation ------------------------------------------------

func TestFindLatestConversation_ReturnsHighestIDRegardlessOfStatus(t *testing.T) {
	// Unlike FindConversation, this returns the highest-id conversation in this
	// inbox EVEN IF it is resolved — that is the conversation the reopen path
	// resurrects. A higher-id conversation in a different inbox must be skipped
	// so we never resurrect the wrong thread, and a lower-id open conversation
	// in the right inbox must lose to the higher-id resolved one.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/accounts/1/contacts/7/conversations" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []map[string]any{
				{"id": 10, "inbox_id": 2, "status": "open"},     // right inbox, lower id
				{"id": 99, "inbox_id": 88, "status": "open"},    // higher id but wrong inbox -> skip
				{"id": 20, "inbox_id": 2, "status": "resolved"}, // right inbox, highest -> winner
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindLatestConversation(7)
	if err != nil {
		t.Fatalf("FindLatestConversation: %v", err)
	}
	if conv == nil || conv.ID != 20 {
		t.Fatalf("conv = %+v, want ID 20 (highest in inbox 2, even though resolved)", conv)
	}
	if conv.Status != "resolved" {
		t.Errorf("Status = %q, want resolved (status is preserved, not filtered)", conv.Status)
	}
	if conv.ContactID != 7 {
		t.Errorf("ContactID = %d, want 7 (set from the argument)", conv.ContactID)
	}
	if conv.InboxID != 2 {
		t.Errorf("InboxID = %d, want 2", conv.InboxID)
	}
}

func TestFindLatestConversation_SkipsOtherInboxesEntirely(t *testing.T) {
	// When every conversation belongs to another inbox, there is no latest for
	// THIS inbox -> (nil, nil), so the reopen path falls through to creating a
	// fresh conversation in our inbox.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []map[string]any{
				{"id": 50, "inbox_id": 88, "status": "open"},
				{"id": 51, "inbox_id": 89, "status": "resolved"},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindLatestConversation(7)
	if err != nil {
		t.Fatalf("FindLatestConversation: %v", err)
	}
	if conv != nil {
		t.Fatalf("conv = %+v, want nil (no conversation in our inbox)", conv)
	}
}

func TestFindLatestConversation_EmptyPayloadReturnsNil(t *testing.T) {
	// No conversations at all -> (nil, nil).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"payload": []map[string]any{}})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindLatestConversation(7)
	if err != nil {
		t.Fatalf("FindLatestConversation: %v", err)
	}
	if conv != nil {
		t.Fatalf("conv = %+v, want nil", conv)
	}
}

func TestFindLatestConversation_Non200(t *testing.T) {
	// Shares listContactConversations with FindConversation, so the error Op is
	// the same "list contact conversations".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusBadGateway, map[string]any{"error": "down"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindLatestConversation(7)
	if conv != nil {
		t.Fatalf("conv = %+v, want nil", conv)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusBadGateway || httpErr.Op != "list contact conversations" {
		t.Errorf("err = %+v, want 502 'list contact conversations'", httpErr)
	}
}

// --- ToggleConversationStatus ----------------------------------------------

func TestToggleConversationStatus_PostsStatus(t *testing.T) {
	// POST to the toggle_status endpoint with a {"status":...} body. A 200 is
	// success (nil error). We assert the path, method, and body for each of the
	// statuses the reopen path actually sends.
	tests := []struct {
		name   string
		status string
	}{
		{"reopen to open", "open"},
		{"reopen to pending", "pending"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/1/conversations/55/toggle_status" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body["status"] != tc.status {
					t.Errorf("status = %q, want %q", body["status"], tc.status)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			if err := c.ToggleConversationStatus(55, tc.status); err != nil {
				t.Fatalf("ToggleConversationStatus: %v", err)
			}
		})
	}
}

func TestToggleConversationStatus_Non200(t *testing.T) {
	// Non-200 -> HTTPStatusError with Op "toggle conversation status".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]any{"error": "no such conversation"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	err := c.ToggleConversationStatus(55, "open")
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusNotFound || httpErr.Op != "toggle conversation status" {
		t.Errorf("err = %+v, want 404 'toggle conversation status'", httpErr)
	}
}

// --- CreateConversation: pending status ------------------------------------

func TestCreateConversation_PostsPendingWhenConfigured(t *testing.T) {
	// With ChatwootConversationPending=true a freshly created conversation is
	// opened as "pending" so it lands in the agent's unassigned queue. The
	// default "open" case is covered in client_methods_test.go; here we only
	// add the pending branch. Save/restore the global so sibling tests still
	// observe the default false.
	orig := config.ChatwootConversationPending
	config.ChatwootConversationPending = true
	defer func() { config.ChatwootConversationPending = orig }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/1/conversations" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["status"] != "pending" {
			t.Errorf("status = %v, want pending", body["status"])
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"payload": map[string]any{"id": 900}})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.CreateConversation(7, "")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if conv.ID != 900 {
		t.Fatalf("conv.ID = %d, want 900", conv.ID)
	}
}

// --- CreateMessage with MessageOptions (JSON path) -------------------------

func TestCreateMessage_OptionsJSONPath(t *testing.T) {
	// On the no-attachment JSON path, opts populate source_id and
	// content_attributes. source_id stamps the WAID anchor; content_attributes
	// carries reply metadata. Both have ,omitempty, so the zero options must
	// emit NEITHER key — that reproduces the pre-options behavior exactly.
	tests := []struct {
		name              string
		opts              MessageOptions
		wantSourceID      any  // nil means key must be absent
		wantHasContentAtt bool // whether content_attributes must be present
		wantPrivate       bool
	}{
		{
			name:              "no options: neither key present",
			opts:              MessageOptions{},
			wantSourceID:      nil,
			wantHasContentAtt: false,
			wantPrivate:       false,
		},
		{
			name:              "source_id only",
			opts:              MessageOptions{SourceID: "WAID:abc123"},
			wantSourceID:      "WAID:abc123",
			wantHasContentAtt: false,
			wantPrivate:       false,
		},
		{
			name: "content_attributes only",
			opts: MessageOptions{ContentAttributes: map[string]any{
				"in_reply_to_external_id": "WAID:x",
			}},
			wantSourceID:      nil,
			wantHasContentAtt: true,
			wantPrivate:       false,
		},
		{
			name: "both source_id and content_attributes",
			opts: MessageOptions{
				SourceID:          "WAID:abc123",
				ContentAttributes: map[string]any{"in_reply_to_external_id": "WAID:x"},
			},
			wantSourceID:      "WAID:abc123",
			wantHasContentAtt: true,
			wantPrivate:       false,
		},
		{
			name:              "private note",
			opts:              MessageOptions{Private: true},
			wantSourceID:      nil,
			wantHasContentAtt: false,
			wantPrivate:       true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if tc.wantSourceID == nil {
					if _, ok := body["source_id"]; ok {
						t.Errorf("source_id should be omitted, got %v", body["source_id"])
					}
				} else if body["source_id"] != tc.wantSourceID {
					t.Errorf("source_id = %v, want %v", body["source_id"], tc.wantSourceID)
				}
				if tc.wantHasContentAtt {
					ca, ok := body["content_attributes"].(map[string]any)
					if !ok {
						t.Fatalf("content_attributes missing or wrong type: %v", body["content_attributes"])
					}
					if ca["in_reply_to_external_id"] != "WAID:x" {
						t.Errorf("content_attributes.in_reply_to_external_id = %v, want WAID:x", ca["in_reply_to_external_id"])
					}
				} else if _, ok := body["content_attributes"]; ok {
					t.Errorf("content_attributes should be omitted, got %v", body["content_attributes"])
				}
				if body["private"] != tc.wantPrivate {
					t.Errorf("private = %v, want %v", body["private"], tc.wantPrivate)
				}
				writeJSON(t, w, http.StatusOK, map[string]any{"id": 1})
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			id, err := c.CreateMessage(5, "hello", "incoming", nil, tc.opts)
			if err != nil {
				t.Fatalf("CreateMessage: %v", err)
			}
			if id != 1 {
				t.Fatalf("id = %d, want 1", id)
			}
		})
	}
}

// --- CreateMessage with MessageOptions (multipart path) --------------------

func TestCreateMessage_OptionsMultipartPath(t *testing.T) {
	// On the attachment path, source_id and content_attributes become form
	// fields. content_attributes is written as a JSON string (not a nested
	// object, since multipart fields are flat). When options are zero, both
	// fields are absent (the WriteField calls are guarded by non-empty checks).
	tmp := filepath.Join(t.TempDir(), "a.oga")
	if err := os.WriteFile(tmp, []byte("X"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	tests := []struct {
		name              string
		opts              MessageOptions
		wantSourceID      string // "" means field must be absent
		wantContentAttrJS string // "" means field must be absent
		wantPrivate       string
	}{
		{
			name:              "no options: neither field present",
			opts:              MessageOptions{},
			wantSourceID:      "",
			wantContentAttrJS: "",
			wantPrivate:       "false",
		},
		{
			name:              "source_id present",
			opts:              MessageOptions{SourceID: "WAID:abc123"},
			wantSourceID:      "WAID:abc123",
			wantContentAttrJS: "",
			wantPrivate:       "false",
		},
		{
			name: "content_attributes present as JSON string",
			opts: MessageOptions{ContentAttributes: map[string]any{
				"in_reply_to_external_id": "WAID:x",
			}},
			wantSourceID:      "",
			wantContentAttrJS: `{"in_reply_to_external_id":"WAID:x"}`,
			wantPrivate:       "false",
		},
		{
			name:              "private note",
			opts:              MessageOptions{Private: true},
			wantSourceID:      "",
			wantContentAttrJS: "",
			wantPrivate:       "true",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil {
					t.Fatalf("ParseMediaType: %v", err)
				}
				mr := multipart.NewReader(r.Body, params["boundary"])
				var gotSourceID, gotContentAttr string
				var sawSourceID, sawContentAttr bool
				var gotPrivate string
				for {
					part, err := mr.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("NextPart: %v", err)
					}
					switch part.FormName() {
					case "source_id":
						data, _ := io.ReadAll(part)
						gotSourceID = string(data)
						sawSourceID = true
					case "content_attributes":
						data, _ := io.ReadAll(part)
						gotContentAttr = string(data)
						sawContentAttr = true
					case "private":
						data, _ := io.ReadAll(part)
						gotPrivate = string(data)
					}
				}
				if tc.wantSourceID == "" {
					if sawSourceID {
						t.Errorf("source_id field present (%q), want absent", gotSourceID)
					}
				} else {
					if !sawSourceID || gotSourceID != tc.wantSourceID {
						t.Errorf("source_id = %q (present=%v), want %q", gotSourceID, sawSourceID, tc.wantSourceID)
					}
				}
				if tc.wantContentAttrJS == "" {
					if sawContentAttr {
						t.Errorf("content_attributes field present (%q), want absent", gotContentAttr)
					}
				} else {
					if !sawContentAttr || gotContentAttr != tc.wantContentAttrJS {
						t.Errorf("content_attributes = %q (present=%v), want %q", gotContentAttr, sawContentAttr, tc.wantContentAttrJS)
					}
				}
				if gotPrivate != tc.wantPrivate {
					t.Errorf("private = %q, want %q", gotPrivate, tc.wantPrivate)
				}
				writeJSON(t, w, http.StatusOK, map[string]any{"id": 2})
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			id, err := c.CreateMessage(5, "caption", "incoming", []string{tmp}, tc.opts)
			if err != nil {
				t.Fatalf("CreateMessage: %v", err)
			}
			if id != 2 {
				t.Fatalf("id = %d, want 2", id)
			}
		})
	}
}
