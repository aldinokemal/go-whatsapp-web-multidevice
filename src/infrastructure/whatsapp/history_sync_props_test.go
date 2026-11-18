package whatsapp

import (
	"sync"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"google.golang.org/protobuf/proto"
)

func TestApplyFullHistorySyncProps(t *testing.T) {
	enabled, days, size := config.WhatsappFullHistorySync, config.WhatsappFullHistoryDays, config.WhatsappFullHistorySizeMB
	t.Cleanup(func() {
		config.WhatsappFullHistorySync, config.WhatsappFullHistoryDays, config.WhatsappFullHistorySizeMB = enabled, days, size
	})
	chrome := waCompanionReg.DeviceProps_CHROME
	newProps := func() *waCompanionReg.DeviceProps {
		return &waCompanionReg.DeviceProps{
			PlatformType:      chrome.Enum(),
			HistorySyncConfig: &waCompanionReg.DeviceProps_HistorySyncConfig{StorageQuotaMb: proto.Uint32(10240)},
		}
	}

	config.WhatsappFullHistorySync = false
	props := newProps()
	applyFullHistorySyncProps(props)
	assert.Equal(t, chrome, props.GetPlatformType(), "disabled: the configured platform stays")
	assert.False(t, props.GetRequireFullSync())
	assert.Nil(t, props.GetHistorySyncConfig().FullSyncDaysLimit)

	config.WhatsappFullHistorySync, config.WhatsappFullHistoryDays, config.WhatsappFullHistorySizeMB = true, 30, 512
	props = newProps()
	applyFullHistorySyncProps(props)
	assert.Equal(t, waCompanionReg.DeviceProps_DESKTOP, props.GetPlatformType())
	assert.True(t, props.GetRequireFullSync())
	assert.Equal(t, uint32(30), props.GetHistorySyncConfig().GetFullSyncDaysLimit())
	assert.Equal(t, uint32(512), props.GetHistorySyncConfig().GetFullSyncSizeMbLimit())
	assert.Equal(t, uint32(10240), props.GetHistorySyncConfig().GetStorageQuotaMb(), "other history settings are kept")
}

// Devices are created concurrently and whatsmeow marshals store.DeviceProps
// without a lock, so configureDeviceProps writes it only once. Under -race, a
// write on every call shows up here as a data race.
func TestConfigureDevicePropsConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			configureDeviceProps()
		}()
	}
	wg.Wait()
}
