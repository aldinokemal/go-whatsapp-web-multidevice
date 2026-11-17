package chatwoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// reopenCounters tallies the calls FindOrCreateConversation makes against the
// fake Chatwoot server so each case can assert exactly which paths ran. Listing
// conversations hits the same GET endpoint for open and latest selection, so we
// only distinguish create POSTs from toggle POSTs (the behaviorally meaningful
// branches).
type reopenCounters struct {
	createCalls int
	toggleCalls int
	toggleBody  string
}

// reopenServer builds an httptest server that serves the contact's
// conversation list (for open and latest selection), a toggle_status endpoint,
// and a conversation-create endpoint, recording calls into the returned
// counters. listPayload is the conversation list returned by the GET; toggleOK
// controls whether toggle_status succeeds.
func reopenServer(t *testing.T, listPayload []map[string]any, toggleOK bool, createID int) (*httptest.Server, *reopenCounters) {
	t.Helper()
	counters := &reopenCounters{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1/contacts/7/conversations":
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": listPayload})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations/20/toggle_status":
			counters.toggleCalls++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode toggle body: %v", err)
			}
			counters.toggleBody = body["status"]
			if toggleOK {
				w.WriteHeader(http.StatusOK)
			} else {
				writeJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "toggle failed"})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts/1/conversations":
			counters.createCalls++
			writeJSON(t, w, http.StatusOK, map[string]any{"payload": map[string]any{"id": createID}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	return server, counters
}

func TestFindOrCreateConversation_ReopenReturnsOpenWithoutReopening(t *testing.T) {
	// (a) When an OPEN conversation exists, it is returned immediately: no
	// create, no toggle — even with reopen enabled. There is nothing to
	// resurrect.
	origReopen := config.ChatwootReopenConversation
	config.ChatwootReopenConversation = true
	defer func() { config.ChatwootReopenConversation = origReopen }()

	server, counters := reopenServer(t, []map[string]any{
		{"id": 12, "inbox_id": 2, "status": "open"},
	}, true, 999)
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if conv.ID != 12 || conv.Status != "open" {
		t.Fatalf("conv = %+v, want id 12 status open", conv)
	}
	if counters.toggleCalls != 0 {
		t.Errorf("toggle calls = %d, want 0 (open conv needs no reopen)", counters.toggleCalls)
	}
	if counters.createCalls != 0 {
		t.Errorf("create calls = %d, want 0 (found existing open conv)", counters.createCalls)
	}
}

func TestFindOrCreateConversation_ReopenResolvedTogglesOpen(t *testing.T) {
	// (b) No OPEN conversation, but a RESOLVED one exists and reopen=true:
	// the latest conversation is selected, ToggleConversationStatus is called
	// with "open", and the returned conv carries the UPDATED status.
	// No new conversation is created.
	origReopen := config.ChatwootReopenConversation
	origPending := config.ChatwootConversationPending
	config.ChatwootReopenConversation = true
	config.ChatwootConversationPending = false
	defer func() {
		config.ChatwootReopenConversation = origReopen
		config.ChatwootConversationPending = origPending
	}()

	server, counters := reopenServer(t, []map[string]any{
		{"id": 20, "inbox_id": 2, "status": "resolved"},
	}, true, 999)
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if conv.ID != 20 {
		t.Fatalf("conv.ID = %d, want 20 (reopened, not newly created)", conv.ID)
	}
	if conv.Status != "open" {
		t.Errorf("Status = %q, want open (updated after toggle)", conv.Status)
	}
	if counters.toggleCalls != 1 {
		t.Errorf("toggle calls = %d, want 1", counters.toggleCalls)
	}
	if counters.toggleBody != "open" {
		t.Errorf("toggle status = %q, want open", counters.toggleBody)
	}
	if counters.createCalls != 0 {
		t.Errorf("create calls = %d, want 0", counters.createCalls)
	}
}

func TestFindOrCreateConversation_ReopenResolvedTogglesPendingWhenConfigured(t *testing.T) {
	// (b') Same reopen path but with ConversationPending=true: the resolved
	// conversation is toggled to "pending" instead of "open", routing the
	// returning contact to the unassigned queue.
	origReopen := config.ChatwootReopenConversation
	origPending := config.ChatwootConversationPending
	config.ChatwootReopenConversation = true
	config.ChatwootConversationPending = true
	defer func() {
		config.ChatwootReopenConversation = origReopen
		config.ChatwootConversationPending = origPending
	}()

	server, counters := reopenServer(t, []map[string]any{
		{"id": 20, "inbox_id": 2, "status": "resolved"},
	}, true, 999)
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if conv.ID != 20 || conv.Status != "pending" {
		t.Fatalf("conv = %+v, want id 20 status pending", conv)
	}
	if counters.toggleBody != "pending" {
		t.Errorf("toggle status = %q, want pending", counters.toggleBody)
	}
	if counters.createCalls != 0 {
		t.Errorf("create calls = %d, want 0", counters.createCalls)
	}
}

func TestFindOrCreateConversation_ReopenNoConversationFallsThroughToCreate(t *testing.T) {
	// (c) reopen=true but the contact has NO conversation at all in our inbox:
	// latest selection returns nil, so the method falls through to
	// CreateConversation. No toggle happens.
	origReopen := config.ChatwootReopenConversation
	config.ChatwootReopenConversation = true
	defer func() { config.ChatwootReopenConversation = origReopen }()

	server, counters := reopenServer(t, []map[string]any{}, true, 700)
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if conv.ID != 700 {
		t.Fatalf("conv.ID = %d, want 700 (created)", conv.ID)
	}
	if counters.toggleCalls != 0 {
		t.Errorf("toggle calls = %d, want 0", counters.toggleCalls)
	}
	if counters.createCalls != 1 {
		t.Errorf("create calls = %d, want 1", counters.createCalls)
	}
}

func TestFindOrCreateConversation_ReopenDisabledSkipsLatestAndToggles(t *testing.T) {
	// (d) reopen=false: even though a resolved conversation exists for the
	// contact, the reopen branch is skipped entirely — latest selection is
	// never consulted and no toggle happens. With no OPEN conversation found,
	// the method goes straight to creating a new one.
	origReopen := config.ChatwootReopenConversation
	config.ChatwootReopenConversation = false
	defer func() { config.ChatwootReopenConversation = origReopen }()

	server, counters := reopenServer(t, []map[string]any{
		{"id": 20, "inbox_id": 2, "status": "resolved"},
	}, true, 701)
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation: %v", err)
	}
	if conv.ID != 701 {
		t.Fatalf("conv.ID = %d, want 701 (created, reopen disabled)", conv.ID)
	}
	if counters.toggleCalls != 0 {
		t.Errorf("toggle calls = %d, want 0 (reopen disabled)", counters.toggleCalls)
	}
	if counters.createCalls != 1 {
		t.Errorf("create calls = %d, want 1", counters.createCalls)
	}
}

func TestFindOrCreateConversation_ReopenToggleFailureStillReturnsLatest(t *testing.T) {
	// (e) When ToggleConversationStatus fails, the error is logged but NOT
	// propagated: the method still returns the latest conversation (no error),
	// so a transient toggle failure never blocks message delivery. Because the
	// toggle failed, the returned status keeps its original "resolved" value.
	origReopen := config.ChatwootReopenConversation
	config.ChatwootReopenConversation = true
	defer func() { config.ChatwootReopenConversation = origReopen }()

	server, counters := reopenServer(t, []map[string]any{
		{"id": 20, "inbox_id": 2, "status": "resolved"},
	}, false, 999)
	defer server.Close()

	c := newTestClient(t, server.URL)
	conv, err := c.FindOrCreateConversation(7, "")
	if err != nil {
		t.Fatalf("FindOrCreateConversation should swallow toggle error, got: %v", err)
	}
	if conv.ID != 20 {
		t.Fatalf("conv.ID = %d, want 20 (latest returned despite toggle failure)", conv.ID)
	}
	if conv.Status != "resolved" {
		t.Errorf("Status = %q, want resolved (unchanged because toggle failed)", conv.Status)
	}
	if counters.toggleCalls != 1 {
		t.Errorf("toggle calls = %d, want 1 (attempted once)", counters.toggleCalls)
	}
	if counters.createCalls != 0 {
		t.Errorf("create calls = %d, want 0 (latest still returned, not created)", counters.createCalls)
	}
}
