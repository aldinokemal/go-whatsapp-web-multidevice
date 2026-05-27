package whatsapp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

func TestChatwootStatusFromReceipt(t *testing.T) {
	cases := map[string]string{
		"delivered":   "delivered",
		"read":        "read",
		"read-self":   "read",
		"played":      "read",
		"played-self": "read",
		"sender":      "",
		"retry":       "",
		"":            "",
	}
	for in, want := range cases {
		if got := chatwootStatusFromReceipt(in); got != want {
			t.Errorf("chatwootStatusFromReceipt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReceiptMessageIDs(t *testing.T) {
	if got := receiptMessageIDs(map[string]any{"ids": []string{"a", "b"}}); len(got) != 2 || got[0] != "a" {
		t.Errorf("[]string case: %v", got)
	}
	if got := receiptMessageIDs(map[string]any{"ids": []any{"a", 5, "b"}}); len(got) != 2 {
		t.Errorf("[]any case (ints skipped): %v", got)
	}
	if got := receiptMessageIDs(map[string]any{}); got != nil {
		t.Errorf("missing ids: %v", got)
	}
}

func TestHandleChatwootMessageAck(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cw := &chatwoot.Client{BaseURL: srv.URL, APIToken: "t", AccountID: 2, HTTPClient: srv.Client()}
	chatwoot.TrackOutgoingMessage("WAID-ack-1", 10, 99)

	handleChatwootMessageAck(cw, map[string]any{"ids": []string{"WAID-ack-1"}, "receipt_type": "read"})

	if gotPath != "/api/v1/accounts/2/conversations/10/messages/99" {
		t.Errorf("path = %s", gotPath)
	}
	if !strings.Contains(gotBody, `"status":"read"`) {
		t.Errorf("body = %s", gotBody)
	}
}

func TestHandleChatwootMessageAckUntracked(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cw := &chatwoot.Client{BaseURL: srv.URL, APIToken: "t", AccountID: 2, HTTPClient: srv.Client()}
	// Unknown WA id and an ignored receipt type must not call Chatwoot.
	handleChatwootMessageAck(cw, map[string]any{"ids": []string{"WAID-not-tracked"}, "receipt_type": "delivered"})
	handleChatwootMessageAck(cw, map[string]any{"ids": []string{"WAID-ack-1"}, "receipt_type": "sender"})
	if called {
		t.Fatal("Chatwoot should not be called for untracked id or ignored receipt type")
	}
}
