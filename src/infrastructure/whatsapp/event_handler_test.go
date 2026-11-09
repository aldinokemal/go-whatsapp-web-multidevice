package whatsapp

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
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

// A StreamReplaced event for one device used to call os.Exit(0) and take every
// device in the process down with it (#883). It must only disconnect that device.
func TestHandlerStreamReplacedOnlyDisconnectsThatDevice(t *testing.T) {
	ctx := context.Background()
	replaced := NewDeviceInstance("test-stream-replaced", nil, nil)
	replaced.SetState(domainDevice.DeviceStateLoggedIn)
	other := NewDeviceInstance("test-stream-other", nil, nil)
	other.SetState(domainDevice.DeviceStateLoggedIn)

	go handler(ctx, replaced, &events.StreamReplaced{})
	msg := recvBroadcast(t)
	if msg.Code != "DEVICE_STREAM_REPLACED" {
		t.Fatalf("broadcast code = %s, want DEVICE_STREAM_REPLACED", msg.Code)
	}
	if result, ok := msg.Result.(map[string]string); !ok || result["device_id"] != "test-stream-replaced" {
		t.Errorf("broadcast result = %#v, want device_id test-stream-replaced", msg.Result)
	}

	if got := replaced.State(); got != domainDevice.DeviceStateDisconnected {
		t.Errorf("replaced device state = %s, want %s", got, domainDevice.DeviceStateDisconnected)
	}
	if got := other.State(); got != domainDevice.DeviceStateLoggedIn {
		t.Errorf("other device state = %s, want %s", got, domainDevice.DeviceStateLoggedIn)
	}
}
