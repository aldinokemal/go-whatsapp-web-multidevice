package chatwoot

import (
	"net"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// The per-device guard shares the media-fetch list, so ranges such as CGNAT
// (Alibaba's 100.100.100.200 metadata) are refused like RFC1918.
func TestValidateChatwootURLRejectsSharedNonPublicRanges(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })
	config.ChatwootAllowedHosts = nil

	for _, raw := range []string{
		"http://100.100.100.200",
		"http://100.64.0.1",
		"http://198.18.0.1",
		"http://192.0.0.1",
		"http://[64:ff9b::a00:1]",
	} {
		if err := ValidateChatwootURL(raw); err == nil {
			t.Errorf("ValidateChatwootURL(%q) = nil, want SSRF rejection", raw)
		}
	}
	if !isDisallowedSSRFIP(net.ParseIP("100.100.100.200")) {
		t.Error("isDisallowedSSRFIP(100.100.100.200) = false, want true")
	}
}
