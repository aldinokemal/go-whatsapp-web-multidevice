package usecase

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
)

func TestPersistEditedMessageUpdatesStoredRow(t *testing.T) {
	db := openUsecaseTestSQLiteDB(t)
	repo := chatstorage.NewStorageRepository(db)
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("InitializeSchema() error = %v", err)
	}

	deviceID := "device-1"
	chatJID := "1234567890@s.whatsapp.net"
	messageID := "wamid-original"

	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    chatJID,
		Content:   "old content",
		Timestamp: time.Date(2026, time.March, 3, 9, 30, 0, 0, time.UTC),
		IsFromMe:  true,
	}); err != nil {
		t.Fatalf("StoreMessage() error = %v", err)
	}

	service := serviceMessage{chatStorageRepo: repo}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	if err := service.persistEditedMessage(ctx, messageID, chatJID, "new content", time.Date(2026, time.March, 3, 9, 35, 0, 0, time.UTC)); err != nil {
		t.Fatalf("persistEditedMessage() error = %v", err)
	}

	stored, err := repo.GetMessageByID(messageID)
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message to exist")
	}
	if stored.Content != "new content" {
		t.Fatalf("expected updated content %q, got %q", "new content", stored.Content)
	}
}

func openUsecaseTestSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "usecase-test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}
