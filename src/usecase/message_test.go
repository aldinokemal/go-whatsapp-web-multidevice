package usecase

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func TestMessageActionsDeleteStoredMessageAfterWhatsAppSuccess(t *testing.T) {
	tests := []struct {
		name string
		run  func(service serviceMessage, ctx context.Context) error
	}{
		{
			name: "revoke",
			run: func(service serviceMessage, ctx context.Context) error {
				_, err := service.RevokeMessage(ctx, domainMessage.RevokeRequest{
					MessageID: "message-1",
					Phone:     "628123456789@s.whatsapp.net",
				})
				return err
			},
		},
		{
			name: "delete for me",
			run: func(service serviceMessage, ctx context.Context) error {
				return service.DeleteMessage(ctx, domainMessage.DeleteRequest{
					MessageID: "message-1",
					Phone:     "628123456789@s.whatsapp.net",
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, ctx := newMessageActionTestService(t, nil)

			require.NoError(t, tc.run(service, ctx))

			message, err := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", "message-1")
			require.NoError(t, err)
			require.Nil(t, message)

			otherDeviceMessage, err := repo.GetMessageByIDAndDevice("device-b@s.whatsapp.net", "message-1")
			require.NoError(t, err)
			require.NotNil(t, otherDeviceMessage)
		})
	}
}

func TestMessageActionsKeepStoredMessageWhenWhatsAppFails(t *testing.T) {
	remoteErr := errors.New("whatsapp unavailable")
	tests := []struct {
		name string
		run  func(service serviceMessage, ctx context.Context) error
	}{
		{
			name: "revoke",
			run: func(service serviceMessage, ctx context.Context) error {
				_, err := service.RevokeMessage(ctx, domainMessage.RevokeRequest{
					MessageID: "message-1",
					Phone:     "628123456789@s.whatsapp.net",
				})
				return err
			},
		},
		{
			name: "delete for me",
			run: func(service serviceMessage, ctx context.Context) error {
				return service.DeleteMessage(ctx, domainMessage.DeleteRequest{
					MessageID: "message-1",
					Phone:     "628123456789@s.whatsapp.net",
				})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, ctx := newMessageActionTestService(t, remoteErr)

			err := tc.run(service, ctx)
			require.ErrorIs(t, err, remoteErr)

			message, lookupErr := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", "message-1")
			require.NoError(t, lookupErr)
			require.NotNil(t, message)
		})
	}
}

func newMessageActionTestService(t *testing.T, remoteErr error) (serviceMessage, domainChatStorage.IChatStorageRepository, context.Context) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())

	chatJID := "628123456789@s.whatsapp.net"
	timestamp := time.Date(2026, time.August, 22, 11, 0, 0, 0, time.UTC)
	for _, deviceID := range []string{"device-a@s.whatsapp.net", "device-b@s.whatsapp.net"} {
		require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{
			DeviceID:        deviceID,
			JID:             chatJID,
			Name:            chatJID,
			LastMessageTime: timestamp,
		}))
		require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
			ID:        "message-1",
			ChatJID:   chatJID,
			DeviceID:  deviceID,
			Sender:    deviceID,
			Content:   "stored content",
			Timestamp: timestamp,
			IsFromMe:  true,
		}))
	}

	deviceJID := types.NewJID("device-a", types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &waStore.Device{ID: &deviceJID}}
	instance := whatsapp.NewDeviceInstance("device-a", client, repo)
	ctx := whatsapp.ContextWithDevice(context.Background(), instance)
	recipient := types.NewJID("628123456789", types.DefaultUserServer)

	service := serviceMessage{
		chatStorageRepo: repo,
		validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
			return recipient, nil
		},
		sendMessageFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, _ *waE2E.Message) (whatsmeow.SendResponse, error) {
			return whatsmeow.SendResponse{ID: "action-1", Timestamp: timestamp}, remoteErr
		},
		sendAppStateFn: func(_ context.Context, _ *whatsmeow.Client, _ appstate.PatchInfo) error {
			return remoteErr
		},
	}
	return service, repo, ctx
}
