package whatsapp

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

// deviceStorageRecordForTest is a seam for unit tests, mirroring webhookStorageForTest
// in webhook_forward.go: it lets tests stub the device record lookup without a real
// DeviceManager/storage.
var deviceStorageRecordForTest func(deviceJID string) (*domainChatStorage.DeviceRecord, error)

// getDeviceRecordForStorageSettings resolves the device record used to derive the
// per-device chat_storage / auto_download_media overrides, using the test override
// if set and otherwise the live DeviceManager storage (same resolution path as
// getWebhookConfigForDevice).
func getDeviceRecordForStorageSettings(deviceJID string) (*domainChatStorage.DeviceRecord, error) {
	if deviceStorageRecordForTest != nil {
		return deviceStorageRecordForTest(deviceJID)
	}
	dm := GetDeviceManager()
	if dm != nil && dm.storage != nil {
		return dm.storage.GetDeviceRecordByJID(deviceJID)
	}
	return nil, nil
}

// clientDeviceJID returns the own-account JID (bare number form) for a connected
// client, or "" when the client or its store is not yet available.
func clientDeviceJID(client *whatsmeow.Client) string {
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return ""
	}
	return client.Store.ID.ToNonAD().String()
}

// isChatStorageEnabledForClient reports whether incoming messages/reactions/history
// should be persisted to the local chat storage database for the device behind
// client. See isChatStorageEnabledForDeviceJID for the resolution rules.
func isChatStorageEnabledForClient(client *whatsmeow.Client) bool {
	return isChatStorageEnabledForDeviceJID(clientDeviceJID(client))
}

// isChatStorageEnabledForDeviceJID reports whether chat storage is enabled for the
// device identified by deviceJID. A per-device chat_storage=false override skips
// storage entirely for that device; no override (nil) keeps the instance-wide
// default of always storing, so existing single-tenant deployments are unaffected.
// Multi-tenant deployments running one instance for many customer devices use the
// override to keep alarm/service devices out of chat storage without touching
// every other device. Split out from the *whatsmeow.Client variant so it can be
// unit tested without a live client/store.
func isChatStorageEnabledForDeviceJID(deviceJID string) bool {
	if deviceJID == "" {
		return true
	}

	record, err := getDeviceRecordForStorageSettings(deviceJID)
	if err != nil {
		logrus.Warnf("Failed to resolve chat_storage override for device %s, defaulting to enabled: %v", deviceJID, err)
		return true
	}
	if record != nil && record.ChatStorage != nil {
		return *record.ChatStorage
	}
	return true
}

// isAutoDownloadMediaEnabledForClient reports whether incoming media should be
// auto-downloaded for the device behind client. See
// isAutoDownloadMediaEnabledForDeviceJID for the resolution rules.
func isAutoDownloadMediaEnabledForClient(client *whatsmeow.Client) bool {
	return isAutoDownloadMediaEnabledForDeviceJID(clientDeviceJID(client))
}

// isAutoDownloadMediaEnabledForDeviceJID reports whether auto-download is enabled
// for the device identified by deviceJID. No override (nil) falls back to the
// instance-wide --auto-download-media / WHATSAPP_AUTO_DOWNLOAD_MEDIA flag. Split
// out from the *whatsmeow.Client variant so it can be unit tested without a live
// client/store.
func isAutoDownloadMediaEnabledForDeviceJID(deviceJID string) bool {
	if deviceJID == "" {
		return config.WhatsappAutoDownloadMedia
	}

	record, err := getDeviceRecordForStorageSettings(deviceJID)
	if err != nil {
		logrus.Warnf("Failed to resolve auto_download_media override for device %s, defaulting to global: %v", deviceJID, err)
		return config.WhatsappAutoDownloadMedia
	}
	if record != nil && record.AutoDownloadMedia != nil {
		return *record.AutoDownloadMedia
	}
	return config.WhatsappAutoDownloadMedia
}
