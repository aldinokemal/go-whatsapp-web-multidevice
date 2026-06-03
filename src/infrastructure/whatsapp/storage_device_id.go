package whatsapp

import (
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// normalizeStorageDeviceID converts a WhatsApp JID to its storage key form.
// For real JIDs this strips the device suffix (e.g. :11), while preserving
// arbitrary placeholder IDs that are not parseable as JIDs.
func normalizeStorageDeviceID(deviceID string) string {
	trimmed := strings.TrimSpace(deviceID)
	if trimmed == "" {
		return ""
	}

	jid, err := types.ParseJID(trimmed)
	if err != nil {
		return trimmed
	}

	return jid.ToNonAD().String()
}

// storageDeviceIDForInstance resolves the best storage key for a device instance.
// Prefer the current JID when available, because it is the stable identifier used
// by the rest of the storage layer. Fall back to the instance ID for placeholder
// devices that have not connected yet.
func storageDeviceIDForInstance(inst *DeviceInstance) string {
	if inst == nil {
		return ""
	}

	if jid := normalizeStorageDeviceID(inst.JID()); jid != "" {
		return jid
	}

	return normalizeStorageDeviceID(inst.ID())
}
