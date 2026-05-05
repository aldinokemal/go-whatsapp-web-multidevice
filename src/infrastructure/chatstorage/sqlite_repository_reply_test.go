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
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestCreateMessageStoresReplyContext(t *testing.T) {
	db := openReplyTestSQLiteDB(t)
	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("InitializeSchema() error = %v", err)
	}

	deviceID := "device-1"
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("1234567890", types.DefaultUserServer),
				Sender:   types.NewJID("5551234567", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "wamid-incoming-reply",
			Timestamp: time.Date(2026, time.March, 3, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: protoString("reply text"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID: protoString("wamid-original"),
					QuotedMessage: &waE2E.Message{
						Conversation: protoString("original body"),
					},
				},
			},
		},
	}

	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	if err := repo.CreateMessage(ctx, evt); err != nil {
		t.Fatalf("CreateMessage() error = %v", err)
	}

	stored, err := repo.GetMessageByID("wamid-incoming-reply")
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message, got nil")
	}
	if stored.RepliedToID != "wamid-original" {
		t.Fatalf("expected replied_to_id %q, got %q", "wamid-original", stored.RepliedToID)
	}
	if stored.QuotedBody != "original body" {
		t.Fatalf("expected quoted_body %q, got %q", "original body", stored.QuotedBody)
	}
}

func TestStoreSentMessageWithContextStoresReplyContext(t *testing.T) {
	db := openReplyTestSQLiteDB(t)
	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("InitializeSchema() error = %v", err)
	}

	deviceID := "device-1"
	timestamp := time.Date(2026, time.March, 3, 10, 5, 0, 0, time.UTC)
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: protoString("reply text"),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID: protoString("wamid-original-sent"),
				QuotedMessage: &waE2E.Message{
					Conversation: protoString("sent original body"),
				},
			},
		},
	}

	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	if err := repo.StoreSentMessageWithContext(
		ctx,
		"wamid-sent-reply",
		"1234567890@s.whatsapp.net",
		"5551234567@s.whatsapp.net",
		"reply text",
		timestamp,
		msg,
	); err != nil {
		t.Fatalf("StoreSentMessageWithContext() error = %v", err)
	}

	stored, err := repo.GetMessageByID("wamid-sent-reply")
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message, got nil")
	}
	if !stored.IsFromMe {
		t.Fatal("expected sent message to be marked as from me")
	}
	if stored.RepliedToID != "wamid-original-sent" {
		t.Fatalf("expected replied_to_id %q, got %q", "wamid-original-sent", stored.RepliedToID)
	}
	if stored.QuotedBody != "sent original body" {
		t.Fatalf("expected quoted_body %q, got %q", "sent original body", stored.QuotedBody)
	}
}

func openReplyTestSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "chatstorage-reply-test.db")
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

func TestStoreMessagePersistsReplyFields(t *testing.T) {
	db := openReplyTestSQLiteDB(t)
	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("InitializeSchema() error = %v", err)
	}

	message := &domainChatStorage.Message{
		ID:          "wamid-direct",
		ChatJID:     "1234567890@s.whatsapp.net",
		DeviceID:    "device-1",
		Sender:      "5551234567@s.whatsapp.net",
		Content:     "direct reply",
		Timestamp:   time.Date(2026, time.March, 3, 10, 10, 0, 0, time.UTC),
		IsFromMe:    false,
		RepliedToID: "wamid-original-direct",
		QuotedBody:  "direct quoted body",
	}

	if err := repo.StoreMessage(message); err != nil {
		t.Fatalf("StoreMessage() error = %v", err)
	}

	stored, err := repo.GetMessageByID("wamid-direct")
	if err != nil {
		t.Fatalf("GetMessageByID() error = %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message, got nil")
	}
	if stored.RepliedToID != "wamid-original-direct" {
		t.Fatalf("expected replied_to_id %q, got %q", "wamid-original-direct", stored.RepliedToID)
	}
	if stored.QuotedBody != "direct quoted body" {
		t.Fatalf("expected quoted_body %q, got %q", "direct quoted body", stored.QuotedBody)
	}
}
