package chatstorage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

func newTestSQLiteRepository(t *testing.T) *SQLiteRepository {
	t.Helper()

	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chatstorage.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}
	return repo
}

func TestSQLiteRepositoryInitializesMessageReactionsSchema(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	var tableName string
	err := repo.db.QueryRow(`
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name = 'message_reactions'
	`).Scan(&tableName)
	if err != nil {
		t.Fatalf("expected message_reactions table to exist: %v", err)
	}
	if tableName != "message_reactions" {
		t.Fatalf("expected message_reactions table, got %q", tableName)
	}
}

func TestSQLiteRepositoryGetsDeviceWebhookConfigByJID(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	webhookURL := "https://device-webhook.example.com"
	if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
		DeviceID:    "session-a",
		DisplayName: "Session A",
		JID:         "628123456789@s.whatsapp.net",
	}); err != nil {
		t.Fatalf("save device record: %v", err)
	}

	if err := repo.SetDeviceWebhookConfig("session-a", &domainChatStorage.DeviceWebhookConfig{
		WebhookURL:                &webhookURL,
		WebhookSecret:             "device-secret",
		WebhookEvents:             "message,message.ack",
		WebhookInsecureSkipVerify: true,
	}); err != nil {
		t.Fatalf("set device webhook config: %v", err)
	}

	record, err := repo.GetDeviceRecordByJID("628123456789@s.whatsapp.net")
	if err != nil {
		t.Fatalf("get device record by jid: %v", err)
	}
	if record == nil {
		t.Fatal("expected device record")
	}
	if record.DeviceID != "session-a" {
		t.Fatalf("expected session-a, got %q", record.DeviceID)
	}
	if record.WebhookURL == nil || *record.WebhookURL != webhookURL {
		t.Fatalf("expected webhook URL %q, got %v", webhookURL, record.WebhookURL)
	}
	if record.WebhookSecret != "device-secret" {
		t.Fatalf("expected device secret, got %q", record.WebhookSecret)
	}
	if record.WebhookEvents != "message,message.ack" {
		t.Fatalf("expected webhook events, got %q", record.WebhookEvents)
	}
	if !record.WebhookInsecureSkipVerify {
		t.Fatal("expected insecure skip verify to be true")
	}
}

func TestSQLiteRepositorySetDeviceWebhookConfigReturnsErrNoRowsForMissingDevice(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	webhookURL := "https://device-webhook.example.com"
	err := repo.SetDeviceWebhookConfig("missing-device", &domainChatStorage.DeviceWebhookConfig{
		WebhookURL: &webhookURL,
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for missing device, got %v", err)
	}
}

func TestSQLiteRepositoryStoresUpdatesRemovesAndHydratesReactions(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	otherDeviceID := "device-b@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC)

	seedChatMessage(t, repo, deviceID, chatJID, "msg-1", "hello reaction", now)
	seedChatMessage(t, repo, otherDeviceID, chatJID, "msg-1", "hello other device", now)

	if err := repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID:  "msg-1",
		ChatJID:    chatJID,
		DeviceID:   deviceID,
		ReactorJID: "628111111111@s.whatsapp.net",
		Emoji:      "\U0001f44d",
		IsFromMe:   false,
		Timestamp:  now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("store reaction: %v", err)
	}
	if err := repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID:  "msg-1",
		ChatJID:    chatJID,
		DeviceID:   otherDeviceID,
		ReactorJID: "628222222222@s.whatsapp.net",
		Emoji:      "\U0001f525",
		IsFromMe:   false,
		Timestamp:  now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("store other device reaction: %v", err)
	}

	messages := getMessagesForTest(t, repo, deviceID, chatJID)
	if got := len(messages[0].Reactions); got != 1 {
		t.Fatalf("expected one device-scoped reaction, got %d", got)
	}
	if got := messages[0].Reactions[0].Emoji; got != "\U0001f44d" {
		t.Fatalf("expected hydrated thumbs-up reaction, got %q", got)
	}

	if err := repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID:  "msg-1",
		ChatJID:    chatJID,
		DeviceID:   deviceID,
		ReactorJID: "628111111111@s.whatsapp.net",
		Emoji:      "\U0001f525",
		IsFromMe:   false,
		Timestamp:  now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("update reaction: %v", err)
	}

	messages = getMessagesForTest(t, repo, deviceID, chatJID)
	if got := len(messages[0].Reactions); got != 1 {
		t.Fatalf("expected one updated reaction, got %d", got)
	}
	if got := messages[0].Reactions[0].Emoji; got != "\U0001f525" {
		t.Fatalf("expected updated fire reaction, got %q", got)
	}

	searchResults, err := repo.SearchMessages(deviceID, chatJID, "reaction", 10)
	if err != nil {
		t.Fatalf("search messages: %v", err)
	}
	if got := len(searchResults); got != 1 {
		t.Fatalf("expected one search result, got %d", got)
	}
	if got := len(searchResults[0].Reactions); got != 1 {
		t.Fatalf("expected search result to hydrate reactions, got %d", got)
	}

	if err := repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID:  "msg-1",
		ChatJID:    chatJID,
		DeviceID:   deviceID,
		ReactorJID: "628111111111@s.whatsapp.net",
		Emoji:      "",
		Timestamp:  now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("remove reaction: %v", err)
	}

	messages = getMessagesForTest(t, repo, deviceID, chatJID)
	if got := len(messages[0].Reactions); got != 0 {
		t.Fatalf("expected reaction removal to clear reactions, got %d", got)
	}
}

func TestSQLiteRepositoryDeletesReactionsWithMessagesAndDevices(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	otherDeviceID := "device-b@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC)

	seedChatMessage(t, repo, deviceID, chatJID, "msg-1", "hello", now)
	seedReaction(t, repo, deviceID, chatJID, "msg-1", "628111111111@s.whatsapp.net")
	seedChatMessage(t, repo, otherDeviceID, chatJID, "msg-1", "hello", now)
	seedReaction(t, repo, otherDeviceID, chatJID, "msg-1", "628222222222@s.whatsapp.net")

	if err := repo.DeleteMessageByDevice(deviceID, "msg-1", chatJID); err != nil {
		t.Fatalf("delete message by device: %v", err)
	}
	if got := countMessageReactions(t, repo); got != 1 {
		t.Fatalf("expected only other device reaction to remain, got %d", got)
	}

	if err := repo.DeleteDeviceData(otherDeviceID); err != nil {
		t.Fatalf("delete device data: %v", err)
	}
	if got := countMessageReactions(t, repo); got != 0 {
		t.Fatalf("expected device cleanup to delete reactions, got %d", got)
	}
}

func TestStoreSentMessageWithContextRequiresDeviceInContext(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "6289605618749@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.May, 22, 10, 0, 0, 0, time.UTC)

	err := repo.StoreSentMessageWithContext(
		context.Background(),
		"msg-sent-1",
		deviceID,
		chatJID,
		"hello from api",
		now,
		nil,
	)
	if err == nil {
		t.Fatal("expected error when storing sent message without device context")
	}
	if !errors.Is(err, domainChatStorage.ErrMissingDeviceContext) {
		t.Fatalf("expected missing device context error, got %v", err)
	}
}

func TestSQLiteRepositoryStoresMessageDirectPath(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.June, 19, 8, 0, 0, 0, time.UTC)
	directPath := "/v/t62.7118-24/media.enc?ccb=11-4"

	if err := repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            chatJID,
		LastMessageTime: now,
	}); err != nil {
		t.Fatalf("store chat: %v", err)
	}
	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:            "msg-media-1",
		ChatJID:       chatJID,
		DeviceID:      deviceID,
		Sender:        "628999999999@s.whatsapp.net",
		Timestamp:     now,
		MediaType:     "image",
		URL:           "https://mmg.whatsapp.net/v/t62.7118-24/media.enc?ccb=11-4",
		DirectPath:    directPath,
		MediaKey:      []byte("media-key"),
		FileSHA256:    []byte("file-sha"),
		FileEncSHA256: []byte("file-enc-sha"),
		FileLength:    1234,
	}); err != nil {
		t.Fatalf("store message: %v", err)
	}

	got, err := repo.GetMessageByID("msg-media-1")
	if err != nil {
		t.Fatalf("get message by id: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored message")
	}
	if got.DirectPath != directPath {
		t.Fatalf("DirectPath = %q, want %q", got.DirectPath, directPath)
	}
}

func TestSQLiteRepositoryStoresBatchMessageDirectPath(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.June, 19, 8, 0, 0, 0, time.UTC)
	directPath := "/v/t62.7118-24/batch-media.enc?ccb=11-4"

	if err := repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            chatJID,
		LastMessageTime: now,
	}); err != nil {
		t.Fatalf("store chat: %v", err)
	}
	if err := repo.StoreMessagesBatch([]*domainChatStorage.Message{{
		ID:            "msg-media-batch-1",
		ChatJID:       chatJID,
		DeviceID:      deviceID,
		Sender:        "628999999999@s.whatsapp.net",
		Timestamp:     now,
		MediaType:     "video",
		URL:           "https://mmg.whatsapp.net/v/t62.7118-24/batch-media.enc?ccb=11-4",
		DirectPath:    directPath,
		MediaKey:      []byte("media-key"),
		FileSHA256:    []byte("file-sha"),
		FileEncSHA256: []byte("file-enc-sha"),
		FileLength:    5678,
	}}); err != nil {
		t.Fatalf("store messages batch: %v", err)
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
		t.Fatalf("expected one message, got %d", len(messages))
	}
	if messages[0].DirectPath != directPath {
		t.Fatalf("DirectPath = %q, want %q", messages[0].DirectPath, directPath)
	}
}

func seedChatMessage(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, messageID, content string, timestamp time.Time) {
	t.Helper()
	if err := repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            chatJID,
		LastMessageTime: timestamp,
	}); err != nil {
		t.Fatalf("store chat: %v", err)
	}
	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    "628999999999@s.whatsapp.net",
		Content:   content,
		Timestamp: timestamp,
	}); err != nil {
		t.Fatalf("store message: %v", err)
	}
}

func seedReaction(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, messageID, reactorJID string) {
	t.Helper()
	if err := repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID:  messageID,
		ChatJID:    chatJID,
		DeviceID:   deviceID,
		ReactorJID: reactorJID,
		Emoji:      "\U0001f44d",
		Timestamp:  time.Date(2026, time.May, 16, 8, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("store reaction: %v", err)
	}
}

func getMessagesForTest(t *testing.T, repo *SQLiteRepository, deviceID, chatJID string) []*domainChatStorage.Message {
	t.Helper()
	messages, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: deviceID,
		ChatJID:  chatJID,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	return messages
}

func countMessageReactions(t *testing.T, repo *SQLiteRepository) int {
	t.Helper()
	var count int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM message_reactions`).Scan(&count); err != nil {
		t.Fatalf("count message reactions: %v", err)
	}
	return count
}
