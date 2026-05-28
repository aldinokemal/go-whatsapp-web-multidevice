package webhook

import "testing"

func TestRegistryGetWebhooksForDevice(t *testing.T) {
	repo := newTestWebhookRepo(t)

	enabled := sampleWebhookConfig()
	if err := repo.Save(enabled); err != nil {
		t.Fatalf("save enabled: %v", err)
	}
	disabled := sampleWebhookConfig()
	disabled.Enabled = false
	if err := repo.Save(disabled); err != nil {
		t.Fatalf("save disabled: %v", err)
	}

	reg := NewWebhookRegistry(repo)
	got := reg.GetWebhooksForDevice(enabled.DeviceID)
	if len(got) != 1 {
		t.Fatalf("expected only the enabled config, got %d", len(got))
	}
	if got[0].WebhookURL != enabled.WebhookURL {
		t.Fatalf("unexpected config: %+v", got[0])
	}
}

func TestRegistryUnknownDeviceReturnsEmpty(t *testing.T) {
	repo := newTestWebhookRepo(t)
	reg := NewWebhookRegistry(repo)
	if got := reg.GetWebhooksForDevice("nobody@s.whatsapp.net"); len(got) != 0 {
		t.Fatalf("expected empty slice, got %d", len(got))
	}
}

func TestRegistryRefreshPicksUpChanges(t *testing.T) {
	repo := newTestWebhookRepo(t)
	reg := NewWebhookRegistry(repo)

	cfg := sampleWebhookConfig()
	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Not visible until refreshed.
	if got := reg.GetWebhooksForDevice(cfg.DeviceID); len(got) != 0 {
		t.Fatalf("expected stale-empty before refresh, got %d", len(got))
	}
	if err := reg.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := reg.GetWebhooksForDevice(cfg.DeviceID); len(got) != 1 {
		t.Fatalf("expected 1 config after refresh, got %d", len(got))
	}
}

func TestGlobalRegistrySetGet(t *testing.T) {
	prev := GetGlobalRegistry()
	t.Cleanup(func() { SetGlobalRegistry(prev) })

	reg := NewWebhookRegistry(nil)
	SetGlobalRegistry(reg)
	if GetGlobalRegistry() != reg {
		t.Fatal("expected GetGlobalRegistry to return the registry set")
	}
}
