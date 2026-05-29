package chatwoot

import (
	"testing"
)

func TestRegistryGetClientForDevice(t *testing.T) {
	repo := newTestConfigRepo(t)
	if err := repo.Save(sampleConfig()); err != nil {
		t.Fatalf("save: %v", err)
	}

	reg := NewClientRegistry(repo)
	client, err := reg.GetClientForDevice("628111@s.whatsapp.net")
	if err != nil {
		t.Fatalf("get client: %v", err)
	}
	if client.InboxID != 67 || client.AccountID != 2 || client.APIToken != "token-a" {
		t.Fatalf("unexpected client: %+v", client)
	}
	if client.BaseURL != "https://chatwoot.example.com" {
		t.Fatalf("unexpected base url: %s", client.BaseURL)
	}
}

func TestRegistryGetClientForDeviceNoConfig(t *testing.T) {
	// Empty repo and no env-var default configured -> ErrNoConfig.
	reg := NewClientRegistry(newTestConfigRepo(t))
	if _, err := reg.GetClientForDevice("unknown@s.whatsapp.net"); err != ErrNoConfig {
		t.Fatalf("expected ErrNoConfig, got %v", err)
	}
}

func TestRegistryGetClientForInbox(t *testing.T) {
	repo := newTestConfigRepo(t)
	if err := repo.Save(sampleConfig()); err != nil {
		t.Fatalf("save: %v", err)
	}

	reg := NewClientRegistry(repo)
	client, deviceID, err := reg.GetClientForInbox(2, 67)
	if err != nil {
		t.Fatalf("get client for inbox: %v", err)
	}
	if deviceID != "628111@s.whatsapp.net" || client.InboxID != 67 {
		t.Fatalf("unexpected resolution: device=%s inbox=%d", deviceID, client.InboxID)
	}

	if _, _, err := reg.GetClientForInbox(2, 999); err != ErrNoConfig {
		t.Fatalf("expected ErrNoConfig for unknown inbox, got %v", err)
	}
}

func TestRegistryRefresh(t *testing.T) {
	repo := newTestConfigRepo(t)
	reg := NewClientRegistry(repo) // empty cache

	if err := repo.Save(sampleConfig()); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Not in cache yet, but repo lookup path resolves it.
	if _, err := reg.GetClientForDevice("628111@s.whatsapp.net"); err != nil {
		t.Fatalf("repo-path lookup: %v", err)
	}

	// After disabling and refreshing, the cached client is dropped.
	disabled := sampleConfig()
	disabled.Enabled = false
	if err := repo.Save(disabled); err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	if err := reg.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// An explicitly disabled device returns (nil, nil): it must NOT fall back to
	// the env-var default, so callers can tell "disabled" apart from "absent".
	client, err := reg.GetClientForDevice("628111@s.whatsapp.net")
	if err != nil {
		t.Fatalf("expected nil error for disabled device, got %v", err)
	}
	if client != nil {
		t.Fatalf("expected nil client for disabled device, got %+v", client)
	}
}
