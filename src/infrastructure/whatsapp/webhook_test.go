package whatsapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
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
