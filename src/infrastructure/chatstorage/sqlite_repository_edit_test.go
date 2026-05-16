package chatstorage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestCreateMessageStoresEditHistoryAndUpdatesOriginalMessage(t *testing.T) {
	db := openTestDB(t)
	repo := NewStorageRepository(db).(*SQLiteRepository)

	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}

	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("device-1", nil, nil))

	original := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("123", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "MSG-1",
			Timestamp: time.Date(2026, time.May, 16, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			Conversation: editProtoString("hello"),
		},
	}

	if err := repo.CreateMessage(ctx, original); err != nil {
		t.Fatalf("store original message: %v", err)
	}

	edited := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("123", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "EDIT-1",
			Timestamp: time.Date(2026, time.May, 16, 10, 5, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
				Key: &waCommon.MessageKey{
					ID:       editProtoString("MSG-1"),
					RemoteJID: editProtoString("123@s.whatsapp.net"),
					FromMe:   editProtoBool(false),
				},
				EditedMessage: &waE2E.Message{
					Conversation: editProtoString("hello again"),
				},
			},
		},
	}

	if err := repo.CreateMessage(ctx, edited); err != nil {
		t.Fatalf("store edited message: %v", err)
	}

	got, err := repo.GetMessageByID("MSG-1")
	if err != nil {
		t.Fatalf("load original message: %v", err)
	}
	if got == nil {
		t.Fatal("expected original message to still exist")
	}
	if got.Content != "hello again" {
		t.Fatalf("expected original message content to be updated, got %q", got.Content)
	}

	rows, err := db.Query(`
		SELECT original_message_id, edit_event_id, previous_content, new_content
		FROM message_edits
		WHERE original_message_id = ?
		ORDER BY edited_at ASC
	`, "MSG-1")
	if err != nil {
		t.Fatalf("load edit history: %v", err)
	}
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
		if err := rows.Scan(&edit.OriginalMessageID, &edit.EditEventID, &edit.PreviousContent, &edit.NewContent); err != nil {
			t.Fatalf("scan edit history: %v", err)
		}
		histories = append(histories, edit)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate edit history: %v", err)
	}
	if len(histories) != 1 {
		t.Fatalf("expected 1 edit history row, got %d", len(histories))
	}
	if histories[0].PreviousContent != "hello" {
		t.Fatalf("expected previous content %q, got %q", "hello", histories[0].PreviousContent)
	}
	if histories[0].NewContent != "hello again" {
		t.Fatalf("expected new content %q, got %q", "hello again", histories[0].NewContent)
	}
	if histories[0].EditEventID != "EDIT-1" {
		t.Fatalf("expected edit event id %q, got %q", "EDIT-1", histories[0].EditEventID)
	}
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
