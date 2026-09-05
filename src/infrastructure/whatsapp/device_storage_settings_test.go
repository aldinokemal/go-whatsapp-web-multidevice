package whatsapp

import (
	"errors"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func withDeviceStorageRecordForTest(t *testing.T, fn func(deviceJID string) (*chatstorage.DeviceRecord, error)) {
	t.Helper()
	original := deviceStorageRecordForTest
	deviceStorageRecordForTest = fn
	t.Cleanup(func() { deviceStorageRecordForTest = original })
}

func TestIsChatStorageEnabledForDeviceJID_NoDeviceID(t *testing.T) {
	if !isChatStorageEnabledForDeviceJID("") {
		t.Fatal("expected chat storage enabled by default when no device JID is known")
	}
}

func TestIsChatStorageEnabledForDeviceJID_DeviceNotFound(t *testing.T) {
	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return nil, nil
	})

	if !isChatStorageEnabledForDeviceJID("unknown@s.whatsapp.net") {
		t.Fatal("expected chat storage enabled when the device has no record")
	}
}

func TestIsChatStorageEnabledForDeviceJID_NoOverrideDefaultsEnabled(t *testing.T) {
	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{DeviceID: deviceJID}, nil
	})

	if !isChatStorageEnabledForDeviceJID("6289600000000@s.whatsapp.net") {
		t.Fatal("expected chat storage enabled when no override is set")
	}
}

func TestIsChatStorageEnabledForDeviceJID_ExplicitFalseOverride(t *testing.T) {
	disabled := false
	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{DeviceID: deviceJID, ChatStorage: &disabled}, nil
	})

	if isChatStorageEnabledForDeviceJID("6289600000000@s.whatsapp.net") {
		t.Fatal("expected chat storage disabled by explicit override")
	}
}

func TestIsChatStorageEnabledForDeviceJID_ExplicitTrueOverride(t *testing.T) {
	enabled := true
	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{DeviceID: deviceJID, ChatStorage: &enabled}, nil
	})

	if !isChatStorageEnabledForDeviceJID("6289600000000@s.whatsapp.net") {
		t.Fatal("expected chat storage enabled by explicit override")
	}
}

func TestIsChatStorageEnabledForDeviceJID_LookupErrorDefaultsEnabled(t *testing.T) {
	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return nil, errors.New("boom")
	})

	if !isChatStorageEnabledForDeviceJID("6289600000000@s.whatsapp.net") {
		t.Fatal("expected chat storage enabled (fail open) when the lookup errors")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceJID_NoDeviceIDFollowsGlobal(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })

	config.WhatsappAutoDownloadMedia = false
	if isAutoDownloadMediaEnabledForDeviceJID("") {
		t.Fatal("expected global false to apply when no device JID is known")
	}

	config.WhatsappAutoDownloadMedia = true
	if !isAutoDownloadMediaEnabledForDeviceJID("") {
		t.Fatal("expected global true to apply when no device JID is known")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceJID_NoOverrideFollowsGlobal(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })
	config.WhatsappAutoDownloadMedia = false

	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{DeviceID: deviceJID}, nil
	})

	if isAutoDownloadMediaEnabledForDeviceJID("6289600000000@s.whatsapp.net") {
		t.Fatal("expected global default (false) to apply when no override is set")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceJID_OverrideWinsOverGlobal(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })
	config.WhatsappAutoDownloadMedia = false

	enabled := true
	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return &chatstorage.DeviceRecord{DeviceID: deviceJID, AutoDownloadMedia: &enabled}, nil
	})

	if !isAutoDownloadMediaEnabledForDeviceJID("6289600000000@s.whatsapp.net") {
		t.Fatal("expected the per-device override (true) to win over the global default (false)")
	}
}

func TestIsAutoDownloadMediaEnabledForDeviceJID_LookupErrorFollowsGlobal(t *testing.T) {
	original := config.WhatsappAutoDownloadMedia
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = original })
	config.WhatsappAutoDownloadMedia = true

	withDeviceStorageRecordForTest(t, func(deviceJID string) (*chatstorage.DeviceRecord, error) {
		return nil, errors.New("boom")
	})

	if !isAutoDownloadMediaEnabledForDeviceJID("6289600000000@s.whatsapp.net") {
		t.Fatal("expected global default to apply when the lookup errors")
	}
}

func TestClientDeviceJID_NilClient(t *testing.T) {
	if got := clientDeviceJID(nil); got != "" {
		t.Fatalf("expected empty device JID for nil client, got %q", got)
	}
}
