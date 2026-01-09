package chatstorage

import (
	"database/sql"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) (*sql.DB, *SQLiteRepository) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	return db, repo
}

func TestGetMessages_RequiresDeviceID(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test that GetMessages fails without DeviceID
	_, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		ChatJID: "test@s.whatsapp.net",
	})
	if err == nil {
		t.Error("Expected error when DeviceID is missing, got nil")
	}
	if err.Error() != "device_id is required in MessageFilter for device-scoped queries" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestGetMessages_AllowsCrossDeviceWithFlag(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test that GetMessages works with AllowCrossDevice flag
	_, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		ChatJID:          "test@s.whatsapp.net",
		AllowCrossDevice: true,
	})
	if err != nil {
		t.Errorf("Expected no error with AllowCrossDevice=true, got: %v", err)
	}
}

func TestGetMessages_NilFilterReturnsError(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test that GetMessages fails with nil filter
	_, err := repo.GetMessages(nil)
	if err == nil {
		t.Error("Expected error when filter is nil, got nil")
	}
	if err.Error() != "filter is required" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestGetChats_RequiresDeviceID(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test that GetChats fails without DeviceID
	_, err := repo.GetChats(&domainChatStorage.ChatFilter{})
	if err == nil {
		t.Error("Expected error when DeviceID is missing, got nil")
	}
	if err.Error() != "device_id is required in ChatFilter for device-scoped queries" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestGetChats_AllowsCrossDeviceWithFlag(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test that GetChats works with AllowCrossDevice flag
	_, err := repo.GetChats(&domainChatStorage.ChatFilter{
		AllowCrossDevice: true,
	})
	if err != nil {
		t.Errorf("Expected no error with AllowCrossDevice=true, got: %v", err)
	}
}

func TestSearchMessages_RequiresDeviceID(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test that SearchMessages fails without DeviceID
	_, err := repo.SearchMessages("", "test@s.whatsapp.net", "hello", 10)
	if err == nil {
		t.Error("Expected error when deviceID is empty, got nil")
	}
	if err.Error() != "device_id is required for SearchMessages" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestDeviceIsolation_MessagesNotSharedAcrossDevices(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Create messages for two different devices
	device1 := "device1@s.whatsapp.net"
	device2 := "device2@s.whatsapp.net"
	chatJID := "contact@s.whatsapp.net"

	// Store chat for device1
	chat1 := &domainChatStorage.Chat{
		DeviceID:        device1,
		JID:             chatJID,
		Name:            "Contact",
		LastMessageTime: time.Now(),
	}
	if err := repo.StoreChat(chat1); err != nil {
		t.Fatalf("Failed to store chat for device1: %v", err)
	}

	// Store chat for device2
	chat2 := &domainChatStorage.Chat{
		DeviceID:        device2,
		JID:             chatJID,
		Name:            "Contact",
		LastMessageTime: time.Now(),
	}
	if err := repo.StoreChat(chat2); err != nil {
		t.Fatalf("Failed to store chat for device2: %v", err)
	}

	// Store message for device1
	msg1 := &domainChatStorage.Message{
		ID:        "msg1",
		ChatJID:   chatJID,
		DeviceID:  device1,
		Sender:    "sender@s.whatsapp.net",
		Content:   "Message for device 1",
		Timestamp: time.Now(),
	}
	if err := repo.StoreMessage(msg1); err != nil {
		t.Fatalf("Failed to store message for device1: %v", err)
	}

	// Store message for device2
	msg2 := &domainChatStorage.Message{
		ID:        "msg2",
		ChatJID:   chatJID,
		DeviceID:  device2,
		Sender:    "sender@s.whatsapp.net",
		Content:   "Message for device 2",
		Timestamp: time.Now(),
	}
	if err := repo.StoreMessage(msg2); err != nil {
		t.Fatalf("Failed to store message for device2: %v", err)
	}

	// Query messages for device1 - should only get msg1
	messages, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: device1,
		ChatJID:  chatJID,
	})
	if err != nil {
		t.Fatalf("Failed to get messages for device1: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message for device1, got %d", len(messages))
	}
	if len(messages) > 0 && messages[0].ID != "msg1" {
		t.Errorf("Expected message ID 'msg1', got '%s'", messages[0].ID)
	}

	// Query messages for device2 - should only get msg2
	messages, err = repo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: device2,
		ChatJID:  chatJID,
	})
	if err != nil {
		t.Fatalf("Failed to get messages for device2: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message for device2, got %d", len(messages))
	}
	if len(messages) > 0 && messages[0].ID != "msg2" {
		t.Errorf("Expected message ID 'msg2', got '%s'", messages[0].ID)
	}
}

func TestDeviceIsolation_ChatsNotSharedAcrossDevices(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	device1 := "device1@s.whatsapp.net"
	device2 := "device2@s.whatsapp.net"

	// Store chat only for device1
	chat1 := &domainChatStorage.Chat{
		DeviceID:        device1,
		JID:             "contact1@s.whatsapp.net",
		Name:            "Contact 1",
		LastMessageTime: time.Now(),
	}
	if err := repo.StoreChat(chat1); err != nil {
		t.Fatalf("Failed to store chat for device1: %v", err)
	}

	// Store chat only for device2
	chat2 := &domainChatStorage.Chat{
		DeviceID:        device2,
		JID:             "contact2@s.whatsapp.net",
		Name:            "Contact 2",
		LastMessageTime: time.Now(),
	}
	if err := repo.StoreChat(chat2); err != nil {
		t.Fatalf("Failed to store chat for device2: %v", err)
	}

	// Query chats for device1 - should only get contact1
	chats, err := repo.GetChats(&domainChatStorage.ChatFilter{
		DeviceID: device1,
	})
	if err != nil {
		t.Fatalf("Failed to get chats for device1: %v", err)
	}

	if len(chats) != 1 {
		t.Errorf("Expected 1 chat for device1, got %d", len(chats))
	}
	if len(chats) > 0 && chats[0].JID != "contact1@s.whatsapp.net" {
		t.Errorf("Expected chat JID 'contact1@s.whatsapp.net', got '%s'", chats[0].JID)
	}

	// Query chats for device2 - should only get contact2
	chats, err = repo.GetChats(&domainChatStorage.ChatFilter{
		DeviceID: device2,
	})
	if err != nil {
		t.Fatalf("Failed to get chats for device2: %v", err)
	}

	if len(chats) != 1 {
		t.Errorf("Expected 1 chat for device2, got %d", len(chats))
	}
	if len(chats) > 0 && chats[0].JID != "contact2@s.whatsapp.net" {
		t.Errorf("Expected chat JID 'contact2@s.whatsapp.net', got '%s'", chats[0].JID)
	}
}

func TestDeprecatedMethods_ReturnErrors(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test GetChat
	_, err := repo.GetChat("test@s.whatsapp.net")
	if err == nil {
		t.Error("Expected error from deprecated GetChat, got nil")
	}

	// Test GetMessageByID
	_, err = repo.GetMessageByID("test@s.whatsapp.net", "msg1")
	if err == nil {
		t.Error("Expected error from deprecated GetMessageByID, got nil")
	}

	// Test DeleteChat
	err = repo.DeleteChat("test@s.whatsapp.net")
	if err == nil {
		t.Error("Expected error from deprecated DeleteChat, got nil")
	}

	// Test DeleteMessage
	err = repo.DeleteMessage("msg1", "test@s.whatsapp.net")
	if err == nil {
		t.Error("Expected error from deprecated DeleteMessage, got nil")
	}
}

func TestSearchMessages_DeviceIsolation(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	device1 := "device1@s.whatsapp.net"
	device2 := "device2@s.whatsapp.net"
	chatJID := "contact@s.whatsapp.net"

	// Store chats
	chat1 := &domainChatStorage.Chat{
		DeviceID:        device1,
		JID:             chatJID,
		Name:            "Contact",
		LastMessageTime: time.Now(),
	}
	repo.StoreChat(chat1)

	chat2 := &domainChatStorage.Chat{
		DeviceID:        device2,
		JID:             chatJID,
		Name:            "Contact",
		LastMessageTime: time.Now(),
	}
	repo.StoreChat(chat2)

	// Store message with searchable content for device1
	msg1 := &domainChatStorage.Message{
		ID:        "msg1",
		ChatJID:   chatJID,
		DeviceID:  device1,
		Sender:    "sender@s.whatsapp.net",
		Content:   "Hello world from device 1",
		Timestamp: time.Now(),
	}
	repo.StoreMessage(msg1)

	// Store message with searchable content for device2
	msg2 := &domainChatStorage.Message{
		ID:        "msg2",
		ChatJID:   chatJID,
		DeviceID:  device2,
		Sender:    "sender@s.whatsapp.net",
		Content:   "Hello world from device 2",
		Timestamp: time.Now(),
	}
	repo.StoreMessage(msg2)

	// Search for device1 - should only find msg1
	results, err := repo.SearchMessages(device1, chatJID, "Hello", 10)
	if err != nil {
		t.Fatalf("Failed to search messages for device1: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result for device1, got %d", len(results))
	}
	if len(results) > 0 && results[0].ID != "msg1" {
		t.Errorf("Expected message ID 'msg1', got '%s'", results[0].ID)
	}

	// Search for device2 - should only find msg2
	results, err = repo.SearchMessages(device2, chatJID, "Hello", 10)
	if err != nil {
		t.Fatalf("Failed to search messages for device2: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result for device2, got %d", len(results))
	}
	if len(results) > 0 && results[0].ID != "msg2" {
		t.Errorf("Expected message ID 'msg2', got '%s'", results[0].ID)
	}
}

func TestGetMessageByIDByDevice_DeviceIsolation(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	device1 := "device1@s.whatsapp.net"
	device2 := "device2@s.whatsapp.net"
	chatJID := "contact@s.whatsapp.net"

	// Store chats
	chat1 := &domainChatStorage.Chat{
		DeviceID:        device1,
		JID:             chatJID,
		Name:            "Contact",
		LastMessageTime: time.Now(),
	}
	repo.StoreChat(chat1)

	// Store message for device1
	msg1 := &domainChatStorage.Message{
		ID:        "msg1",
		ChatJID:   chatJID,
		DeviceID:  device1,
		Sender:    "sender@s.whatsapp.net",
		Content:   "Message for device 1",
		Timestamp: time.Now(),
	}
	repo.StoreMessage(msg1)

	// Device1 should be able to get the message
	message, err := repo.GetMessageByIDByDevice(device1, chatJID, "msg1")
	if err != nil {
		t.Fatalf("Failed to get message for device1: %v", err)
	}
	if message == nil {
		t.Error("Expected message for device1, got nil")
	}

	// Device2 should NOT be able to get the message (cross-device isolation)
	message, err = repo.GetMessageByIDByDevice(device2, chatJID, "msg1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if message != nil {
		t.Error("Expected nil for device2 (message belongs to device1), got message")
	}
}
