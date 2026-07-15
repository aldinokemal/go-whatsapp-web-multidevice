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
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// newTestClient builds a Client pointed at the given test server. Every
// method under test reads BaseURL/APIToken/AccountID/InboxID, so this keeps
// each table case terse and consistent with the existing client_test.go.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	return &Client{
		BaseURL:    serverURL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: http.DefaultClient,
	}
}

// --- NewClient -------------------------------------------------------------

func TestNewClient_TrimsTrailingSlashAndMapsConfig(t *testing.T) {
	// NewClient reads package-level config globals. Mutating globals in a
	// test is only safe if we restore them afterwards, otherwise a sibling
	// test that constructs the default client would observe leaked state.
	origURL := config.ChatwootURL
	origToken := config.ChatwootAPIToken
	origAccount := config.ChatwootAccountID
	origInbox := config.ChatwootInboxID
	defer func() {
		config.ChatwootURL = origURL
		config.ChatwootAPIToken = origToken
		config.ChatwootAccountID = origAccount
		config.ChatwootInboxID = origInbox
	}()

	tests := []struct {
		name      string
		url       string
		token     string
		wantURL   string
		wantToken string
	}{
		// The trailing slash matters: endpoints are built with
		// fmt.Sprintf("%s/api/v1/...") so a stray slash would yield a
		// double-slash path. TrimRight removes any run of trailing slashes.
		{"single trailing slash", "https://chatwoot.example.com/", "tok-123", "https://chatwoot.example.com", "tok-123"},
		{"multiple trailing slashes", "https://chatwoot.example.com///", "tok-123", "https://chatwoot.example.com", "tok-123"},
		{"no trailing slash", "https://chatwoot.example.com", "tok-123", "https://chatwoot.example.com", "tok-123"},
		{"empty url stays empty", "", "tok-123", "", "tok-123"},
		// Tokens/URLs from Docker secret files, .env lines, or shell heredocs
		// commonly carry surrounding whitespace or a trailing newline. An
		// untrimmed token yields a malformed "api_access_token" header and a
		// 401 from Chatwoot (issue #674); a newline on the URL survives the
		// slash trim and corrupts every endpoint. Both must be trimmed.
		{"token with trailing newline", "https://chatwoot.example.com", "tok-123\n", "https://chatwoot.example.com", "tok-123"},
		{"token with surrounding spaces", "https://chatwoot.example.com", "  tok-123  ", "https://chatwoot.example.com", "tok-123"},
		{"url with trailing newline before slash", "https://chatwoot.example.com/\n", "tok-123", "https://chatwoot.example.com", "tok-123"},
		{"url with surrounding whitespace", "  https://chatwoot.example.com/  ", "tok-123", "https://chatwoot.example.com", "tok-123"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.ChatwootURL = tc.url
			config.ChatwootAPIToken = tc.token
			config.ChatwootAccountID = 7
			config.ChatwootInboxID = 9

			c := NewClient()

			if c.BaseURL != tc.wantURL {
				t.Errorf("BaseURL = %q, want %q", c.BaseURL, tc.wantURL)
			}
			if c.APIToken != tc.wantToken {
				t.Errorf("APIToken = %q, want %q", c.APIToken, tc.wantToken)
			}
			if c.AccountID != 7 {
				t.Errorf("AccountID = %d, want 7", c.AccountID)
			}
			if c.InboxID != 9 {
				t.Errorf("InboxID = %d, want 9", c.InboxID)
			}
			if c.HTTPClient == nil {
				t.Fatal("HTTPClient must not be nil")
			}
		})
	}
}

// --- IsConfigured ----------------------------------------------------------

func TestIsConfigured(t *testing.T) {
	tests := []struct {
		name string
		c    Client
		want bool
	}{
		// Only the fully-populated client is configured. Each false case
		// zeroes exactly one required field to prove every field is checked.
		{"all set", Client{BaseURL: "u", APIToken: "t", AccountID: 1, InboxID: 2}, true},
		{"missing BaseURL", Client{BaseURL: "", APIToken: "t", AccountID: 1, InboxID: 2}, false},
		{"missing APIToken", Client{BaseURL: "u", APIToken: "", AccountID: 1, InboxID: 2}, false},
		{"zero AccountID", Client{BaseURL: "u", APIToken: "t", AccountID: 0, InboxID: 2}, false},
		{"zero InboxID", Client{BaseURL: "u", APIToken: "t", AccountID: 1, InboxID: 0}, false},
		{"all empty", Client{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsConfigured(); got != tc.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- FindContactByIdentifier ----------------------------------------------

func TestFindContactByIdentifier_PhoneMatch(t *testing.T) {
	// 1:1 path: the search query is the E.164-normalized phone, and the
	// match is made on PhoneNumber equality. A contact whose phone differs
	// must be ignored so we never bind to the wrong person.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/accounts/1/contacts/search" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if got := r.URL.Query().Get("q"); got != "+6281234567890" {
			t.Fatalf("search q = %q, want +6281234567890", got)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []Contact{
				{ID: 1, PhoneNumber: "+6289999999999"}, // wrong phone, must be skipped
				{ID: 2, PhoneNumber: "+6281234567890", Name: "Match"},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindContactByIdentifier("6281234567890", false)
	if err != nil {
		t.Fatalf("FindContactByIdentifier: %v", err)
	}
	if contact == nil || contact.ID != 2 {
		t.Fatalf("contact = %+v, want ID 2", contact)
	}
}

func TestFindContactByIdentifier_PhoneNoMatch(t *testing.T) {
	// When the search returns contacts but none match the normalized phone,
	// the method returns (nil, nil) — not found is not an error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []Contact{{ID: 1, PhoneNumber: "+6280000000000"}},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindContactByIdentifier("6281234567890", false)
	if err != nil {
		t.Fatalf("FindContactByIdentifier: %v", err)
	}
	if contact != nil {
		t.Fatalf("contact = %+v, want nil", contact)
	}
}

func TestFindContactByIdentifier_GroupMatchByIdentifier(t *testing.T) {
	// Group path: the raw JID is used as the search query (no phone
	// normalization), and a match is made on the Identifier field.
	const groupJID = "120363123456789@g.us"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != groupJID {
			t.Fatalf("search q = %q, want %s", got, groupJID)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []Contact{{ID: 55, Identifier: groupJID, Name: "Group"}},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindContactByIdentifier(groupJID, true)
	if err != nil {
		t.Fatalf("FindContactByIdentifier: %v", err)
	}
	if contact == nil || contact.ID != 55 {
		t.Fatalf("contact = %+v, want ID 55", contact)
	}
}

func TestFindContactByIdentifier_GroupMatchByCustomAttribute(t *testing.T) {
	// Group path fallback: a contact whose Identifier field is blank can
	// still match via custom_attributes.gowa_whatsapp_jid, which is the
	// attribute CreateContact stamps on every contact it creates.
	const groupJID = "120363123456789@g.us"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []Contact{{
				ID:               66,
				Identifier:       "", // not matched on this field
				CustomAttributes: map[string]any{"gowa_whatsapp_jid": groupJID},
			}},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindContactByIdentifier(groupJID, true)
	if err != nil {
		t.Fatalf("FindContactByIdentifier: %v", err)
	}
	if contact == nil || contact.ID != 66 {
		t.Fatalf("contact = %+v, want ID 66", contact)
	}
}

func TestFindContactByIdentifier_LidUsesIdentifierNotPhone(t *testing.T) {
	// An @lid JID is a WhatsApp linked-device ID, not a phone number. The
	// identifier-based branch must be taken: the raw @lid value is the
	// search query and the match is on Identifier, never PhoneNumber.
	const lidJID = "11223344556677@lid"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != lidJID {
			t.Fatalf("search q = %q, want %s (raw @lid, not normalized phone)", got, lidJID)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": []Contact{
				// A contact whose phone happens to equal the normalized lid
				// must NOT match, proving the phone branch is not taken.
				{ID: 1, PhoneNumber: "+11223344556677"},
				{ID: 2, Identifier: lidJID, Name: "Lid"},
			},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindContactByIdentifier(lidJID, false)
	if err != nil {
		t.Fatalf("FindContactByIdentifier: %v", err)
	}
	if contact == nil || contact.ID != 2 {
		t.Fatalf("contact = %+v, want ID 2", contact)
	}
}

func TestFindContactByIdentifier_EmptyPayload(t *testing.T) {
	// An empty payload yields (nil, nil): the loop never matches and the
	// trailing return is reached.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"payload": []Contact{}})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindContactByIdentifier("6281234567890", false)
	if err != nil {
		t.Fatalf("FindContactByIdentifier: %v", err)
	}
	if contact != nil {
		t.Fatalf("contact = %+v, want nil", contact)
	}
}

func TestFindContactByIdentifier_Non200(t *testing.T) {
	// A non-200 status is surfaced as an *HTTPStatusError carrying the code
	// and Op so retrySyncOp can classify it as transient or fatal.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "boom"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindContactByIdentifier("6281234567890", false)
	if contact != nil {
		t.Fatalf("contact = %+v, want nil", contact)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want 500", httpErr.StatusCode)
	}
	if httpErr.Op != "search contact" {
		t.Errorf("Op = %q, want 'search contact'", httpErr.Op)
	}
}

// --- CreateContact ---------------------------------------------------------

func TestCreateContact_IndividualSendsPhone(t *testing.T) {
	// 1:1 contacts are keyed on their E.164 phone. The request must set
	// phone_number to the normalized value, leave identifier empty (omitted
	// by ,omitempty), and stamp custom_attributes.gowa_whatsapp_jid with the
	// raw identifier so later identifier-based lookups can still match.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/1/contacts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["phone_number"] != "+6281234567890" {
			t.Errorf("phone_number = %v, want +6281234567890", body["phone_number"])
		}
		if _, ok := body["identifier"]; ok {
			t.Errorf("identifier should be omitted for 1:1, got %v", body["identifier"])
		}
		ca, _ := body["custom_attributes"].(map[string]any)
		if ca["gowa_whatsapp_jid"] != "6281234567890" {
			t.Errorf("gowa_whatsapp_jid = %v, want 6281234567890", ca["gowa_whatsapp_jid"])
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": map[string]any{"contact": map[string]any{"id": 100, "name": "Alice"}},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.CreateContact("Alice", "6281234567890", false)
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if contact.ID != 100 {
		t.Fatalf("contact.ID = %d, want 100", contact.ID)
	}
}

func TestCreateContact_GroupAndLidSendIdentifier(t *testing.T) {
	// Both group JIDs and @lid JIDs use the identifier field and leave
	// phone_number empty (it is non-phone data).
	tests := []struct {
		name       string
		identifier string
		isGroup    bool
	}{
		{"group jid", "120363123456789@g.us", true},
		{"lid jid", "11223344556677@lid", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body["identifier"] != tc.identifier {
					t.Errorf("identifier = %v, want %s", body["identifier"], tc.identifier)
				}
				if _, ok := body["phone_number"]; ok {
					t.Errorf("phone_number should be omitted, got %v", body["phone_number"])
				}
				writeJSON(t, w, http.StatusOK, map[string]any{
					"payload": map[string]any{"contact": map[string]any{"id": 200}},
				})
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			contact, err := c.CreateContact("name", tc.identifier, tc.isGroup)
			if err != nil {
				t.Fatalf("CreateContact: %v", err)
			}
			if contact.ID != 200 {
				t.Fatalf("contact.ID = %d, want 200", contact.ID)
			}
		})
	}
}

func TestCreateContact_DecodesAllResponseShapes(t *testing.T) {
	// Chatwoot's contact-create response shape has drifted across versions:
	// nested {payload:{contact}}, flat {payload:Contact}, and bare Contact
	// at the root. All three must decode, and both 200 and 201 are accepted.
	tests := []struct {
		name   string
		status int
		body   any
	}{
		{
			name:   "nested payload.contact, 200",
			status: http.StatusOK,
			body:   map[string]any{"payload": map[string]any{"contact": map[string]any{"id": 1}}},
		},
		{
			name:   "flat payload contact, 201",
			status: http.StatusCreated,
			body:   map[string]any{"payload": map[string]any{"id": 2}},
		},
		{
			name:   "root-level contact, 200",
			status: http.StatusOK,
			body:   map[string]any{"id": 3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tc.status, tc.body)
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			contact, err := c.CreateContact("name", "6281234567890", false)
			if err != nil {
				t.Fatalf("CreateContact: %v", err)
			}
			if contact == nil || contact.ID == 0 {
				t.Fatalf("contact = %+v, want non-zero ID", contact)
			}
		})
	}
}

func TestCreateContact_Non2xx(t *testing.T) {
	// Any status outside {200,201} is an HTTPStatusError with Op "create contact".
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"error": "taken"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.CreateContact("name", "6281234567890", false)
	if contact != nil {
		t.Fatalf("contact = %+v, want nil", contact)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusUnprocessableEntity || httpErr.Op != "create contact" {
		t.Errorf("err = %+v, want 422 'create contact'", httpErr)
	}
}

func TestCreateContact_ZeroIDOrUndecodable(t *testing.T) {
	// A 2xx response whose body yields no valid (non-zero) ID is a hard
	// failure — there is nothing usable to return. This covers both a body
	// with id:0 and a body that does not unmarshal into any contact shape.
	tests := []struct {
		name string
		body string
	}{
		{"all shapes give zero id", `{"payload":{"contact":{"id":0}}}`},
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
			contact, err := c.CreateContact("name", "6281234567890", false)
			if contact != nil {
				t.Fatalf("contact = %+v, want nil", contact)
			}
			if err == nil {
				t.Fatal("expected error for zero/undecodable id, got nil")
			}
			// This is a plain fmt.Errorf, not an HTTPStatusError.
			var httpErr *HTTPStatusError
			if errors.As(err, &httpErr) {
				t.Fatalf("err should not be HTTPStatusError, got %v", err)
			}
		})
	}
}

// --- UpdateContactName -----------------------------------------------------

func TestUpdateContactName(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		// PUT to the contact endpoint accepts both 200 and 204 (Chatwoot
		// versions differ). Anything else is an error.
		{"200 ok", http.StatusOK, false},
		{"204 no content", http.StatusNoContent, false},
		{"500 error", http.StatusInternalServerError, true},
		{"422 error", http.StatusUnprocessableEntity, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/api/v1/accounts/1/contacts/42" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body["name"] != "New Name" {
					t.Errorf("name = %v, want New Name", body["name"])
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			err := c.UpdateContactName(42, "New Name")
			if tc.wantErr {
				var httpErr *HTTPStatusError
				if !errors.As(err, &httpErr) {
					t.Fatalf("err = %v, want *HTTPStatusError", err)
				}
				if httpErr.Op != "update contact" {
					t.Errorf("Op = %q, want 'update contact'", httpErr.Op)
				}
			} else if err != nil {
				t.Fatalf("UpdateContactName: %v", err)
			}
		})
	}
}

// --- selectOpenConversation ------------------------------------------------

func TestSelectOpenConversation_ReturnsFirstOpenMatchingInbox(t *testing.T) {
	// The first conversation that is both in this client's inbox AND not
	// resolved wins. Earlier entries that fail either condition are skipped:
	// a resolved conversation in the right inbox, and an open conversation
	// in a different inbox, both come before the valid one.
	items := []conversationListItem{
		{ID: 10, InboxID: 2, Status: "resolved"}, // right inbox, resolved -> skip
		{ID: 11, InboxID: 99, Status: "open"},    // wrong inbox -> skip
		{ID: 12, InboxID: 2, Status: "open"},     // match
		{ID: 13, InboxID: 2, Status: "pending"},  // also valid but later
	}
	conv := selectOpenConversation(items, 2, 7)
	if conv == nil || conv.ID != 12 {
		t.Fatalf("conv = %+v, want ID 12", conv)
	}
	if conv.ContactID != 7 {
		t.Errorf("ContactID = %d, want 7 (set from the argument, not the payload)", conv.ContactID)
	}
	if conv.InboxID != 2 || conv.Status != "open" {
		t.Errorf("conv = %+v, want inbox 2 status open", conv)
	}
}

func TestSelectOpenConversation_AcceptsNonResolvedStatuses(t *testing.T) {
	// "resolved" is the only excluded status; any other status (here
	// "pending") for the right inbox is considered an active conversation.
	items := []conversationListItem{
		{ID: 20, InboxID: 2, Status: "pending"},
	}
	conv := selectOpenConversation(items, 2, 7)
	if conv == nil || conv.ID != 20 {
		t.Fatalf("conv = %+v, want ID 20", conv)
	}
}

func TestSelectOpenConversation_NoneMatch(t *testing.T) {
	// All candidates are either resolved or in the wrong inbox -> nil.
	items := []conversationListItem{
		{ID: 30, InboxID: 2, Status: "resolved"},
		{ID: 31, InboxID: 99, Status: "open"},
	}
	conv := selectOpenConversation(items, 2, 7)
	if conv != nil {
		t.Fatalf("conv = %+v, want nil", conv)
	}
}

// --- CreateConversation ----------------------------------------------------

func TestCreateConversation_PostsOpenStatus(t *testing.T) {
	// The create request must carry this client's inbox, the contact id, and
	// status "open". The {payload:Conversation} shape decodes here.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/1/conversations" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["inbox_id"] != float64(2) {
			t.Errorf("inbox_id = %v, want 2", body["inbox_id"])
		}
		if body["contact_id"] != float64(7) {
			t.Errorf("contact_id = %v, want 7", body["contact_id"])
		}
		if body["status"] != "open" {
			t.Errorf("status = %v, want open", body["status"])
		}
		if body["source_id"] != "628999@s.whatsapp.net" {
			t.Errorf("source_id = %v, want 628999@s.whatsapp.net", body["source_id"])
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"payload": map[string]any{"id": 500},
		})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.CreateConversation(7, "628999@s.whatsapp.net")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if conv.ID != 500 {
		t.Fatalf("conv.ID = %d, want 500", conv.ID)
	}
}

func TestCreateConversation_RootShape(t *testing.T) {
	// Fallback decode path: the conversation lives at the JSON root.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 501})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.CreateConversation(7, "")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if conv.ID != 501 {
		t.Fatalf("conv.ID = %d, want 501", conv.ID)
	}
}

func TestCreateConversation_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "x"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.CreateConversation(7, "")
	if conv != nil {
		t.Fatalf("conv = %+v, want nil", conv)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.Op != "create conversation" {
		t.Errorf("Op = %q, want 'create conversation'", httpErr.Op)
	}
}

func TestCreateConversation_ZeroID(t *testing.T) {
	// A 200 with no usable ID in either shape is an error (not HTTPStatusError).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"payload":{"id":0}}`)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.CreateConversation(7, "")
	if conv != nil {
		t.Fatalf("conv = %+v, want nil", conv)
	}
	if err == nil {
		t.Fatal("expected error for zero id, got nil")
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		t.Fatalf("err should not be HTTPStatusError, got %v", err)
	}
}

// --- FindOrCreateConversation ---------------------------------------------

func TestFindOrCreateConversation_ReturnsFoundWithoutCreating(t *testing.T) {
	// When an open conversation already exists, no POST is made.
	createCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/7/conversations":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []map[string]any{{"id": 600, "inbox_id": 2, "status": "open"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations":
			createCalls++
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": map[string]any{"id": 999}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if conv.ID != 600 {
		t.Fatalf("conv.ID = %d, want 600 (found, not created)", conv.ID)
	}
	if createCalls != 0 {
		t.Fatalf("create calls = %d, want 0", createCalls)
	}
}

func TestFindOrCreateConversation_CreatesWhenNotFound(t *testing.T) {
	// No open conversation -> the method falls through to CreateConversation.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": []map[string]any{}})
		case r.Method == http.MethodPost:
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": map[string]any{"id": 700}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if conv.ID != 700 {
		t.Fatalf("conv.ID = %d, want 700 (created)", conv.ID)
	}
}

func TestFindOrCreateConversation_SwallowsFindErrorThenCreates(t *testing.T) {
	// Listing conversations failing is logged but NOT propagated: the method
	// still proceeds to create. This codifies the current swallow-and-create
	// behavior — a find error must not block conversation creation.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			writeJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "find failed"})
		case r.Method == http.MethodPost:
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": map[string]any{"id": 800}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation should swallow find error, got: %v", err)
	}
	if conv.ID != 800 {
		t.Fatalf("conv.ID = %d, want 800 (created after find error)", conv.ID)
	}
}

// --- FindOrCreateContact 422 recovery -------------------------------------

func TestFindOrCreateContact_RecoversFrom422ByRefinding(t *testing.T) {
	// A concurrent creator wins the race between our find and create, so create
	// returns 422 (identifier already taken). We re-find once and reuse the
	// now-existing contact instead of dropping the message.
	searchCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
			searchCalls++
			if searchCalls == 1 {
				writeJSON(t, w, http.StatusOK, map[string]any{"payload": []map[string]any{}})
				return
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []map[string]any{{"id": 42, "identifier": "12345@g.us"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/contacts":
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]any{
				"message": "Identifier has already been taken",
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	contact, err := c.FindOrCreateContact("Group", "12345@g.us", true)
	if err != nil {
		t.Fatalf("FindOrCreateContact should recover from 422, got: %v", err)
	}
	if contact == nil || contact.ID != 42 {
		t.Fatalf("contact = %+v, want id 42 (re-found after 422)", contact)
	}
	if searchCalls != 2 {
		t.Fatalf("search calls = %d, want 2 (initial find + 422 recovery re-find)", searchCalls)
	}
}

func TestFindOrCreateContact_PropagatesNon422CreateError(t *testing.T) {
	// A non-422 create failure (e.g. 500) is NOT recovered: it surfaces so the
	// caller treats it as transient and retries.
	searchCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/search":
			searchCalls++
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": []map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/contacts":
			writeJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "boom"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	_, err := c.FindOrCreateContact("Group", "12345@g.us", true)
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("err = %v, want 500 HTTPStatusError", err)
	}
	if searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1 (no recovery re-find for non-422)", searchCalls)
	}
}

// --- CreateMessage (no attachments) ---------------------------------------

func TestCreateMessage_PlainPostsAndReturnsID(t *testing.T) {
	// Plain text message: posts content/message_type with private=false, and
	// returns the id from a 200 response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/1/conversations/5/messages" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["content"] != "hello" {
			t.Errorf("content = %v, want hello", body["content"])
		}
		if body["message_type"] != "outgoing" {
			t.Errorf("message_type = %v, want outgoing", body["message_type"])
		}
		if body["private"] != false {
			t.Errorf("private = %v, want false", body["private"])
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 321})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id, err := c.CreateMessage(5, "hello", "outgoing", nil, MessageOptions{})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if id != 321 {
		t.Fatalf("id = %d, want 321", id)
	}
}

func TestCreateMessage_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusTooManyRequests, map[string]any{"error": "slow down"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id, err := c.CreateMessage(5, "hello", "outgoing", nil, MessageOptions{})
	if id != 0 {
		t.Fatalf("id = %d, want 0", id)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests || httpErr.Op != "create message" {
		t.Errorf("err = %+v, want 429 'create message'", httpErr)
	}
}

func TestCreateMessage_MissingIDReturnsZeroNoError(t *testing.T) {
	// A 200 whose body lacks an id (or has id:0) returns (0, nil): the send
	// succeeded but no id is available to register for echo-dedup.
	tests := []struct {
		name string
		body string
	}{
		{"empty object", `{}`},
		{"explicit zero id", `{"id":0}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			c := newTestClient(t, server.URL)
			id, err := c.CreateMessage(5, "hello", "outgoing", nil, MessageOptions{})
			if err != nil {
				t.Fatalf("CreateMessage: %v", err)
			}
			if id != 0 {
				t.Fatalf("id = %d, want 0", id)
			}
		})
	}
}

func TestDeleteMessage_SendsDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/accounts/1/conversations/5/messages/9" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("api_access_token"); got != "token" {
			t.Fatalf("api token = %q, want token", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := c.DeleteMessage(5, 9); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
}

func TestUpdateLastSeen_UsesInboxIdentifierAndContactSource(t *testing.T) {
	var sawUpdate bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts/1/inboxes":
			if r.Method != http.MethodGet {
				t.Fatalf("list inboxes method = %s", r.Method)
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{
					{ID: 2, Name: "WhatsApp", InboxIdentifier: "api-inbox-token"},
				},
			})
		case "/public/api/v1/inboxes/api-inbox-token/contacts/628123456789@s.whatsapp.net/conversations/5/update_last_seen":
			if r.Method != http.MethodPost {
				t.Fatalf("update_last_seen method = %s", r.Method)
			}
			sawUpdate = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := c.UpdateLastSeen(5, "628123456789@s.whatsapp.net"); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}
	if !sawUpdate {
		t.Fatal("expected update_last_seen request")
	}
}

// --- createMessageWithAttachments (via CreateMessage with attachments) -----

func TestCreateMessage_WithAttachmentUploadsMultipart(t *testing.T) {
	// With attachments, the request becomes multipart/form-data. We assert
	// the text fields ride along, the file part carries the right filename
	// and Content-Type, and the returned id is decoded from the response.
	tmp := filepath.Join(t.TempDir(), "voice.oga")
	if err := os.WriteFile(tmp, []byte("OGGDATA"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			t.Fatalf("Content-Type = %q, want multipart/form-data", ct)
		}
		mediaType, params, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("ParseMediaType(%q): %v", ct, err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		sawContent, sawType, sawPrivate, sawFile := false, false, false, false
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart: %v", err)
			}
			switch part.FormName() {
			case "content":
				data, _ := io.ReadAll(part)
				if string(data) != "caption" {
					t.Errorf("content = %q, want caption", data)
				}
				sawContent = true
			case "message_type":
				data, _ := io.ReadAll(part)
				if string(data) != "incoming" {
					t.Errorf("message_type = %q, want incoming", data)
				}
				sawType = true
			case "private":
				data, _ := io.ReadAll(part)
				if string(data) != "false" {
					t.Errorf("private = %q, want false", data)
				}
				sawPrivate = true
			case "attachments[]":
				if part.FileName() != "voice.oga" {
					t.Errorf("filename = %q, want voice.oga", part.FileName())
				}
				// .oga must be tagged audio/ogg so Chatwoot renders the
				// voice note inline rather than as a generic download.
				if got := part.Header.Get("Content-Type"); got != "audio/ogg" {
					t.Errorf("attachment Content-Type = %q, want audio/ogg", got)
				}
				data, _ := io.ReadAll(part)
				if string(data) != "OGGDATA" {
					t.Errorf("attachment body = %q, want OGGDATA", data)
				}
				sawFile = true
			}
		}
		if !sawContent || !sawType || !sawPrivate || !sawFile {
			t.Fatalf("missing parts: content=%v type=%v private=%v file=%v", sawContent, sawType, sawPrivate, sawFile)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 4242})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id, err := c.CreateMessage(5, "caption", "incoming", []string{tmp}, MessageOptions{})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if id != 4242 {
		t.Fatalf("id = %d, want 4242", id)
	}
}

func TestCreateMessage_AttachmentUnknownExtensionUsesOctetStream(t *testing.T) {
	// An extension with no known MIME mapping falls back to
	// application/octet-stream.
	tmp := filepath.Join(t.TempDir(), "blob.xyz123")
	if err := os.WriteFile(tmp, []byte("RAW"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		found := false
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart: %v", err)
			}
			if part.FormName() == "attachments[]" {
				if got := part.Header.Get("Content-Type"); got != "application/octet-stream" {
					t.Errorf("Content-Type = %q, want application/octet-stream", got)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("attachment part not found")
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"id": 1})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id, err := c.CreateMessage(5, "x", "incoming", []string{tmp}, MessageOptions{})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if id != 1 {
		t.Fatalf("id = %d, want 1", id)
	}
}

func TestCreateMessage_AttachmentMissingFileFailsBeforeRequest(t *testing.T) {
	// A missing local attachment must fail the send before Chatwoot receives a
	// partial message without the file the operator expected to deliver.
	missing := filepath.Join(t.TempDir(), "does-not-exist.png")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Fatal("CreateMessage should not call Chatwoot when attachment open fails")
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	_, err := c.CreateMessage(5, "text only", "incoming", []string{missing}, MessageOptions{})
	if err == nil {
		t.Fatal("CreateMessage error = nil, want missing attachment error")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestCreateMessage_AttachmentNon200(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "a.oga")
	if err := os.WriteFile(tmp, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusServiceUnavailable, map[string]any{"error": "x"})
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id, err := c.CreateMessage(5, "x", "incoming", []string{tmp}, MessageOptions{})
	if id != 0 {
		t.Fatalf("id = %d, want 0", id)
	}
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *HTTPStatusError", err)
	}
	if httpErr.StatusCode != http.StatusServiceUnavailable || httpErr.Op != "create message with attachments" {
		t.Errorf("err = %+v, want 503 'create message with attachments'", httpErr)
	}
}

func TestCreateMessage_AttachmentMissingIDReturnsZero(t *testing.T) {
	// As with the plain path, a 200 without a usable id yields (0, nil).
	tmp := filepath.Join(t.TempDir(), "a.oga")
	if err := os.WriteFile(tmp, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	id, err := c.CreateMessage(5, "x", "incoming", []string{tmp}, MessageOptions{})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if id != 0 {
		t.Fatalf("id = %d, want 0", id)
	}
}

// --- Echo dedup map (MarkMessageAsSent / IsMessageSentByUs) ----------------

func TestEchoDedup_MapBehavior(t *testing.T) {
	const acc = 1
	// MarkMessageAsSent(_, 0) is a no-op and IsMessageSentByUs(_, 0) is always
	// false: id 0 is the "no id" sentinel returned by CreateMessage and must
	// never be treated as a tracked message.
	MarkMessageAsSent(acc, 0)
	if IsMessageSentByUs(acc, 0) {
		t.Error("IsMessageSentByUs(acc, 0) = true, want false")
	}

	// A real id, once marked, is recognized as ours. We use a large, fixed
	// id unlikely to collide with anything (the map is package-global and
	// shared, but this exact value is only touched here). IsMessageSentByUs
	// does not delete on read, so it stays true on a second check — Chatwoot
	// fires multiple webhook events for one message.
	const id = 987654321
	if IsMessageSentByUs(acc, id) {
		t.Fatalf("IsMessageSentByUs(%d) = true before marking, want false", id)
	}
	MarkMessageAsSent(acc, id)
	if !IsMessageSentByUs(acc, id) {
		t.Fatalf("IsMessageSentByUs(%d) = false after marking, want true", id)
	}
	if !IsMessageSentByUs(acc, id) {
		t.Fatalf("IsMessageSentByUs(%d) = false on second check, want true (no delete-on-read)", id)
	}

	// An id that was never marked is not ours.
	if IsMessageSentByUs(acc, 123456789) {
		t.Error("IsMessageSentByUs(unknown) = true, want false")
	}
}

// TestEchoDedup_AccountPartitioned guards multi-account routing: the same
// numeric Chatwoot message id in two different accounts must not collide, or a
// message we sent in account A would suppress a genuine agent reply with the
// same id in account B (dropped reply).
func TestEchoDedup_AccountPartitioned(t *testing.T) {
	const id = 555111222
	const accA, accB = 11, 22

	MarkMessageAsSent(accA, id)
	if !IsMessageSentByUs(accA, id) {
		t.Fatalf("account A id %d should be ours after marking", id)
	}
	if IsMessageSentByUs(accB, id) {
		t.Fatalf("account B id %d must NOT be considered ours (cross-account collision)", id)
	}
}
