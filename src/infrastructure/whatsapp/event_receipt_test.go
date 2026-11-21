package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestShouldForwardReceipt(t *testing.T) {
	original := config.WhatsappWebhookAllDeviceReceipts
	defer func() { config.WhatsappWebhookAllDeviceReceipts = original }()
	config.WhatsappWebhookAllDeviceReceipts = false
	contact := types.NewJID("628123456789", types.DefaultUserServer)
	own := types.NewJID("628111111111", types.DefaultUserServer)
	ownLID := types.NewJID("251556368777322", types.HiddenUserServer)

	withDevice := func(jid types.JID, device uint16) types.JID {
		jid.Device = device
		return jid
	}
	receipt := func(sender types.JID, fromMe bool, receiptType types.ReceiptType) *events.Receipt {
		return &events.Receipt{
			MessageSource: types.MessageSource{
				Chat:     contact,
				Sender:   sender,
				IsFromMe: fromMe,
			},
			MessageIDs: []types.MessageID{"3EB00106E8BE0F407E88EC"},
			Type:       receiptType,
		}
	}

	tests := []struct {
		name string
		evt  *events.Receipt
		want bool
	}{
		{"contact primary delivered", receipt(contact, false, types.ReceiptTypeDelivered), true},
		{"contact primary read", receipt(contact, false, types.ReceiptTypeRead), true},
		{"contact linked device delivered", receipt(withDevice(contact, 3), false, types.ReceiptTypeDelivered), false},
		{"contact linked device read", receipt(withDevice(contact, 3), false, types.ReceiptTypeRead), true},
		{"contact linked device played", receipt(withDevice(contact, 3), false, types.ReceiptTypePlayed), true},
		{"contact linked device read-self", receipt(withDevice(contact, 3), false, types.ReceiptTypeReadSelf), false},
		{"contact linked device retry", receipt(withDevice(contact, 3), false, types.ReceiptTypeRetry), false},
		{"own linked device played", receipt(withDevice(own, 12), true, types.ReceiptTypePlayed), true},
		{"own primary read-self", receipt(own, true, types.ReceiptTypeReadSelf), true},
		{"own linked device read", receipt(withDevice(own, 12), true, types.ReceiptTypeRead), true},
		{"own linked device read-self", receipt(withDevice(own, 12), true, types.ReceiptTypeReadSelf), true},
		{"own linked device read-self via LID", receipt(withDevice(ownLID, 12), true, types.ReceiptTypeReadSelf), true},
		{"own linked device delivered", receipt(withDevice(own, 12), true, types.ReceiptTypeDelivered), false},
		{"own linked device played-self", receipt(withDevice(own, 12), true, types.ReceiptTypePlayedSelf), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, shouldForwardReceipt(tt.evt))
		})
	}
}

// handleReceipt decides which receipt types are forwarded at all; a type it does
// not dispatch never reaches shouldForwardReceipt. A contact's linked-device read
// and played receipts must reach the webhook, and its delivery receipts must not.
func TestHandleReceiptForwardsLinkedDeviceReadAndPlayed(t *testing.T) {
	originalWebhooks, originalEvents := config.WhatsappWebhook, config.WhatsappWebhookEvents
	originalAllDevices := config.WhatsappWebhookAllDeviceReceipts
	originalSubmit, originalLog := submitWebhookFn, log
	t.Cleanup(func() {
		config.WhatsappWebhook, config.WhatsappWebhookEvents = originalWebhooks, originalEvents
		config.WhatsappWebhookAllDeviceReceipts = originalAllDevices
		submitWebhookFn, log = originalSubmit, originalLog
	})
	log = waLog.Noop
	config.WhatsappWebhook = []string{"https://example.test/webhook"}
	config.WhatsappWebhookEvents = nil
	config.WhatsappWebhookAllDeviceReceipts = false
	forwarded := make(chan string, 4)
	submitWebhookFn = func(_ context.Context, body map[string]any, _ string, _ *domainChatStorage.DeviceWebhookConfig) error {
		payload, _ := body["payload"].(map[string]any)
		receiptType, _ := payload["receipt_type"].(string)
		forwarded <- receiptType
		return nil
	}

	own := types.NewJID("628111111111", types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &own}}
	contactLinked := types.NewJID("628123456789", types.DefaultUserServer)
	contactLinked.Device = 3
	receipt := func(receiptType types.ReceiptType) *events.Receipt {
		return &events.Receipt{
			MessageSource: types.MessageSource{Chat: contactLinked.ToNonAD(), Sender: contactLinked},
			MessageIDs:    []types.MessageID{"3EB00106E8BE0F407E88EC"},
			Type:          receiptType,
		}
	}

	for _, receiptType := range []types.ReceiptType{types.ReceiptTypeRead, types.ReceiptTypePlayed} {
		handleReceipt(context.Background(), receipt(receiptType), "", client)
		select {
		case got := <-forwarded:
			assert.Equal(t, string(receiptType), got)
		case <-time.After(5 * time.Second):
			t.Fatalf("%q receipt from a contact's linked device was not forwarded", receiptType)
		}
	}

	// Called directly rather than through handleReceipt: its goroutine would still
	// read the config while the cleanup restores it.
	if err := forwardReceiptToWebhook(context.Background(), receipt(types.ReceiptTypeDelivered), "", client); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-forwarded:
		t.Fatalf("a contact's linked-device delivery receipt was forwarded as %q", got)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestCreateReceiptPayloadOwnLinkedDeviceReadSelf(t *testing.T) {
	own := types.NewJID("628111111111", types.DefaultUserServer)
	ownCompanion := own
	ownCompanion.Device = 12
	contact := types.NewJID("628123456789", types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &own}}

	body := createReceiptPayload(context.Background(), &events.Receipt{
		MessageSource: types.MessageSource{
			Chat:     contact,
			Sender:   ownCompanion,
			IsFromMe: true,
		},
		MessageIDs: []types.MessageID{"3A1", "3A2"},
		Timestamp:  time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC),
		Type:       types.ReceiptTypeReadSelf,
	}, own.String(), client)

	assert.Equal(t, "message.ack", body["event"])
	payload, ok := body["payload"].(map[string]any)
	if !assert.True(t, ok) {
		return
	}
	assert.Equal(t, []types.MessageID{"3A1", "3A2"}, payload["ids"])
	assert.Equal(t, contact.String(), payload["chat_id"])
	assert.Equal(t, own.String(), payload["from"], "device suffix must be stripped from the sender")
	assert.Equal(t, "read-self", payload["receipt_type"])
}

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
