package chatstorage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestCreateMessageMessageEditUpdatesOriginalRow(t *testing.T) {
	db := openTestSQLiteDB(t)
	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("InitializeSchema() error = %v", err)
	}

	deviceID := "device-1"
	chatJID := "1234567890@s.whatsapp.net"
	originalID := "wamid-original"

	originalTimestamp := time.Date(2026, time.March, 3, 9, 30, 0, 0, time.UTC)
	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:        originalID,
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    chatJID,
		Content:   "old content",
		Timestamp: originalTimestamp,
		IsFromMe:  true,
	}); err != nil {
		t.Fatalf("StoreMessage() error = %v", err)
	}

	editTimestamp := time.Date(2026, time.March, 3, 9, 35, 0, 0, time.UTC)
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("1234567890", types.DefaultUserServer),
				Sender:   types.NewJID("1234567890", types.DefaultUserServer),
				IsFromMe: true,
			},
			ID:        "wamid-edit-event",
			Timestamp: editTimestamp,
		},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: protoProtocolMessageType(waE2E.ProtocolMessage_MESSAGE_EDIT),
				Key: &waCommon.MessageKey{
					ID:        protoString(originalID),
					RemoteJID: protoString(chatJID),
					FromMe:    protoBool(true),
				},
				EditedMessage: &waE2E.Message{
					Conversation: protoString("new content"),
				},
			},
		},
	}

	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	if err := repo.CreateMessage(ctx, evt); err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}

	stored, err := repo.GetMessageByID(originalID)
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if stored == nil {
		t.Fatal("expected original message to remain in storage")
	}
	if stored.Content != "new content" {
		t.Fatalf("expected updated content %q, got %q", "new content", stored.Content)
	}

	var messageCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE chat_jid = ? AND device_id = ?`,
		chatJID, deviceID,
	).Scan(&messageCount); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if messageCount != 1 {
		t.Fatalf("expected 1 stored row for the edited message, got %d", messageCount)
	}
}

func openTestSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "chatstorage-test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func protoString(value string) *string {
	return &value
}

func protoBool(value bool) *bool {
	return &value
}

func protoProtocolMessageType(value waE2E.ProtocolMessage_Type) *waE2E.ProtocolMessage_Type {
	return &value
}
