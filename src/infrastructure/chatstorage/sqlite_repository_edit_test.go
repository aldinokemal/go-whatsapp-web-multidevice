package chatstorage

import (
	"context"
	"database/sql"
	"testing"
	"time"

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
