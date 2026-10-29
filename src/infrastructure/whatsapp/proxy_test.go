package whatsapp

import "testing"

func TestRedactProxyURLMasksUserinfo(t *testing.T) {
	for raw, want := range map[string]string{
		"ftp://ACCESS_TOKEN@proxy.internal:1080": "ftp://***@proxy.internal:1080",
		"socks5://user:pass@proxy.internal:1080": "socks5://***@proxy.internal:1080",
		"http://proxy.internal:8080":             "http://proxy.internal:8080",
	} {
		if got := redactProxyURL(raw); got != want {
			t.Fatalf("redactProxyURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
