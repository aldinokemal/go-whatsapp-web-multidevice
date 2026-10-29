package whatsapp

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"
)

// applyFullHistorySyncProps asks the phone for the FULL message history when a device pairs.
// No-op unless config.WhatsappFullHistorySync is enabled.
func applyFullHistorySyncProps() {
	if !config.WhatsappFullHistorySync {
		return
	}
	desktop := waCompanionReg.DeviceProps_DESKTOP
	store.DeviceProps.PlatformType = &desktop
	store.DeviceProps.RequireFullSync = proto.Bool(true)
	if store.DeviceProps.HistorySyncConfig == nil {
		store.DeviceProps.HistorySyncConfig = &waCompanionReg.DeviceProps_HistorySyncConfig{}
	}
	store.DeviceProps.HistorySyncConfig.FullSyncDaysLimit = proto.Uint32(config.WhatsappFullHistoryDays)
	store.DeviceProps.HistorySyncConfig.FullSyncSizeMbLimit = proto.Uint32(config.WhatsappFullHistorySizeMB)
}
