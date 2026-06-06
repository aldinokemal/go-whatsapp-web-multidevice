package chatwoot

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

func TestCanonicalizeChatwootURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"https plain", "https://app.chatwoot.com", "https://app.chatwoot.com", false},
		{"trailing slash", "https://app.chatwoot.com/", "https://app.chatwoot.com", false},
		{"uppercase scheme+host", "HTTPS://APP.Chatwoot.COM", "https://app.chatwoot.com", false},
		{"strip default https port", "https://app.chatwoot.com:443", "https://app.chatwoot.com", false},
		{"strip default http port", "http://example.com:80/", "http://example.com", false},
		{"keep nonstandard port", "https://host.example.com:8443", "https://host.example.com:8443", false},
		{"keep subpath, trim slash", "https://h.example.com/chatwoot/", "https://h.example.com/chatwoot", false},
		{"surrounding spaces", "  https://app.chatwoot.com  ", "https://app.chatwoot.com", false},
		{"reject ftp", "ftp://example.com", "", true},
		{"reject empty", "", "", true},
		{"reject no host", "https://", "", true},
		{"reject userinfo", "https://user:pass@example.com", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeChatwootURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("canonicalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateChatwootURL_SSRF(t *testing.T) {
	reject := []string{
		"http://127.0.0.1",
		"http://localhost",
		"http://10.0.0.5",
		"http://192.168.1.1:3000",
		"http://172.16.5.5",
		"http://169.254.169.254", // cloud metadata
		"http://[::1]",
		"http://0.0.0.0",
		"ftp://example.com",
		"https://user:pass@example.com",
	}
	for _, u := range reject {
		if err := ValidateChatwootURL(u); err == nil {
			t.Errorf("ValidateChatwootURL(%q) = nil, want rejection", u)
		}
	}

	accept := []string{
		"https://app.chatwoot.com",
		"https://chat.example.com:8443",
		"http://203.0.113.10", // public IP literal
	}
	for _, u := range accept {
		if err := ValidateChatwootURL(u); err != nil {
			t.Errorf("ValidateChatwootURL(%q) = %v, want accept", u, err)
		}
	}
}

func TestValidateChatwootURL_Allowlist(t *testing.T) {
	orig := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = orig })

	config.ChatwootAllowedHosts = []string{"app.chatwoot.com"}
	if err := ValidateChatwootURL("https://app.chatwoot.com"); err != nil {
		t.Errorf("allowlisted host rejected: %v", err)
	}
	if err := ValidateChatwootURL("https://evil.example.com"); err == nil {
		t.Error("non-allowlisted host accepted, want rejection")
	}
}
