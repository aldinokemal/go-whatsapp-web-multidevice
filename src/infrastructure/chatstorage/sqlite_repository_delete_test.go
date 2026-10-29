package chatstorage

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestCreateMessageRevokeDeletesTargetForActiveDevice(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	otherDeviceID := "device-b@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	timestamp := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)

	seedChatMessage(t, repo, deviceID, chatJID, "message-1", "remove me", timestamp)
	seedChatMessage(t, repo, otherDeviceID, chatJID, "message-1", "keep me", timestamp)
	// The revoke sender is the author. seedChatMessage stores a different sender.
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID:        "message-1",
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    "628123456789@s.whatsapp.net",
		Content:   "remove me",
		Timestamp: timestamp,
	}))

	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, repo))
	revoke := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("628123456789", types.DefaultUserServer),
				Sender: types.NewJID("628123456789", types.DefaultUserServer),
			},
			ID:        "revoke-event-1",
			Timestamp: timestamp.Add(time.Minute),
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			Key: &waCommon.MessageKey{
				RemoteJID: proto.String(chatJID),
				ID:        proto.String("message-1"),
			},
		}},
	}

	require.NoError(t, repo.CreateMessage(ctx, revoke))

	deleted, err := repo.GetMessageByIDAndDevice(deviceID, "message-1")
	require.NoError(t, err)
	require.Nil(t, deleted)

	preserved, err := repo.GetMessageByIDAndDevice(otherDeviceID, "message-1")
	require.NoError(t, err)
	require.NotNil(t, preserved)
	require.Equal(t, "keep me", preserved.Content)
}

func TestDeleteMessageByDevicePurgesContentButPreservesChatwootLink(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	timestamp := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)

	seedChatMessage(t, repo, deviceID, chatJID, "message-1", "latest content", timestamp)
	seedReaction(t, repo, deviceID, chatJID, "message-1", "628111111111@s.whatsapp.net")
	require.NoError(t, repo.StoreMessageEdit(&domainChatStorage.MessageEdit{
		OriginalMessageID: "message-1",
		EditEventID:       "edit-1",
		ChatJID:           chatJID,
		DeviceID:          deviceID,
		Editor:            deviceID,
		PreviousContent:   "sensitive original content",
		NewContent:        "latest content",
		EditedAt:          timestamp,
	}))
	require.NoError(t, repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID:               deviceID,
		WhatsAppMessageID:      "message-1",
		WhatsAppChatJID:        chatJID,
		ChatwootMessageID:      101,
		ChatwootConversationID: 202,
		ChatwootInboxID:        303,
		ChatwootConfigID:       404,
		ChatwootAccountID:      505,
	}))

	require.NoError(t, repo.DeleteMessageByDevice(deviceID, "message-1", chatJID))

	message, err := repo.GetMessageByIDAndDevice(deviceID, "message-1")
	require.NoError(t, err)
	require.Nil(t, message)

	edits, err := repo.GetMessageEdits("message-1", deviceID)
	require.NoError(t, err)
	require.Empty(t, edits)
	require.Zero(t, countMessageReactions(t, repo))

	link, err := repo.GetChatwootMessageLinkByWhatsAppID(deviceID, "message-1")
	require.NoError(t, err)
	require.NotNil(t, link)
	require.Equal(t, 101, link.ChatwootMessageID)
}

func TestDeleteMessageRollsBackDependentDeletesWhenMessageDeleteFails(t *testing.T) {
	tests := []struct {
		name   string
		delete func(repo *SQLiteRepository, deviceID, messageID, chatJID string) error
	}{
		{
			name: "unscoped",
			delete: func(repo *SQLiteRepository, _, messageID, chatJID string) error {
				return repo.DeleteMessage(messageID, chatJID)
			},
		},
		{
			name: "device scoped",
			delete: func(repo *SQLiteRepository, deviceID, messageID, chatJID string) error {
				return repo.DeleteMessageByDevice(deviceID, messageID, chatJID)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			deviceID := "device-a@s.whatsapp.net"
			chatJID := "628123456789@s.whatsapp.net"
			timestamp := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)

			seedChatMessage(t, repo, deviceID, chatJID, "message-1", "latest content", timestamp)
			seedReaction(t, repo, deviceID, chatJID, "message-1", "628111111111@s.whatsapp.net")
			require.NoError(t, repo.StoreMessageEdit(&domainChatStorage.MessageEdit{
				OriginalMessageID: "message-1",
				EditEventID:       "edit-1",
				ChatJID:           chatJID,
				DeviceID:          deviceID,
				Editor:            deviceID,
				PreviousContent:   "original content",
				NewContent:        "latest content",
				EditedAt:          timestamp,
			}))
			_, err := repo.db.Exec(`
				CREATE TRIGGER fail_target_message_delete
				BEFORE DELETE ON messages
				WHEN OLD.id = 'message-1'
				BEGIN
					SELECT RAISE(ABORT, 'forced message delete failure');
				END
			`)
			require.NoError(t, err)

			err = tc.delete(repo, deviceID, "message-1", chatJID)
			require.ErrorContains(t, err, "forced message delete failure")

			message, lookupErr := repo.GetMessageByIDAndDevice(deviceID, "message-1")
			require.NoError(t, lookupErr)
			require.NotNil(t, message)
			require.Equal(t, 1, countMessageReactions(t, repo))

			edits, editsErr := repo.GetMessageEdits("message-1", deviceID)
			require.NoError(t, editsErr)
			require.Len(t, edits, 1)
		})
	}
}

func TestCreateMessageRevokeIgnoresNonAuthor(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := authzDevice(t)
	chatJID := "628123456789@s.whatsapp.net"
	timestamp := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	seedChatMessage(t, repo, deviceID, chatJID, "message-foreign", "keep me", timestamp)

	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, repo))
	revoke := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("628123456789", types.DefaultUserServer),
				Sender: types.NewJID("628000000001", types.DefaultUserServer),
			},
			ID:        "revoke-event-foreign",
			Timestamp: timestamp.Add(time.Minute),
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			Key: &waCommon.MessageKey{
				RemoteJID: proto.String(chatJID),
				ID:        proto.String("message-foreign"),
			},
		}},
	}
	require.NoError(t, repo.CreateMessage(ctx, revoke))

	kept, err := repo.GetMessageByIDAndDevice(deviceID, "message-foreign")
	require.NoError(t, err)
	require.NotNil(t, kept)
	require.Equal(t, "keep me", kept.Content)

	authorized, ok := whatsapp.MessageRevokeAuthorized(deviceID, chatJID, "message-foreign")
	require.True(t, ok)
	require.False(t, authorized)
}

func TestRevokeAuthorizationSurvivesLaterMissingRow(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := authzDevice(t)
	chatJID := "628123456789@s.whatsapp.net"
	timestamp := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID: "message-1", ChatJID: chatJID, DeviceID: deviceID,
		Sender: "628123456789@s.whatsapp.net", Content: "hello", Timestamp: timestamp,
	}))
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, repo))

	author := revokeEvent(chatJID, "revoke-ok", "628123456789", timestamp.Add(time.Minute))
	require.NoError(t, repo.CreateMessage(ctx, author))
	authorized, ok := whatsapp.MessageRevokeAuthorized(deviceID, chatJID, "message-1")
	require.True(t, ok)
	require.True(t, authorized)
	eventOK, ok := whatsapp.MessageEventAuthorized(deviceID, "revoke-ok")
	require.True(t, ok)
	require.True(t, eventOK)

	// The row is gone. A peer revoke of the same id must not flip the note.
	peer := revokeEvent(chatJID, "revoke-peer", "628000000001", timestamp.Add(2*time.Minute))
	require.NoError(t, repo.CreateMessage(ctx, peer))
	authorized, ok = whatsapp.MessageRevokeAuthorized(deviceID, chatJID, "message-1")
	require.True(t, ok)
	require.True(t, authorized, "missing-row revoke cleared an authorized decision")
	peerOK, ok := whatsapp.MessageEventAuthorized(deviceID, "revoke-peer")
	require.True(t, ok)
	require.False(t, peerOK)
}

func TestUnauthorizedRevokeDoesNotClearAuthorization(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := authzDevice(t)
	chatJID := "628123456789@s.whatsapp.net"
	timestamp := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID: "message-1", ChatJID: chatJID, DeviceID: deviceID,
		Sender: "628123456789@s.whatsapp.net", Content: "hello", Timestamp: timestamp,
	}))
	whatsapp.NoteMessageRevokeAuthorization(deviceID, chatJID, "message-1", true)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, repo))

	peer := revokeEvent(chatJID, "revoke-late", "628000000001", timestamp.Add(time.Minute))
	require.NoError(t, repo.CreateMessage(ctx, peer))

	kept, err := repo.GetMessageByIDAndDevice(deviceID, "message-1")
	require.NoError(t, err)
	require.NotNil(t, kept)
	authorized, ok := whatsapp.MessageRevokeAuthorized(deviceID, chatJID, "message-1")
	require.True(t, ok)
	require.True(t, authorized)
	eventOK, ok := whatsapp.MessageEventAuthorized(deviceID, "revoke-late")
	require.True(t, ok)
	require.False(t, eventOK)
}

func revokeEvent(chatJID, eventID, senderUser string, at time.Time) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("628123456789", types.DefaultUserServer),
				Sender: types.NewJID(senderUser, types.DefaultUserServer),
			},
			ID:        eventID,
			Timestamp: at,
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			Key: &waCommon.MessageKey{
				RemoteJID: proto.String(chatJID),
				ID:        proto.String("message-1"),
			},
		}},
	}
}

// authzDevice returns a device id unique to the running test, with Chatwoot
// enabled so CreateMessage records edit/revoke decisions. Both are undone at
// cleanup, so a -count or -shuffle run never reads another test's decision.
func authzDevice(t *testing.T) string {
	t.Helper()
	enabled := config.ChatwootEnabled
	config.ChatwootEnabled = true
	deviceID := t.Name()
	t.Cleanup(func() {
		config.ChatwootEnabled = enabled
		whatsapp.ForgetMessageAuthorizationsForTest(deviceID)
	})
	return deviceID
}

func TestCreateMessageGroupAdminRevoke(t *testing.T) {
	const (
		groupJID = "120363000000000001@g.us"
		author   = "628111111111@s.whatsapp.net"
		revoker  = "628222222222@s.whatsapp.net"
	)
	for _, tc := range []struct {
		name        string
		admin       bool
		participant string
		authorized  bool
	}{
		{"admin deletes the author message", true, author, true},
		{"non-admin peer is denied", false, author, false},
		{"admin naming another author is denied", true, revoker, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			deviceID := authzDevice(t)
			var asked []string
			orig := isGroupAdmin
			isGroupAdmin = func(_ context.Context, _ *whatsmeow.Client, group types.JID, user ...types.JID) bool {
				asked = append(asked, group.String(), user[0].String())
				return tc.admin
			}
			t.Cleanup(func() { isGroupAdmin = orig })
			require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
				ID: "SPAM", ChatJID: groupJID, DeviceID: deviceID, Sender: author, Content: "spam", Timestamp: time.Now(),
			}))

			group, _ := types.ParseJID(groupJID)
			sender, _ := types.ParseJID(revoker)
			ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, repo))
			require.NoError(t, repo.CreateMessage(ctx, &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{Chat: group, Sender: sender, IsGroup: true},
					ID:            "REV-ADMIN",
					Edit:          types.EditAttributeAdminRevoke,
					Timestamp:     time.Now(),
				},
				Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
					Type: waE2E.ProtocolMessage_REVOKE.Enum(),
					Key: &waCommon.MessageKey{
						RemoteJID:   proto.String(groupJID),
						ID:          proto.String("SPAM"),
						FromMe:      proto.Bool(false),
						Participant: proto.String(tc.participant),
					},
				}},
			}))

			kept, err := repo.GetMessageByIDAndDevice(deviceID, "SPAM")
			require.NoError(t, err)
			require.Equal(t, !tc.authorized, kept != nil)
			eventOK, ok := whatsapp.MessageEventAuthorized(deviceID, "REV-ADMIN")
			require.True(t, ok)
			require.Equal(t, tc.authorized, eventOK)
			if tc.authorized {
				require.Equal(t, []string{groupJID, revoker}, asked)
			}
		})
	}
}

func TestCreateMessageOwnRevokePurgesUnstoredPoll(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := authzDevice(t)
	chatJID := "628123456789@s.whatsapp.net"
	require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: deviceID, ChatJID: chatJID, PollMessageID: "POLL-OWN", Question: "lunch?",
		Options: []domainChatStorage.PollOption{{Name: "yes"}}, Version: "v3",
	}))
	seedReaction(t, repo, deviceID, chatJID, "POLL-OWN", "628111111111@s.whatsapp.net")

	// Polls never get a messages row. The account's own revoke still counts.
	revoke := revokeEvent(chatJID, "REV-OWN-POLL", "628000000000", time.Now())
	revoke.Info.IsFromMe = true
	revoke.Message.ProtocolMessage.Key.ID = proto.String("POLL-OWN")
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, repo))
	require.NoError(t, repo.CreateMessage(ctx, revoke))

	poll, err := repo.GetPollDefinition(deviceID, chatJID, "POLL-OWN")
	require.NoError(t, err)
	require.Nil(t, poll)
	require.Zero(t, countMessageReactions(t, repo))
	eventOK, ok := whatsapp.MessageEventAuthorized(deviceID, "REV-OWN-POLL")
	require.True(t, ok)
	require.True(t, eventOK)
	authorized, ok := whatsapp.MessageRevokeAuthorized(deviceID, chatJID, "POLL-OWN")
	require.True(t, ok)
	require.True(t, authorized)
}

func TestCreateMessagePeerEditOfUnstoredMessageGrantsNoRevoke(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := authzDevice(t)
	chatJID := "628123456789@s.whatsapp.net"
	require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: deviceID, ChatJID: chatJID, PollMessageID: "POLL-A", Question: "lunch?",
		Options: []domainChatStorage.PollOption{{Name: "yes"}}, Version: "v3",
	}))
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, repo))

	edit := revokeEvent(chatJID, "EDIT-STUB", "628123456789", time.Now())
	edit.Message.ProtocolMessage = &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waCommon.MessageKey{RemoteJID: proto.String(chatJID), ID: proto.String("POLL-A")},
		EditedMessage: &waE2E.Message{Conversation: proto.String("stub")},
	}
	require.NoError(t, repo.CreateMessage(ctx, edit))
	stub, err := repo.GetMessageByIDAndDevice(deviceID, "POLL-A")
	require.NoError(t, err)
	require.Nil(t, stub, "a peer edit of an unstored message created a row")
	editOK, ok := whatsapp.MessageEventAuthorized(deviceID, "EDIT-STUB")
	require.True(t, ok)
	require.False(t, editOK)

	revoke := revokeEvent(chatJID, "REV-STUB", "628123456789", time.Now())
	revoke.Message.ProtocolMessage.Key.ID = proto.String("POLL-A")
	require.NoError(t, repo.CreateMessage(ctx, revoke))
	poll, err := repo.GetPollDefinition(deviceID, chatJID, "POLL-A")
	require.NoError(t, err)
	require.NotNil(t, poll)
	revokeOK, ok := whatsapp.MessageEventAuthorized(deviceID, "REV-STUB")
	require.True(t, ok)
	require.False(t, revokeOK)
}
