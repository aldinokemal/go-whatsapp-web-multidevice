package call

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
	"testing"
)

func TestBuildRelayConfigs(t *testing.T) {
	endpoints := []core.RelayEndpoint{
		{IP: "1.1.1.1", Port: 3480, Protocol: 0, Key: "k1", RawToken: []byte{1}, RelayName: "relay-a", RelayID: 7, AuthTokenID: "a1"},
		{IP: "1.1.1.1", Port: 3480, Protocol: 0, Key: "k1b", RawToken: []byte{2}, RelayName: "relay-a-dup"},
		{IP: "2.2.2.2", Port: 3480, Protocol: 1, Key: "k2", RawToken: []byte{3}},
		{IP: "3.3.3.3", Port: 3480, Protocol: 0, Key: "", RawToken: []byte{4}},
		{IP: "4.4.4.4", Port: 3490, Protocol: 0, Key: "k4", RawToken: nil},
		{IP: "5.5.5.5", Port: 3500, Protocol: 0, Key: "k5", RawToken: []byte{5}, RelayName: ""},
	}

	got := buildRelayConfigs(endpoints)

	if len(got) != 3 {
		t.Fatalf("expected 3 usable configs, got %d: %+v", len(got), got)
	}
	if got[0].IP != "1.1.1.1" || got[0].Name != "relay-a" || got[0].Port != 3480 {
		t.Errorf("first config wrong: %+v", got[0])
	}
	if got[0].RelayID != 7 || got[0].AuthTokenID != "a1" || got[0].Key != "k1" {
		t.Errorf("first config fields not mapped: %+v", got[0])
	}
	if got[1].IP != "4.4.4.4" || got[1].Name != "4.4.4.4" || got[1].Port != 3490 {
		t.Errorf("second config should keep advertised port and fallback name: %+v", got[1])
	}
	if got[2].IP != "5.5.5.5" || got[2].Name != "5.5.5.5" || got[2].Port != 3500 {
		t.Errorf("expected name fallback to IP for third config: %+v", got[2])
	}
}

func TestBuildRelayConfigsEmpty(t *testing.T) {
	if got := buildRelayConfigs(nil); got != nil {
		t.Errorf("expected nil for no endpoints, got %+v", got)
	}
}
