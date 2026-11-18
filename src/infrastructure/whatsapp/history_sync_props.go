package whatsapp

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"google.golang.org/protobuf/proto"
)

// applyFullHistorySyncProps asks the phone for the FULL message history when a device pairs.
// No-op unless config.WhatsappFullHistorySync is enabled.
func applyFullHistorySyncProps(props *waCompanionReg.DeviceProps) {
	if !config.WhatsappFullHistorySync {
		return
	}
	props.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	props.RequireFullSync = proto.Bool(true)
	if props.HistorySyncConfig == nil {
		props.HistorySyncConfig = &waCompanionReg.DeviceProps_HistorySyncConfig{}
	}
	props.HistorySyncConfig.FullSyncDaysLimit = proto.Uint32(config.WhatsappFullHistoryDays)
	props.HistorySyncConfig.FullSyncSizeMbLimit = proto.Uint32(config.WhatsappFullHistorySizeMB)
}
