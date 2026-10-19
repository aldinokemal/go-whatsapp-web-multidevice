package usecase

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func failOnReadReceipt(t *testing.T) markReadFunc {
	t.Helper()
	return func(context.Context, *whatsmeow.Client, []types.MessageID, time.Time, types.JID, types.JID, ...types.ReceiptType) error {
		t.Helper()
		t.Fatal("read receipt must not be sent")
		return nil
	}
}

func storeGroupMessageForReadTest(t *testing.T, repo domainChatStorage.IChatStorageRepository, deviceID string, groupJID, senderJID types.JID, messageID string) {
	t.Helper()
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             groupJID.String(),
		Name:            "Read receipt group",
		LastMessageTime: time.Now(),
	}))
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   groupJID.String(),
		DeviceID:  deviceID,
		Sender:    senderJID.String(),
		Content:   "stored incoming message",
		Timestamp: time.Now(),
	}))
}

func TestMarkAsReadSendsReceiptWithStoredGroupSender(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	groupJID := types.NewJID("120363000000000000", types.GroupServer)
	senderJID := types.NewJID("628987654321", types.DefaultUserServer)
	storeGroupMessageForReadTest(t, repo, "device-a@s.whatsapp.net", groupJID, senderJID, "group-message-1")

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
		require.Equal(t, []types.MessageID{"group-message-1"}, ids)
		require.False(t, timestamp.IsZero())
		require.Equal(t, groupJID, chat)
		require.Equal(t, senderJID, sender)
		require.Empty(t, receiptTypes)
		return nil
	}

	response, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: "group-message-1",
		Phone:     groupJID.String(),
	})

	require.NoError(t, err)
	require.True(t, receiptCalled)
	require.Equal(t, "group-message-1", response.MessageID)
}

func TestMarkAsReadSelectsMessageByChatWhenIDCollides(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	const messageID = "duplicate-group-message"
	otherGroupJID := types.NewJID("120363000000000010", types.GroupServer)
	targetGroupJID := types.NewJID("120363000000000020", types.GroupServer)
	otherSenderJID := types.NewJID("628111111111", types.DefaultUserServer)
	targetSenderJID := types.NewJID("628222222222", types.DefaultUserServer)

	// Store the colliding row first. An ID+device-only lookup can select this
	// row even though the request targets targetGroupJID.
	storeGroupMessageForReadTest(t, repo, "device-a@s.whatsapp.net", otherGroupJID, otherSenderJID, messageID)
	storeGroupMessageForReadTest(t, repo, "device-a@s.whatsapp.net", targetGroupJID, targetSenderJID, messageID)

	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return targetGroupJID, nil
	}
	service.markReadFn = func(
		_ context.Context,
		_ *whatsmeow.Client,
		ids []types.MessageID,
		_ time.Time,
		chat types.JID,
		sender types.JID,
		_ ...types.ReceiptType,
	) error {
		require.Equal(t, []types.MessageID{messageID}, ids)
		require.Equal(t, targetGroupJID, chat)
		require.Equal(t, targetSenderJID, sender)
		return nil
	}

	response, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: messageID,
		Phone:     targetGroupJID.String(),
	})

	require.NoError(t, err)
	require.Equal(t, messageID, response.MessageID)
}

func TestMarkAsReadRejectsGroupMessageFromAnotherDevice(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	groupJID := types.NewJID("120363000000000001", types.GroupServer)
	senderJID := types.NewJID("628987654321", types.DefaultUserServer)
	storeGroupMessageForReadTest(t, repo, "device-b@s.whatsapp.net", groupJID, senderJID, "other-device-message")
	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return groupJID, nil
	}
	service.markReadFn = failOnReadReceipt(t)

	response, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: "other-device-message",
		Phone:     groupJID.String(),
	})

	assert.ErrorContains(t, err, "not found for current device")
	assert.Empty(t, response.MessageID)
}

func TestMarkAsReadRejectsMessageFromDifferentGroup(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	storedGroupJID := types.NewJID("120363000000000002", types.GroupServer)
	requestedGroupJID := types.NewJID("120363000000000003", types.GroupServer)
	senderJID := types.NewJID("628987654321", types.DefaultUserServer)
	storeGroupMessageForReadTest(t, repo, "device-a@s.whatsapp.net", storedGroupJID, senderJID, "wrong-group-message")
	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return requestedGroupJID, nil
	}
	service.markReadFn = failOnReadReceipt(t)

	response, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: "wrong-group-message",
		Phone:     requestedGroupJID.String(),
	})

	assert.ErrorContains(t, err, "not found for current device and chat")
	assert.Empty(t, response.MessageID)
}

func TestMarkAsReadRejectsGroupMessageWithoutSender(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	groupJID := types.NewJID("120363000000000004", types.GroupServer)
	storeGroupMessageForReadTest(t, repo, "device-a@s.whatsapp.net", groupJID, types.JID{}, "missing-sender-message")
	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return groupJID, nil
	}
	service.markReadFn = failOnReadReceipt(t)

	response, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: "missing-sender-message",
		Phone:     groupJID.String(),
	})

	assert.ErrorContains(t, err, "sender is missing")
	assert.Empty(t, response.MessageID)
}

func TestMarkAsReadRejectsNonUserGroupSender(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	groupJID := types.NewJID("120363000000000005", types.GroupServer)
	invalidSenderJID := types.NewJID("120363999999999999", types.GroupServer)
	storeGroupMessageForReadTest(t, repo, "device-a@s.whatsapp.net", groupJID, invalidSenderJID, "invalid-sender-message")
	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return groupJID, nil
	}
	service.markReadFn = failOnReadReceipt(t)

	response, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: "invalid-sender-message",
		Phone:     groupJID.String(),
	})

	assert.ErrorContains(t, err, "invalid sender")
	assert.Empty(t, response.MessageID)
}

func TestMarkAsReadAcceptsSupportedUserSenderServers(t *testing.T) {
	supportedServers := []string{
		types.DefaultUserServer,
		types.HiddenUserServer,
		types.LegacyUserServer,
		types.MessengerServer,
	}

	for index, server := range supportedServers {
		t.Run(server, func(t *testing.T) {
			service, repo, ctx := newMessageActionTestService(t, nil)
			groupJID := types.NewJID("12036300000000010"+string(rune('0'+index)), types.GroupServer)
			senderJID := types.NewJID("62898765432"+string(rune('0'+index)), server)
			messageID := "supported-sender-" + server
			storeGroupMessageForReadTest(t, repo, "device-a@s.whatsapp.net", groupJID, senderJID, messageID)
			service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return groupJID, nil
			}
			service.markReadFn = func(
				_ context.Context,
				_ *whatsmeow.Client,
				_ []types.MessageID,
				_ time.Time,
				_ types.JID,
				gotSender types.JID,
				_ ...types.ReceiptType,
			) error {
				require.Equal(t, senderJID.ToNonAD(), gotSender)
				return nil
			}

			_, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
				MessageID: messageID,
				Phone:     groupJID.String(),
			})
			require.NoError(t, err)
		})
	}
}

func TestMarkAsReadKeepsDirectChatBehaviorWithoutStorage(t *testing.T) {
	service, _, ctx := newMessageActionTestService(t, nil)
	service.chatStorageRepo = nil

	chatJID := types.NewJID("628123456789", types.DefaultUserServer)
	deviceJID := types.NewJID("device-a", types.DefaultUserServer)
	service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
		return chatJID, nil
	}
	service.markReadFn = func(
		_ context.Context,
		_ *whatsmeow.Client,
		ids []types.MessageID,
		_ time.Time,
		chat types.JID,
		sender types.JID,
		receiptTypes ...types.ReceiptType,
	) error {
		require.Equal(t, []types.MessageID{"direct-message"}, ids)
		require.Equal(t, chatJID, chat)
		require.Equal(t, deviceJID, sender)
		require.Empty(t, receiptTypes)
		return nil
	}

	response, err := service.MarkAsRead(ctx, domainMessage.MarkAsReadRequest{
		MessageID: "direct-message",
		Phone:     chatJID.String(),
	})

	require.NoError(t, err)
	require.Equal(t, "direct-message", response.MessageID)
}
