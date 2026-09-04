package whatsapp

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func receiptFromDevice(device uint16) *events.Receipt {
	return &events.Receipt{
		MessageSource: types.MessageSource{
			Sender: types.JID{User: "6289685028129", Device: device, Server: types.DefaultUserServer},
		},
	}
}

func TestShouldForwardReceipt_PrimaryDeviceOnlyByDefault(t *testing.T) {
	original := config.WhatsappWebhookAllDeviceReceipts
	defer func() { config.WhatsappWebhookAllDeviceReceipts = original }()

	config.WhatsappWebhookAllDeviceReceipts = false

	if !shouldForwardReceipt(receiptFromDevice(0)) {
		t.Error("receipt from the primary device (Device == 0) should be forwarded by default")
	}
	if shouldForwardReceipt(receiptFromDevice(2)) {
		t.Error("receipt from a linked device should be skipped by default")
	}
}

func TestShouldForwardReceipt_AllDevicesWhenEnabled(t *testing.T) {
	original := config.WhatsappWebhookAllDeviceReceipts
	defer func() { config.WhatsappWebhookAllDeviceReceipts = original }()

	config.WhatsappWebhookAllDeviceReceipts = true

	if !shouldForwardReceipt(receiptFromDevice(0)) {
		t.Error("receipt from the primary device should be forwarded when all-device forwarding is enabled")
	}
	if !shouldForwardReceipt(receiptFromDevice(5)) {
		t.Error("receipt from a linked device should be forwarded when all-device forwarding is enabled")
	}
}
