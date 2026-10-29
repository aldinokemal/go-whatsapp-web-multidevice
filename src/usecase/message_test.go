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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func failOnMarkRead(t *testing.T) markReadFunc {
	t.Helper()
	return func(context.Context, *whatsmeow.Client, []types.MessageID, time.Time, types.JID, types.JID, ...types.ReceiptType) error {
		t.Helper()
		t.Fatal("played receipt must not be sent")
		return nil
	}
}

func TestMarkAsPlayedSendsPlayedReceiptWithStoredGroupSender(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	groupJID := types.NewJID("120363000000000000", types.GroupServer)
	senderJID := types.NewJID("628987654321", types.DefaultUserServer)
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        "device-a@s.whatsapp.net",
		JID:             groupJID.String(),
		Name:            "Voice group",
		LastMessageTime: time.Now(),
	}))
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID:        "voice-message-1",
		ChatJID:   groupJID.String(),
		DeviceID:  "device-a@s.whatsapp.net",
		Sender:    senderJID.String(),
		Timestamp: time.Now(),
		MediaType: "audio",
	}))

	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return groupJID, nil
	}
	receiptCalled := false
	service.markReadFn = func(
		_ context.Context,
		_ *whatsmeow.Client,
		ids []types.MessageID,
		timestamp time.Time,
		chat types.JID,
		sender types.JID,
		receiptTypes ...types.ReceiptType,
	) error {
		receiptCalled = true
		require.Equal(t, []types.MessageID{"voice-message-1"}, ids)
		require.False(t, timestamp.IsZero())
		require.Equal(t, groupJID, chat)
		require.Equal(t, senderJID, sender)
		require.Equal(t, []types.ReceiptType{types.ReceiptTypePlayed}, receiptTypes)
		return nil
	}

	response, err := service.MarkAsPlayed(ctx, domainMessage.MarkAsPlayedRequest{
		MessageID: "voice-message-1",
		Phone:     groupJID.String(),
	})

	require.NoError(t, err)
	require.True(t, receiptCalled)
	require.Equal(t, "voice-message-1", response.MessageID)
}

func TestMarkAsPlayedDoesNotReadMessageFromAnotherDevice(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	chatJID := types.NewJID("628123456789", types.DefaultUserServer)
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID:        "device-b-voice-message",
		ChatJID:   chatJID.String(),
		DeviceID:  "device-b@s.whatsapp.net",
		Sender:    chatJID.String(),
		Timestamp: time.Now(),
		MediaType: "audio",
	}))
	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return chatJID, nil
	}
	service.markReadFn = failOnMarkRead(t)

	response, err := service.MarkAsPlayed(ctx, domainMessage.MarkAsPlayedRequest{
		MessageID: "device-b-voice-message",
		Phone:     chatJID.String(),
	})

	assert.ErrorContains(t, err, "not found for current device")
	assert.Empty(t, response.MessageID)
}

func TestMarkAsPlayedAcceptsLegacyPTTMediaType(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	chatJID := types.NewJID("628123456789", types.DefaultUserServer)
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID:        "legacy-ptt-message",
		ChatJID:   chatJID.String(),
		DeviceID:  "device-a@s.whatsapp.net",
		Sender:    chatJID.String(),
		Timestamp: time.Now(),
		MediaType: "ptt",
	}))
	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return chatJID, nil
	}
	service.markReadFn = func(
		_ context.Context,
		_ *whatsmeow.Client,
		ids []types.MessageID,
		_ time.Time,
		chat types.JID,
		_ types.JID,
		receiptTypes ...types.ReceiptType,
	) error {
		assert.Equal(t, []types.MessageID{"legacy-ptt-message"}, ids)
		assert.Equal(t, chatJID, chat)
		assert.Equal(t, []types.ReceiptType{types.ReceiptTypePlayed}, receiptTypes)
		return nil
	}

	response, err := service.MarkAsPlayed(ctx, domainMessage.MarkAsPlayedRequest{
		MessageID: "legacy-ptt-message",
		Phone:     chatJID.String(),
	})

	require.NoError(t, err)
	assert.Equal(t, "legacy-ptt-message", response.MessageID)
}

func TestMarkAsPlayedRejectsInvalidStoredMessage(t *testing.T) {
	userChatJID := types.NewJID("628123456789", types.DefaultUserServer)
	storedGroupJID := types.NewJID("120363000000000001", types.GroupServer)
	requestedGroupJID := types.NewJID("120363000000000002", types.GroupServer)
	groupWithoutSenderJID := types.NewJID("120363000000000003", types.GroupServer)
	senderJID := types.NewJID("628987654321", types.DefaultUserServer)

	tests := []struct {
		name          string
		message       domainChatStorage.Message
		requestedChat types.JID
		errContains   string
	}{
		{
			name: "non-audio message",
			message: domainChatStorage.Message{
				ID: "image-message-1", ChatJID: userChatJID.String(), Sender: userChatJID.String(), MediaType: "image",
			},
			requestedChat: userChatJID,
			errContains:   "not an audio message",
		},
		{
			name: "message from different chat",
			message: domainChatStorage.Message{
				ID: "voice-message-2", ChatJID: storedGroupJID.String(), Sender: senderJID.String(), MediaType: "audio",
			},
			requestedChat: requestedGroupJID,
			errContains:   "does not belong to chat",
		},
		{
			name: "group message without sender",
			message: domainChatStorage.Message{
				ID: "voice-message-3", ChatJID: groupWithoutSenderJID.String(), MediaType: "audio",
			},
			requestedChat: groupWithoutSenderJID,
			errContains:   "sender is missing",
		},
		{
			name: "outgoing audio message",
			message: domainChatStorage.Message{
				ID: "outgoing-voice-message", ChatJID: userChatJID.String(), Sender: "device-a@s.whatsapp.net", IsFromMe: true, MediaType: "audio",
			},
			requestedChat: userChatJID,
			errContains:   "is not an incoming message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo, ctx := newMessageActionTestService(t, nil)
			message := tt.message
			message.DeviceID = "device-a@s.whatsapp.net"
			message.Timestamp = time.Now()
			require.NoError(t, repo.StoreMessage(&message))
			service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return tt.requestedChat, nil
			}
			service.markReadFn = failOnMarkRead(t)

			response, err := service.MarkAsPlayed(ctx, domainMessage.MarkAsPlayedRequest{
				MessageID: message.ID,
				Phone:     tt.requestedChat.String(),
			})

			assert.ErrorContains(t, err, tt.errContains)
			assert.Empty(t, response.MessageID)
		})
	}
}

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
