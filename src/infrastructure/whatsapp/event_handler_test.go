package whatsapp

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"go.mau.fi/whatsmeow/types"
)

func TestResolvePresenceOnConnect(t *testing.T) {
	tests := []struct {
		name      string
		configVal string
		wantPres  types.Presence
		wantSkip  bool
	}{
		{
			name:      "unavailable returns PresenceUnavailable",
			configVal: "unavailable",
			wantPres:  types.PresenceUnavailable,
			wantSkip:  false,
		},
		{
			name:      "available returns PresenceAvailable",
			configVal: "available",
			wantPres:  types.PresenceAvailable,
			wantSkip:  false,
		},
		{
			name:      "none skips sending presence",
			configVal: "none",
			wantPres:  "",
			wantSkip:  true,
		},
		{
			name:      "empty string defaults to unavailable",
			configVal: "",
			wantPres:  types.PresenceUnavailable,
			wantSkip:  false,
		},
		{
			name:      "unknown value defaults to unavailable",
			configVal: "garbage",
			wantPres:  types.PresenceUnavailable,
			wantSkip:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origVal := config.WhatsappPresenceOnConnect
			config.WhatsappPresenceOnConnect = tt.configVal
			defer func() { config.WhatsappPresenceOnConnect = origVal }()

			pres, skip := resolvePresenceOnConnect()
			if skip != tt.wantSkip {
				t.Errorf("resolvePresenceOnConnect() skip = %v, want %v", skip, tt.wantSkip)
			}
			if !skip && pres != tt.wantPres {
				t.Errorf("resolvePresenceOnConnect() presence = %q, want %q", pres, tt.wantPres)
			}
		})
	}
}
