package whatsapp

import (
	"context"
	"errors"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func withDeviceStorageSettingsForTest(t *testing.T, fn func(deviceID string) (*chatstorage.DeviceStorageSettings, error)) {
	t.Helper()
	original := deviceStorageSettingsForTest
	deviceStorageSettingsForTest = fn
	t.Cleanup(func() { deviceStorageSettingsForTest = original })
}

func TestIsChatStorageEnabledForDeviceID_NoDeviceID(t *testing.T) {
	if !isChatStorageEnabledForDeviceID("") {
		t.Fatal("expected chat storage enabled by default when no device ID is known")
	}
}

func TestIsChatStorageEnabledForDeviceID_DeviceNotFound(t *testing.T) {
	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return nil, nil
	})

	if !isChatStorageEnabledForDeviceID("device-a") {
		t.Fatal("expected chat storage enabled when the device has no record")
	}
}

func TestIsChatStorageEnabledForDeviceID_NoOverrideDefaultsEnabled(t *testing.T) {
	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return &chatstorage.DeviceStorageSettings{}, nil
	})

	if !isChatStorageEnabledForDeviceID("device-a") {
		t.Fatal("expected chat storage enabled when no override is set")
	}
}

func TestIsChatStorageEnabledForDeviceID_ExplicitFalseOverride(t *testing.T) {
	disabled := false
	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return &chatstorage.DeviceStorageSettings{ChatStorage: &disabled}, nil
	})

	if isChatStorageEnabledForDeviceID("device-a") {
		t.Fatal("expected chat storage disabled by explicit override")
	}
}

func TestIsChatStorageEnabledForDeviceID_ExplicitTrueOverride(t *testing.T) {
	enabled := true
	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return &chatstorage.DeviceStorageSettings{ChatStorage: &enabled}, nil
	})

	if !isChatStorageEnabledForDeviceID("device-a") {
		t.Fatal("expected chat storage enabled by explicit override")
	}
}

func TestIsChatStorageEnabledForDeviceID_LookupErrorFailsClosed(t *testing.T) {
	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return nil, errors.New("boom")
	})

	if isChatStorageEnabledForDeviceID("device-a") {
		t.Fatal("expected chat storage disabled (fail closed) when the lookup errors")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceID_NoDeviceIDFollowsGlobal(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })

	config.WhatsappAutoDownloadMedia = false
	if isAutoDownloadMediaEnabledForDeviceID("") {
		t.Fatal("expected global false to apply when no device ID is known")
	}

	config.WhatsappAutoDownloadMedia = true
	if !isAutoDownloadMediaEnabledForDeviceID("") {
		t.Fatal("expected global true to apply when no device ID is known")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceID_NoOverrideFollowsGlobal(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })
	config.WhatsappAutoDownloadMedia = false

	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return &chatstorage.DeviceStorageSettings{}, nil
	})

	if isAutoDownloadMediaEnabledForDeviceID("device-a") {
		t.Fatal("expected global default (false) to apply when no override is set")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceID_OverrideWinsOverGlobal(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })
	config.WhatsappAutoDownloadMedia = false

	enabled := true
	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return &chatstorage.DeviceStorageSettings{AutoDownloadMedia: &enabled}, nil
	})

	if !isAutoDownloadMediaEnabledForDeviceID("device-a") {
		t.Fatal("expected the per-device override (true) to win over the global default (false)")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceID_LookupErrorFailsClosed(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })
	config.WhatsappAutoDownloadMedia = true

	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return nil, errors.New("boom")
	})

	if isAutoDownloadMediaEnabledForDeviceID("device-a") {
		t.Fatal("expected auto-download disabled (fail closed) when the lookup errors, even though the global default is true")
	}
}

// TestIsChatStorageEnabledForClient_SiblingSlotsShareNumberButHonorOwnOverride
// covers the maintainer's P1: two device slots that are sibling companion sessions
// for the same WhatsApp account (issue #760) share one bare-number JID but must
// each be resolved by their own registry ID, not collapsed onto one ambiguous
// record. Regression for #833.
func TestIsChatStorageEnabledForClient_SiblingSlotsShareNumberButHonorOwnOverride(t *testing.T) {
	enabledSlot := true
	disabledSlot := false
	settingsByDeviceID := map[string]*chatstorage.DeviceStorageSettings{
		"slot-a": {ChatStorage: &disabledSlot},
		"slot-b": {ChatStorage: &enabledSlot},
	}
	withDeviceStorageSettingsForTest(t, func(deviceID string) (*chatstorage.DeviceStorageSettings, error) {
		return settingsByDeviceID[deviceID], nil
	})

	ctxA := ContextWithDevice(context.Background(), NewDeviceInstance("slot-a", nil, nil))
	ctxB := ContextWithDevice(context.Background(), NewDeviceInstance("slot-b", nil, nil))

	if isChatStorageEnabledForClient(ctxA, nil) {
		t.Fatal("expected slot-a to honor its own chat_storage=false override")
	}
	if !isChatStorageEnabledForClient(ctxB, nil) {
		t.Fatal("expected slot-b to honor its own chat_storage=true override, unaffected by slot-a")
	}
}
