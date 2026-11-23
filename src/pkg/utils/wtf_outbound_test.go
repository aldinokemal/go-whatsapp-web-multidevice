package utils

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// cannedTransport answers every request itself, so a request that reaches it
// proves the hook's client was used: the hosts below do not exist.
type cannedTransport struct {
	mu    sync.Mutex
	urls  []string
	ctype string
	body  []byte
}

func (c *cannedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.urls = append(c.urls, req.URL.String())
	c.mu.Unlock()
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        http.Header{"Content-Type": []string{c.ctype}},
		Body:          io.NopCloser(bytes.NewReader(c.body)),
		ContentLength: int64(len(c.body)),
		Request:       req,
	}, nil
}

func installOutbound(t *testing.T, ct *cannedTransport) *[]time.Duration {
	t.Helper()
	saved := OutboundClient
	t.Cleanup(func() { OutboundClient = saved })
	var timeouts []time.Duration
	OutboundClient = func(timeout time.Duration) *http.Client {
		timeouts = append(timeouts, timeout)
		return &http.Client{Timeout: timeout, Transport: ct}
	}
	return &timeouts
}

// Security: every user-steerable fetch in this package goes through the
// OutboundClient hook when it is set (multi-user mode installs netguard).
func TestDownloadFromURLUsesTheGuardWhenSet(t *testing.T) {
	cases := []struct {
		name  string
		ctype string
		url   string
		fetch func(string) error
	}{
		{"image", "image/png", "http://guard.invalid/a.png", func(u string) error { _, _, err := DownloadImageFromURL(u); return err }},
		{"audio", "audio/ogg", "http://guard.invalid/a.ogg", func(u string) error { _, _, err := DownloadAudioFromURL(u); return err }},
		{"video", "video/mp4", "http://guard.invalid/a.mp4", func(u string) error { _, _, err := DownloadVideoFromURL(u); return err }},
		{"file", "application/pdf", "http://guard.invalid/a.pdf", func(u string) error { _, _, err := DownloadFileFromURL(u); return err }},
		{"link preview", "text/html", "http://guard.invalid/page", func(u string) error { _, err := GetMetaDataFromURL(u); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ct := &cannedTransport{ctype: tc.ctype, body: []byte("<html><title>x</title></html>")}
			timeouts := installOutbound(t, ct)
			if err := tc.fetch(tc.url); err != nil && strings.Contains(err.Error(), "guard.invalid") {
				t.Fatalf("fetch bypassed the hook: %v", err)
			}
			if len(ct.urls) == 0 || ct.urls[0] != tc.url {
				t.Fatalf("hook client saw %v, want %s", ct.urls, tc.url)
			}
			if len(*timeouts) == 0 || (*timeouts)[0] <= 0 {
				t.Fatalf("hook not given a timeout: %v", *timeouts)
			}
		})
	}
}
