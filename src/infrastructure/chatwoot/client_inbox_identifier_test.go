package chatwoot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Read receipts are forwarded from concurrent goroutines, and each one calls
// UpdateLastSeen on the shared client. The first calls race to resolve the
// inbox identifier: run with -race, this test fails if that resolution is not
// synchronised, and without -race it still requires a single inbox lookup.
func TestUpdateLastSeen_ConcurrentCallsResolveInboxIdentifierOnce(t *testing.T) {
	var listCalls, updateCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/accounts/1/inboxes":
			listCalls.Add(1)
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{{ID: 2, Name: "WhatsApp", InboxIdentifier: "api-inbox-token"}},
			})
		case strings.HasPrefix(r.URL.Path, "/public/api/v1/inboxes/api-inbox-token/"):
			updateCalls.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)

	const callers = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- c.UpdateLastSeen(5, "628123456789@s.whatsapp.net")
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("UpdateLastSeen: %v", err)
		}
	}

	if got := listCalls.Load(); got != 1 {
		t.Errorf("inbox list lookups = %d, want exactly 1", got)
	}
	if got := updateCalls.Load(); got != callers {
		t.Errorf("update_last_seen calls = %d, want %d", got, callers)
	}
}

// A failed lookup must not be remembered: the next receipt tries again.
func TestUpdateLastSeen_FailedInboxLookupIsRetried(t *testing.T) {
	var listCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/accounts/1/inboxes" {
			if listCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			writeJSON(t, w, http.StatusOK, map[string]any{
				"payload": []Inbox{{ID: 2, Name: "WhatsApp", InboxIdentifier: "api-inbox-token"}},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(t, server.URL)
	if err := c.UpdateLastSeen(5, "628123456789@s.whatsapp.net"); err == nil {
		t.Fatal("first UpdateLastSeen succeeded although the inbox lookup failed")
	}
	if err := c.UpdateLastSeen(5, "628123456789@s.whatsapp.net"); err != nil {
		t.Fatalf("second UpdateLastSeen: %v", err)
	}
	if err := c.UpdateLastSeen(5, "628123456789@s.whatsapp.net"); err != nil {
		t.Fatalf("third UpdateLastSeen: %v", err)
	}
	if got := listCalls.Load(); got != 2 {
		t.Errorf("inbox list lookups = %d, want 2 (one failure, one success, then cached)", got)
	}
}
