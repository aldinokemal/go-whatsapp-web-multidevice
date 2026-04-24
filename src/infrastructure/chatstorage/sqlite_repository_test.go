package chatstorage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow/types"
)

func newTestSQLiteRepository(t *testing.T) *SQLiteRepository {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "chatstorage.db")
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return repo
}

func TestSQLiteRepository_LoadsReactionsIntoMessages(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	chatJID := "628123456789@s.whatsapp.net"
	deviceID := "device-1"
	messageID := "MSG-1"

	if err := repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            "Test Chat",
		LastMessageTime: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("store chat: %v", err)
	}

	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    "628123456789@s.whatsapp.net",
		Content:   "Hello",
		Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
		IsFromMe:  false,
	}); err != nil {
		t.Fatalf("store message: %v", err)
	}

	reactionTimestamp := time.Date(2026, time.April, 24, 10, 1, 0, 0, time.UTC)
	if err := repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID:  messageID,
		ChatJID:    chatJID,
		DeviceID:   deviceID,
		ReactorJID: "628999000111@s.whatsapp.net",
		Emoji:      "👍",
		IsFromMe:   false,
		Timestamp:  reactionTimestamp,
	}); err != nil {
		t.Fatalf("store reaction: %v", err)
	}

	messages, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: deviceID,
		ChatJID:  chatJID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if len(messages[0].Reactions) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(messages[0].Reactions))
	}

	reaction := messages[0].Reactions[0]
	if reaction.MessageID != messageID {
		t.Fatalf("expected reaction message id %q, got %q", messageID, reaction.MessageID)
	}
	if reaction.ReactorJID != "628999000111@s.whatsapp.net" {
		t.Fatalf("expected reactor jid, got %q", reaction.ReactorJID)
	}
	if reaction.Emoji != "👍" {
		t.Fatalf("expected emoji 👍, got %q", reaction.Emoji)
	}
}

func TestSQLiteRepository_EmptyEmojiRemovesReaction(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	chatJID := "628123456789@s.whatsapp.net"
	deviceID := "device-1"
	messageID := "MSG-2"

	if err := repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            "Test Chat",
		LastMessageTime: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("store chat: %v", err)
	}
	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    "628123456789@s.whatsapp.net",
		Content:   "Hello",
		Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("store message: %v", err)
	}

	reaction := &domainChatStorage.Reaction{
		MessageID:  messageID,
		ChatJID:    chatJID,
		DeviceID:   deviceID,
		ReactorJID: "628999000111@s.whatsapp.net",
		Emoji:      "🔥",
		Timestamp:  time.Date(2026, time.April, 24, 10, 1, 0, 0, time.UTC),
	}
	if err := repo.StoreReaction(reaction); err != nil {
		t.Fatalf("store reaction: %v", err)
	}

	reaction.Emoji = ""
	if err := repo.StoreReaction(reaction); err != nil {
		t.Fatalf("remove reaction: %v", err)
	}

	messages, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: deviceID,
		ChatJID:  chatJID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if len(messages[0].Reactions) != 0 {
		t.Fatalf("expected 0 reactions after removal, got %d", len(messages[0].Reactions))
	}
}

func TestSQLiteRepository_DeleteOperationsRemoveReactions(t *testing.T) {
	t.Run("delete message", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		seedReactionFixture(t, repo, "device-1", "chat-1", "MSG-3")

		if err := repo.DeleteMessageByDevice("device-1", "MSG-3", "chat-1"); err != nil {
			t.Fatalf("delete message: %v", err)
		}

		assertReactionCount(t, repo, 0)
	})

	t.Run("delete chat", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		seedReactionFixture(t, repo, "device-1", "chat-2", "MSG-4")

		if err := repo.DeleteChatByDevice("device-1", "chat-2"); err != nil {
			t.Fatalf("delete chat: %v", err)
		}

		assertReactionCount(t, repo, 0)
	})

	t.Run("delete device data", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		seedReactionFixture(t, repo, "device-1", "chat-3", "MSG-5")

		if err := repo.DeleteDeviceData("device-1"); err != nil {
			t.Fatalf("delete device data: %v", err)
		}

		assertReactionCount(t, repo, 0)
	})
}

func seedReactionFixture(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, messageID string) {
	t.Helper()

	if err := repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            "Test Chat",
		LastMessageTime: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("store chat: %v", err)
	}
	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    types.NewJID("628123456789", types.DefaultUserServer).String(),
		Content:   "Hello",
		Timestamp: time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("store message: %v", err)
	}
	if err := repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID:  messageID,
		ChatJID:    chatJID,
		DeviceID:   deviceID,
		ReactorJID: "628999000111@s.whatsapp.net",
		Emoji:      "👍",
		Timestamp:  time.Date(2026, time.April, 24, 10, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("store reaction: %v", err)
	}
}

func assertReactionCount(t *testing.T, repo *SQLiteRepository, want int) {
	t.Helper()

	var count int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM message_reactions`).Scan(&count); err != nil {
		t.Fatalf("count reactions: %v", err)
	}
	if count != want {
		t.Fatalf("expected %d reactions, got %d", want, count)
	}
}
