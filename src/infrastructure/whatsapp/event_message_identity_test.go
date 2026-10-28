package whatsapp

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestPhoneAddressedLiveEventHonorsKnownLIDIgnoreRules(t *testing.T) {
	original := config.ChatwootIgnoreJids
	t.Cleanup(func() { config.ChatwootIgnoreJids = original })
	phone := types.NewJID("15551234567", types.DefaultUserServer)
	lid := types.NewJID("123456", types.HiddenUserServer)
	client := &whatsmeow.Client{Store: &store.Device{LIDs: &chatDisplayNameLIDStore{pn: phone, lid: lid}}}
	log := &chatwootRequestLog{}
	server := httptest.NewServer(log)
	t.Cleanup(server.Close)
	var deviceIgnore []string
	stubChatwootDevice(t, server, &deviceIgnore)
	for _, group := range []bool{false, true} {
		for _, scope := range []string{"global", "device"} {
			for _, rule := range []string{lid.String(), "@lid"} {
				name := scope + "/" + rule
				if group {
					name += "/group"
				}
				t.Run(name, func(t *testing.T) {
					chat := phone
					if group {
						chat = types.NewJID("120363000000000001", types.GroupServer)
					}
					sender := phone
					sender.Device = 2
					event := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsGroup: group}, ID: "synthetic-live"}}
					data := map[string]any{"id": "synthetic-live", "body": "synthetic text"}
					buildFromFields(context.Background(), client, event, data)
					require.Equal(t, phone.String(), data["from"])
					require.Equal(t, lid.String(), data["from_lid"])
					if !group {
						require.Equal(t, lid.String(), data["chat_lid"])
					} else {
						require.NotContains(t, data, "chat_lid")
					}
					config.ChatwootIgnoreJids = nil
					deviceIgnore = nil
					if scope == "global" {
						config.ChatwootIgnoreJids = []string{rule}
					} else {
						deviceIgnore = []string{rule}
					}
					require.NoError(t, syncPayloadToChatwoot(context.Background(), map[string]any{"payload": data}, "message", "device-a", nil))
					require.Empty(t, log.take(), "ignored live identity must cause no Chatwoot request")
				})
			}
		}
	}
}

type failingReverseLIDStore struct{ store.NoopStore }

func (failingReverseLIDStore) GetLIDForPN(context.Context, types.JID) (types.JID, error) {
	return types.EmptyJID, errors.New("mapping unavailable")
}
func TestPhoneIdentityEnrichmentPreservesUnresolvedOriginal(t *testing.T) {
	phone := types.NewJID("15551234567", types.DefaultUserServer)
	for name, client := range map[string]*whatsmeow.Client{
		"nil client": nil, "nil store": {}, "nil mappings": {Store: &store.Device{}},
		"missing mapping":      {Store: &store.Device{LIDs: &chatDisplayNameLIDStore{}}},
		"mapping error":        {Store: &store.Device{LIDs: &failingReverseLIDStore{}}},
		"wrong mapping domain": {Store: &store.Device{LIDs: &chatDisplayNameLIDStore{pn: phone, lid: types.NewJID("999", types.DefaultUserServer)}}},
	} {
		t.Run(name, func(t *testing.T) {
			data := map[string]any{}
			event := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: phone, Sender: phone}}}
			buildFromFields(context.Background(), client, event, data)
			require.Equal(t, phone.String(), data["chat_id"])
			require.Equal(t, phone.String(), data["from"])
			require.NotContains(t, data, "chat_lid")
			require.NotContains(t, data, "from_lid")
		})
	}
}
