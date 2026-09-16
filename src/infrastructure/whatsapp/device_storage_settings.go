package whatsapp

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

// deviceStorageSettingsForTest is a seam for unit tests, mirroring webhookStorageForTest
// in webhook_forward.go: it lets tests stub the device-ID-keyed settings lookup without
// a real DeviceManager/storage.
var deviceStorageSettingsForTest func(deviceID string) (*domainChatStorage.DeviceStorageSettings, error)

// getDeviceStorageSettingsForID resolves the chat_storage / auto_download_media
// overrides for the device registered under deviceID, using the test override if set
// and otherwise the live DeviceManager storage.
func getDeviceStorageSettingsForID(deviceID string) (*domainChatStorage.DeviceStorageSettings, error) {
	if deviceStorageSettingsForTest != nil {
		return deviceStorageSettingsForTest(deviceID)
	}
	dm := GetDeviceManager()
	if dm != nil && dm.storage != nil {
		return dm.storage.GetDeviceStorageSettings(deviceID)
	}
	return nil, nil
}

// isChatStorageEnabledForClient reports whether incoming messages/reactions/history
// should be persisted to the local chat storage database for the device carried in
// ctx. See isChatStorageEnabledForDeviceID for the resolution rules. Falls back to
// enabled when ctx carries no device (e.g. a call path outside the event handler),
// matching the pre-existing instance-wide default.
func isChatStorageEnabledForClient(ctx context.Context, client *whatsmeow.Client) bool {
	instance, ok := DeviceFromContext(ctx)
	if !ok || instance == nil {
		return true
	}
	return isChatStorageEnabledForDeviceID(instance.ID())
}

// isChatStorageEnabledForDeviceID reports whether chat storage is enabled for the
// device registered under deviceID. Resolving by the device's registry ID (rather
// than its bare-number JID) always identifies the exact slot, even when sibling
// companion sessions share the same account number (issue #760). A per-device
// chat_storage=false override skips storage entirely for that device; no override
// (nil) keeps the instance-wide default of always storing, so existing deployments
// are unaffected. A lookup error fails closed (disabled): a transient storage
// failure cannot distinguish "no override" from a persisted false, so defaulting to
// enabled on an indeterminate read would silently defeat an explicit opt-out. Split
// out from the *whatsmeow.Client variant so it can be unit tested without a live
// client/store.
func isChatStorageEnabledForDeviceID(deviceID string) bool {
	if deviceID == "" {
		return true
	}

	settings, err := getDeviceStorageSettingsForID(deviceID)
	if err != nil {
		logrus.Warnf("Failed to resolve chat_storage override for device %s, failing closed (disabled): %v", deviceID, err)
		return false
	}
	if settings != nil && settings.ChatStorage != nil {
		return *settings.ChatStorage
	}
	return true
}

// isAutoDownloadMediaEnabledForClient reports whether incoming media should be
// auto-downloaded for the device carried in ctx. See
// isAutoDownloadMediaEnabledForDeviceID for the resolution rules. Falls back to the
// instance-wide flag when ctx carries no device.
func isAutoDownloadMediaEnabledForClient(ctx context.Context, client *whatsmeow.Client) bool {
	instance, ok := DeviceFromContext(ctx)
	if !ok || instance == nil {
		return config.WhatsappAutoDownloadMedia
	}
	return isAutoDownloadMediaEnabledForDeviceID(instance.ID())
}

// isAutoDownloadMediaEnabledForDeviceID reports whether auto-download is enabled
// for the device registered under deviceID. No override (nil) falls back to the
// instance-wide --auto-download-media / WHATSAPP_AUTO_DOWNLOAD_MEDIA flag. A lookup
// error fails closed (disabled), for the same reason as
// isChatStorageEnabledForDeviceID: falling back to a global flag that may be true
// would override a persisted false during a transient storage failure. Split out
// from the *whatsmeow.Client variant so it can be unit tested without a live
// client/store.
func isAutoDownloadMediaEnabledForDeviceID(deviceID string) bool {
	if deviceID == "" {
		return config.WhatsappAutoDownloadMedia
	}

	settings, err := getDeviceStorageSettingsForID(deviceID)
	if err != nil {
		logrus.Warnf("Failed to resolve auto_download_media override for device %s, failing closed (disabled): %v", deviceID, err)
		return false
	}
	if settings != nil && settings.AutoDownloadMedia != nil {
		return *settings.AutoDownloadMedia
	}
	return config.WhatsappAutoDownloadMedia
}
