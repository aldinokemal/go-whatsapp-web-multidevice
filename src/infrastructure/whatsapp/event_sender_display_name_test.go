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
	waLog "go.mau.fi/whatsmeow/util/log"
)

type callOfferLIDStore struct {
	store.NoopStore
	phoneNumbers map[types.JID]types.JID
}

func (s *callOfferLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	return s.phoneNumbers[lid], nil
}

func TestSenderBearingWebhookPayloadsIncludeSenderDisplayName(t *testing.T) {
	accountJID := types.NewJID("628111111111", types.DefaultUserServer)
	senderJID := types.NewJID("628123456789", types.DefaultUserServer)
	client := &whatsmeow.Client{
		Store: &store.Device{
			ID:       &accountJID,
			PushName: "Active Account",
			Contacts: &senderDisplayNameContactGetter{
				contacts: map[types.JID]types.ContactInfo{
					senderJID: {
						Found:    true,
						FullName: "Saved Contact",
					},
				},
			},
		},
	}

	tests := []struct {
		name  string
		build func(*testing.T) map[string]any
	}{
		{
			name: "receipt",
			build: func(*testing.T) map[string]any {
				return createReceiptPayload(context.Background(), &events.Receipt{
					MessageSource: types.MessageSource{
						Chat:   types.NewJID("628100000000", types.DefaultUserServer),
						Sender: senderJID,
					},
					MessageIDs: []types.MessageID{"receipt-message"},
					Timestamp:  time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC),
					Type:       types.ReceiptTypeRead,
				}, accountJID.String(), client)
			},
		},
		{
			name: "delete",
			build: func(t *testing.T) map[string]any {
				t.Helper()
				payload, err := createDeletePayload(context.Background(), &events.DeleteForMe{
					SenderJID: senderJID,
					MessageID: "deleted-message",
				}, nil, accountJID.String(), client)
				assert.NoError(t, err)
				return payload
			},
		},
		{
			name: "chat presence",
			build: func(*testing.T) map[string]any {
				return createChatPresencePayload(context.Background(), &events.ChatPresence{
					MessageSource: types.MessageSource{
						Chat:   types.NewJID("628100000000", types.DefaultUserServer),
						Sender: senderJID,
					},
					State: types.ChatPresenceComposing,
				}, accountJID.String(), client)
			},
		},
		{
			name: "call offer",
			build: func(*testing.T) map[string]any {
				return createCallOfferPayload(context.Background(), &events.CallOffer{
					BasicCallMeta: types.BasicCallMeta{
						CallCreator: senderJID,
						CallID:      "call-id",
						Timestamp:   time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC),
					},
				}, accountJID.String(), client, false)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.build(t)
			payload := body["payload"].(map[string]any)

			assert.Equal(t, senderJID.String(), payload["from"])
			assert.Equal(t, "Saved Contact", payload["sender_display_name"])
		})
	}
}

func TestCreateCallOfferPayloadNormalizesCaller(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	accountJID := types.NewJID("628111111111", types.DefaultUserServer)
	lidCaller := types.NewJID("251556368777322", types.HiddenUserServer)
	phoneCaller := types.NewJID("628123456789", types.DefaultUserServer)
	client := &whatsmeow.Client{
		Store: &store.Device{
			ID: &accountJID,
			Contacts: &senderDisplayNameContactGetter{
				contacts: map[types.JID]types.ContactInfo{
					phoneCaller: {
						Found:    true,
						FullName: "Saved Caller",
					},
				},
			},
			LIDs: &callOfferLIDStore{
				phoneNumbers: map[types.JID]types.JID{
					lidCaller: phoneCaller,
				},
			},
		},
	}

	tests := []struct {
		name        string
		caller      types.JID
		wantFrom    string
		wantFromLID string
	}{
		{
			name:        "LID caller",
			caller:      lidCaller,
			wantFrom:    "628123456789@s.whatsapp.net",
			wantFromLID: "251556368777322@lid",
		},
		{
			name:     "non-LID caller",
			caller:   phoneCaller,
			wantFrom: "628123456789@s.whatsapp.net",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := createCallOfferPayload(context.Background(), &events.CallOffer{
				BasicCallMeta: types.BasicCallMeta{
					CallCreator: tt.caller,
					CallID:      "call-id",
					Timestamp:   time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC),
				},
			}, accountJID.String(), client, false)
			payload := body["payload"].(map[string]any)

			assert.Equal(t, tt.wantFrom, payload["from"])
			assert.Equal(t, "Saved Caller", payload["sender_display_name"])
			if tt.wantFromLID == "" {
				assert.NotContains(t, payload, "from_lid")
			} else {
				assert.Equal(t, tt.wantFromLID, payload["from_lid"])
			}
		})
	}
}
