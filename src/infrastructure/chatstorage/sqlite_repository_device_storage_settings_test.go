package chatstorage

import (
	"database/sql"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func boolPtr(v bool) *bool { return &v }

// A device with no chat_storage/auto_download_media override must report both
// fields as nil so callers know to fall back to the instance-wide default.
func TestGetDeviceStorageSettings_NoOverride(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "dev1", JID: "628177700001@s.whatsapp.net"}); err != nil {
		t.Fatalf("save device record: %v", err)
	}

	settings, err := repo.GetDeviceStorageSettings("dev1")
	if err != nil {
		t.Fatalf("get device storage settings: %v", err)
	}
	if settings == nil {
		t.Fatal("expected non-nil settings for an existing device")
	}
	if settings.ChatStorage != nil {
		t.Fatalf("expected nil ChatStorage, got %v", *settings.ChatStorage)
	}
	if settings.AutoDownloadMedia != nil {
		t.Fatalf("expected nil AutoDownloadMedia, got %v", *settings.AutoDownloadMedia)
	}
}

// GetDeviceStorageSettings on a device id that was never registered returns (nil, nil),
// mirroring GetDeviceWebhookConfig's contract.
func TestGetDeviceStorageSettings_UnknownDevice(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	settings, err := repo.GetDeviceStorageSettings("unknown")
	if err != nil {
		t.Fatalf("get device storage settings: %v", err)
	}
	if settings != nil {
		t.Fatalf("expected nil settings for unknown device, got %+v", settings)
	}
}

// SetDeviceStorageSettings only touches the columns flagged as present in the patch:
// setting chat_storage must leave a previously set auto_download_media untouched.
func TestSetDeviceStorageSettings_PartialUpdateLeavesOtherFieldUntouched(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "dev1", JID: "628177700001@s.whatsapp.net"}); err != nil {
		t.Fatalf("save device record: %v", err)
	}

	if err := repo.SetDeviceStorageSettings("dev1", domainChatStorage.DeviceStoragePatch{
		HasAutoDownloadMedia: true,
		AutoDownloadMedia:    boolPtr(false),
	}); err != nil {
		t.Fatalf("set auto_download_media: %v", err)
	}

	if err := repo.SetDeviceStorageSettings("dev1", domainChatStorage.DeviceStoragePatch{
		HasChatStorage: true,
		ChatStorage:    boolPtr(false),
	}); err != nil {
		t.Fatalf("set chat_storage: %v", err)
	}

	settings, err := repo.GetDeviceStorageSettings("dev1")
	if err != nil {
		t.Fatalf("get device storage settings: %v", err)
	}
	if settings == nil || settings.ChatStorage == nil || *settings.ChatStorage != false {
		t.Fatalf("expected chat_storage=false, got %+v", settings)
	}
	if settings.AutoDownloadMedia == nil || *settings.AutoDownloadMedia != false {
		t.Fatalf("expected auto_download_media to still be false from the earlier call, got %+v", settings)
	}
}

// A patch field present with a nil value clears the override back to "follow the
// instance default" (NULL in the database), not to false.
func TestSetDeviceStorageSettings_NilValueClearsOverride(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "dev1", JID: "628177700001@s.whatsapp.net"}); err != nil {
		t.Fatalf("save device record: %v", err)
	}

	if err := repo.SetDeviceStorageSettings("dev1", domainChatStorage.DeviceStoragePatch{
		HasChatStorage: true,
		ChatStorage:    boolPtr(false),
	}); err != nil {
		t.Fatalf("set chat_storage: %v", err)
	}

	if err := repo.SetDeviceStorageSettings("dev1", domainChatStorage.DeviceStoragePatch{
		HasChatStorage: true,
		ChatStorage:    nil,
	}); err != nil {
		t.Fatalf("clear chat_storage: %v", err)
	}

	settings, err := repo.GetDeviceStorageSettings("dev1")
	if err != nil {
		t.Fatalf("get device storage settings: %v", err)
	}
	if settings == nil || settings.ChatStorage != nil {
		t.Fatalf("expected chat_storage override cleared (nil), got %+v", settings)
	}
}

// SetDeviceStorageSettings on a device id that does not exist reports sql.ErrNoRows,
// mirroring SetDeviceWebhookConfig/SetDeviceWebhookURL.
func TestSetDeviceStorageSettings_UnknownDevice(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	err := repo.SetDeviceStorageSettings("unknown", domainChatStorage.DeviceStoragePatch{
		HasChatStorage: true,
		ChatStorage:    boolPtr(true),
	})
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

// A patch with neither field flagged as present is a no-op, not an error.
func TestSetDeviceStorageSettings_EmptyPatchIsNoop(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "dev1", JID: "628177700001@s.whatsapp.net"}); err != nil {
		t.Fatalf("save device record: %v", err)
	}

	if err := repo.SetDeviceStorageSettings("dev1", domainChatStorage.DeviceStoragePatch{}); err != nil {
		t.Fatalf("expected no error for empty patch, got %v", err)
	}
}

// GetDeviceRecordByJID (used on the message path to resolve per-device overrides)
// must surface chat_storage/auto_download_media alongside the existing webhook fields.
func TestGetDeviceRecordByJID_IncludesStorageSettings(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	jid := "628177700001@s.whatsapp.net"
	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "dev1", JID: jid}); err != nil {
		t.Fatalf("save device record: %v", err)
	}
	if err := repo.SetDeviceStorageSettings("dev1", domainChatStorage.DeviceStoragePatch{
		HasChatStorage:       true,
		ChatStorage:          boolPtr(false),
		HasAutoDownloadMedia: true,
		AutoDownloadMedia:    boolPtr(true),
	}); err != nil {
		t.Fatalf("set storage settings: %v", err)
	}

	record, err := repo.GetDeviceRecordByJID(jid)
	if err != nil {
		t.Fatalf("get device record by jid: %v", err)
	}
	if record == nil {
		t.Fatal("expected a device record")
	}
	if record.ChatStorage == nil || *record.ChatStorage != false {
		t.Fatalf("expected ChatStorage=false, got %+v", record.ChatStorage)
	}
	if record.AutoDownloadMedia == nil || *record.AutoDownloadMedia != true {
		t.Fatalf("expected AutoDownloadMedia=true, got %+v", record.AutoDownloadMedia)
	}
}
