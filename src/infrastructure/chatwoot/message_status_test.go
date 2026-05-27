package chatwoot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrackAndResolveMessage(t *testing.T) {
	TrackOutgoingMessage("WAID-track-1", 10, 99)

	conv, msg, ok := ResolveTrackedMessage("WAID-track-1")
	if !ok || conv != 10 || msg != 99 {
		t.Fatalf("resolve: conv=%d msg=%d ok=%v", conv, msg, ok)
	}

	if _, _, ok := ResolveTrackedMessage("WAID-unknown"); ok {
		t.Fatal("expected unknown id to not resolve")
	}

	// Zero conversation/message or empty id must not be tracked.
	TrackOutgoingMessage("WAID-zero", 0, 5)
	if _, _, ok := ResolveTrackedMessage("WAID-zero"); ok {
		t.Fatal("expected zero conversationID not to be tracked")
	}
}

func TestUpdateMessageStatus(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIToken: "tok", AccountID: 2, HTTPClient: srv.Client()}
	if err := c.UpdateMessageStatus(10, 99, "delivered"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/accounts/2/conversations/10/messages/99" {
		t.Errorf("path = %s", gotPath)
	}
	if !strings.Contains(gotBody, `"status":"delivered"`) {
		t.Errorf("body = %s", gotBody)
	}
}

func TestUpdateMessageStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIToken: "tok", AccountID: 2, HTTPClient: srv.Client()}
	if err := c.UpdateMessageStatus(1, 2, "read"); err == nil {
		t.Fatal("expected error on 404 response")
	}
}
