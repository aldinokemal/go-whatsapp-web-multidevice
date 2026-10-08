package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestShouldForwardReceipt(t *testing.T) {
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
		{"contact linked device read", receipt(withDevice(contact, 3), false, types.ReceiptTypeRead), false},
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
