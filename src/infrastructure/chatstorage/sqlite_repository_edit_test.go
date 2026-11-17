package chatstorage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type SQLiteRepositoryEditTestSuite struct {
	suite.Suite
	db   *sql.DB
	repo *SQLiteRepository
	ctx  context.Context
}

func (suite *SQLiteRepositoryEditTestSuite) SetupTest() {
	suite.db = openTestDB(suite.T())
	suite.repo = NewStorageRepository(suite.db).(*SQLiteRepository)
	require.NoError(suite.T(), suite.repo.InitializeSchema())
	suite.ctx = whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("device-1", nil, nil))
}

func (suite *SQLiteRepositoryEditTestSuite) TestCreateMessageStoresEditHistoryAndUpdatesOriginalMessage() {
	tests := []struct {
		name                 string
		originalMessageID    string
		originalContent      string
		editedEventID        string
		editedContent        string
		editedTimestampDelta time.Duration
	}{
		{
			name:                 "persists edit history and updates the original row",
			originalMessageID:    "MSG-1",
			originalContent:      "hello",
			editedEventID:        "EDIT-1",
			editedContent:        "hello again",
			editedTimestampDelta: 5 * time.Minute,
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			originalTimestamp := time.Date(2026, time.May, 16, 10, 0, 0, 0, time.UTC)
			editedTimestamp := originalTimestamp.Add(tc.editedTimestampDelta)

			original := &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat:     types.NewJID("123", types.DefaultUserServer),
						Sender:   types.NewJID("123", types.DefaultUserServer),
						IsFromMe: false,
					},
					ID:        tc.originalMessageID,
					Timestamp: originalTimestamp,
				},
				Message: &waE2E.Message{
					Conversation: editProtoString(tc.originalContent),
				},
			}

			require.NoError(t, suite.repo.CreateMessage(suite.ctx, original))

			edited := &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat:     types.NewJID("123", types.DefaultUserServer),
						Sender:   types.NewJID("123", types.DefaultUserServer),
						IsFromMe: false,
					},
					ID:        tc.editedEventID,
					Timestamp: editedTimestamp,
				},
				Message: &waE2E.Message{
					ProtocolMessage: &waE2E.ProtocolMessage{
						Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
						Key: &waCommon.MessageKey{
							ID:        editProtoString(tc.originalMessageID),
							RemoteJID: editProtoString("123@s.whatsapp.net"),
							FromMe:    editProtoBool(false),
						},
						EditedMessage: &waE2E.Message{
							Conversation: editProtoString(tc.editedContent),
						},
					},
				},
			}

			require.NoError(t, suite.repo.CreateMessage(suite.ctx, edited))

			got, err := suite.repo.GetMessageByID(tc.originalMessageID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.editedContent, got.Content)

			rows, err := suite.db.Query(`
				SELECT original_message_id, edit_event_id, previous_content, new_content
				FROM message_edits
				WHERE original_message_id = ?
				ORDER BY edited_at ASC
			`, tc.originalMessageID)
			require.NoError(t, err)
			defer rows.Close()

			type messageEditRow struct {
				OriginalMessageID string
				EditEventID       string
				PreviousContent   string
				NewContent        string
			}

			var histories []messageEditRow
			for rows.Next() {
				var edit messageEditRow
				require.NoError(t, rows.Scan(&edit.OriginalMessageID, &edit.EditEventID, &edit.PreviousContent, &edit.NewContent))
				histories = append(histories, edit)
			}
			require.NoError(t, rows.Err())
			require.Len(t, histories, 1)

			assert.Equal(t, tc.originalContent, histories[0].PreviousContent)
			assert.Equal(t, tc.editedContent, histories[0].NewContent)
			assert.Equal(t, tc.editedEventID, histories[0].EditEventID)
		})
	}
}

func TestSQLiteRepositoryEditTestSuite(t *testing.T) {
	suite.Run(t, new(SQLiteRepositoryEditTestSuite))
}

func TestCreateMessageEditRejectsOtherAuthor(t *testing.T) {
	db := openTestDB(t)
	repo := NewStorageRepository(db).(*SQLiteRepository)
	require.NoError(t, repo.InitializeSchema())
	deviceID := authzDevice(t)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	original := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("123", types.DefaultUserServer),
				Sender: types.NewJID("123", types.DefaultUserServer),
			},
			ID:        "MSG-1",
			Timestamp: time.Date(2026, time.May, 16, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{Conversation: editProtoString("hello")},
	}
	require.NoError(t, repo.CreateMessage(ctx, original))

	edited := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("123", types.DefaultUserServer),
				Sender: types.NewJID("999", types.DefaultUserServer),
			},
			ID:        "EDIT-FOREIGN",
			Timestamp: time.Date(2026, time.May, 16, 10, 5, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key: &waCommon.MessageKey{
				ID:        editProtoString("MSG-1"),
				RemoteJID: editProtoString("123@s.whatsapp.net"),
			},
			EditedMessage: &waE2E.Message{Conversation: editProtoString("pwned")},
		}},
	}
	require.NoError(t, repo.CreateMessage(ctx, edited))

	got, err := repo.GetMessageByID("MSG-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "hello", got.Content)
	authorized, ok := whatsapp.MessageEventAuthorized(deviceID, "EDIT-FOREIGN")
	require.True(t, ok)
	require.False(t, authorized)

	// whatsmeow strips each wrapper once, so a doubly wrapped edit still
	// arrives wrapped. It is the same edit and needs the same author check.
	wrapped := &events.Message{Info: edited.Info, Message: &waE2E.Message{
		EphemeralMessage: &waE2E.FutureProofMessage{Message: edited.Message},
	}}
	wrapped.Info.ID = "EDIT-FOREIGN-WRAPPED"
	require.NoError(t, repo.CreateMessage(ctx, wrapped))
	got, err = repo.GetMessageByID("MSG-1")
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Content)
	authorized, ok = whatsapp.MessageEventAuthorized(deviceID, "EDIT-FOREIGN-WRAPPED")
	require.True(t, ok)
	require.False(t, authorized)

	var edits int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM message_edits WHERE original_message_id = ?`, "MSG-1").Scan(&edits))
	assert.Zero(t, edits)
}

func TestCreateMessageEditAllowsOwnAccountAcrossSenderStrings(t *testing.T) {
	db := openTestDB(t)
	repo := NewStorageRepository(db).(*SQLiteRepository)
	require.NoError(t, repo.InitializeSchema())
	deviceID := authzDevice(t)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID:        "MSG-OWN",
		ChatJID:   "628123456789@s.whatsapp.net",
		DeviceID:  deviceID,
		Sender:    "628000000000@s.whatsapp.net",
		Content:   "original",
		Timestamp: time.Date(2026, time.May, 16, 10, 0, 0, 0, time.UTC),
		IsFromMe:  true,
	}))

	edited := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("628123456789", types.DefaultUserServer),
				Sender:   types.NewJID("628999999999", types.DefaultUserServer),
				IsFromMe: true,
			},
			ID:        "EDIT-OWN",
			Timestamp: time.Date(2026, time.May, 16, 10, 5, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key: &waCommon.MessageKey{
				ID:        editProtoString("MSG-OWN"),
				RemoteJID: editProtoString("628123456789@s.whatsapp.net"),
				FromMe:    editProtoBool(true),
			},
			EditedMessage: &waE2E.Message{Conversation: editProtoString("edited by us")},
		}},
	}
	require.NoError(t, repo.CreateMessage(ctx, edited))

	got, err := repo.GetMessageByIDAndDevice(deviceID, "MSG-OWN")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "edited by us", got.Content)
	authorized, ok := whatsapp.MessageEventAuthorized(deviceID, "EDIT-OWN")
	require.True(t, ok)
	require.True(t, authorized)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func editProtoString(value string) *string {
	return &value
}

func editProtoBool(value bool) *bool {
	return &value
}
