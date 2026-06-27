package call

import (
	"testing"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
)

func TestBuildRelayConfigs(t *testing.T) {
	endpoints := []core.RelayEndpoint{

		{IP: "1.1.1.1", Protocol: 0, Key: "k1", RawToken: []byte{1}, RelayName: "relay-a", RelayID: 7, AuthTokenID: "a1"},

		{IP: "1.1.1.1", Protocol: 0, Key: "k1b", RawToken: []byte{2}, RelayName: "relay-a-dup"},

		{IP: "2.2.2.2", Protocol: 1, Key: "k2", RawToken: []byte{3}},

		{IP: "3.3.3.3", Protocol: 0, Key: "", RawToken: []byte{4}},

		{IP: "4.4.4.4", Protocol: 0, Key: "k4", RawToken: nil},

		{IP: "5.5.5.5", Protocol: 0, Key: "k5", RawToken: []byte{5}, RelayName: ""},
	}

	got := buildRelayConfigs(endpoints)

	if len(got) != 2 {
		t.Fatalf("expected 2 usable configs, got %d: %+v", len(got), got)
	}
	if got[0].IP != "1.1.1.1" || got[0].Name != "relay-a" || got[0].Port != 3478 {
		t.Errorf("first config wrong: %+v", got[0])
	}
	if got[0].RelayID != 7 || got[0].AuthTokenID != "a1" || got[0].Key != "k1" {
		t.Errorf("first config fields not mapped: %+v", got[0])
	}
	if got[1].IP != "5.5.5.5" || got[1].Name != "5.5.5.5" {
		t.Errorf("expected name fallback to IP for second config: %+v", got[1])
	}
}

func TestBuildRelayConfigsEmpty(t *testing.T) {
	if got := buildRelayConfigs(nil); got != nil {
		t.Errorf("expected nil for no endpoints, got %+v", got)
	}
}
