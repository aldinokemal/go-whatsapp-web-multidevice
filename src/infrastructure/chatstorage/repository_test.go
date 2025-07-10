package chatstorage

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/suite"
)

type ChatStorageTestSuite struct {
	suite.Suite
	storage *Storage
	repo    Repository
	db      *sql.DB
}

func (suite *ChatStorageTestSuite) SetupTest() {
	// Create temporary database for testing
	tempDB := ":memory:"

	db, err := sql.Open("sqlite3", tempDB)
	suite.Require().NoError(err)

	// Create repository
	repo := NewSQLiteRepository(db)

	// Create storage with in-memory config
	config := &Config{
		DatabasePath:      tempDB,
		EnableForeignKeys: true,
		EnableWAL:         false, // WAL mode doesn't work with :memory:
	}

	storage := &Storage{
		db:     db,
		config: config,
		repo:   repo,
	}

	// Initialize schema manually for testing
	suite.initTestSchema(db)

	suite.storage = storage
	suite.repo = repo
	suite.db = db
}

func (suite *ChatStorageTestSuite) TearDownTest() {
	if suite.db != nil {
		suite.db.Close()
	}
}

func (suite *ChatStorageTestSuite) initTestSchema(db *sql.DB) {
	// Create chats table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			last_message_time TIMESTAMP NOT NULL,
			ephemeral_expiration INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	suite.Require().NoError(err)

	// Create messages table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT NOT NULL,
			chat_jid TEXT NOT NULL,
			sender TEXT NOT NULL,
			content TEXT,
			timestamp TIMESTAMP NOT NULL,
			is_from_me BOOLEAN DEFAULT FALSE,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid) ON DELETE CASCADE
		)
	`)
	suite.Require().NoError(err)
}

func (suite *ChatStorageTestSuite) createTestData() {
	// Create test chats
	chat1 := &Chat{
		JID:             "1234567890@s.whatsapp.net",
		Name:            "Test Contact 1",
		LastMessageTime: time.Now(),
	}
	chat2 := &Chat{
		JID:             "0987654321@s.whatsapp.net",
		Name:            "Test Contact 2",
		LastMessageTime: time.Now(),
	}
	chat3 := &Chat{
		JID:             "1111111111@g.us",
		Name:            "Test Group",
		LastMessageTime: time.Now(),
	}

	err := suite.repo.StoreChat(chat1)
	suite.Require().NoError(err)
	err = suite.repo.StoreChat(chat2)
	suite.Require().NoError(err)
	err = suite.repo.StoreChat(chat3)
	suite.Require().NoError(err)

	// Create test messages
	messages := []*Message{
		{
			ID:        "msg1",
			ChatJID:   "1234567890@s.whatsapp.net",
			Sender:    "1234567890",
			Content:   "Hello World",
			Timestamp: time.Now(),
			IsFromMe:  false,
		},
		{
			ID:        "msg2",
			ChatJID:   "1234567890@s.whatsapp.net",
			Sender:    "me",
			Content:   "Hi there!",
			Timestamp: time.Now(),
			IsFromMe:  true,
		},
		{
			ID:        "msg3",
			ChatJID:   "0987654321@s.whatsapp.net",
			Sender:    "0987654321",
			Content:   "How are you?",
			Timestamp: time.Now(),
			IsFromMe:  false,
		},
		{
			ID:        "msg4",
			ChatJID:   "1111111111@g.us",
			Sender:    "1234567890",
			Content:   "Group message",
			Timestamp: time.Now(),
			IsFromMe:  false,
		},
		{
			ID:        "msg5",
			ChatJID:   "1111111111@g.us",
			Sender:    "me",
			Content:   "My group reply",
			Timestamp: time.Now(),
			IsFromMe:  true,
		},
	}

	err = suite.repo.StoreMessagesBatch(messages)
	suite.Require().NoError(err)
}

func (suite *ChatStorageTestSuite) TestTruncateAllMessages() {
	// Create test data
	suite.createTestData()

	// Verify data exists
	messageCount, err := suite.repo.GetTotalMessageCount()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(5), messageCount)

	// Truncate all messages
	err = suite.repo.TruncateAllMessages()
	suite.Require().NoError(err)

	// Verify messages are gone
	messageCount, err = suite.repo.GetTotalMessageCount()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(0), messageCount)

	// Verify chats still exist
	chats, err := suite.repo.GetChats(&ChatFilter{Limit: 100})
	suite.Require().NoError(err)
	suite.Assert().Equal(3, len(chats))
}

func (suite *ChatStorageTestSuite) TestTruncateAllChats() {
	// Create test data
	suite.createTestData()

	// Verify data exists
	chats, err := suite.repo.GetChats(&ChatFilter{Limit: 100})
	suite.Require().NoError(err)
	suite.Assert().Equal(3, len(chats))

	// Truncate all chats (this should also delete messages due to foreign key constraint)
	err = suite.repo.TruncateAllChats()
	suite.Require().NoError(err)

	// Verify chats are gone
	chats, err = suite.repo.GetChats(&ChatFilter{Limit: 100})
	suite.Require().NoError(err)
	suite.Assert().Equal(0, len(chats))

	// Verify messages are also gone
	messageCount, err := suite.repo.GetTotalMessageCount()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(0), messageCount)
}

func (suite *ChatStorageTestSuite) TestTruncateAllData() {
	// Create test data
	suite.createTestData()

	// Verify data exists
	messageCount, err := suite.repo.GetTotalMessageCount()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(5), messageCount)

	chats, err := suite.repo.GetChats(&ChatFilter{Limit: 100})
	suite.Require().NoError(err)
	suite.Assert().Equal(3, len(chats))

	// Truncate all data
	err = suite.repo.TruncateAllData()
	suite.Require().NoError(err)

	// Verify everything is gone
	messageCount, err = suite.repo.GetTotalMessageCount()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(0), messageCount)

	chats, err = suite.repo.GetChats(&ChatFilter{Limit: 100})
	suite.Require().NoError(err)
	suite.Assert().Equal(0, len(chats))
}

func (suite *ChatStorageTestSuite) TestStorageGetStorageStatistics() {
	// Create test data
	suite.createTestData()

	// Get statistics
	chatCount, messageCount, err := suite.storage.GetStorageStatistics()
	suite.Require().NoError(err)

	suite.Assert().Equal(int64(3), chatCount)
	suite.Assert().Equal(int64(5), messageCount)
}

func (suite *ChatStorageTestSuite) TestStorageTruncateAllDataWithLogging() {
	// Create test data
	suite.createTestData()

	// Verify data exists before truncation
	chatCount, messageCount, err := suite.storage.GetStorageStatistics()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(3), chatCount)
	suite.Assert().Equal(int64(5), messageCount)

	// Truncate with logging
	err = suite.storage.TruncateAllDataWithLogging("TEST")
	suite.Require().NoError(err)

	// Verify data is gone after truncation
	chatCount, messageCount, err = suite.storage.GetStorageStatistics()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(0), chatCount)
	suite.Assert().Equal(int64(0), messageCount)
}

func (suite *ChatStorageTestSuite) TestTruncateEmptyDatabase() {
	// Test truncating when database is already empty
	err := suite.repo.TruncateAllData()
	suite.Require().NoError(err)

	// Verify counts are still zero
	messageCount, err := suite.repo.GetTotalMessageCount()
	suite.Require().NoError(err)
	suite.Assert().Equal(int64(0), messageCount)

	chats, err := suite.repo.GetChats(&ChatFilter{Limit: 100})
	suite.Require().NoError(err)
	suite.Assert().Equal(0, len(chats))
}

func TestChatStorageTestSuite(t *testing.T) {
	suite.Run(t, new(ChatStorageTestSuite))
}

// TestGetMessageByID tests the new optimized GetMessageByID method
func (suite *ChatStorageTestSuite) TestGetMessageByID() {
	// Create test data
	suite.createTestData()

	// Test finding existing message by ID only
	msg, err := suite.repo.GetMessageByID("msg1")
	suite.NoError(err)
	suite.NotNil(msg)
	suite.Equal("msg1", msg.ID)
	suite.Equal("1234567890@s.whatsapp.net", msg.ChatJID)
	suite.Equal("Hello World", msg.Content)

	// Test finding non-existent message
	msg, err = suite.repo.GetMessageByID("nonexistent")
	suite.NoError(err)
	suite.Nil(msg)

	// Test finding message from different chat
	msg, err = suite.repo.GetMessageByID("msg3")
	suite.NoError(err)
	suite.NotNil(msg)
	suite.Equal("msg3", msg.ID)
	suite.Equal("0987654321@s.whatsapp.net", msg.ChatJID)
	suite.Equal("How are you?", msg.Content)
}

// TestSearchMessages tests the new database-level search functionality
func (suite *ChatStorageTestSuite) TestSearchMessages() {
	// Create test data with varied content for search testing
	suite.createTestData()

	// Add more messages with specific content for search testing
	additionalMessages := []*Message{
		{
			ID:        "msg6",
			ChatJID:   "1234567890@s.whatsapp.net",
			Sender:    "1234567890",
			Content:   "This is a test message with HELLO in caps",
			Timestamp: time.Now(),
			IsFromMe:  false,
		},
		{
			ID:        "msg7",
			ChatJID:   "1234567890@s.whatsapp.net",
			Sender:    "me",
			Content:   "Another message without the target word",
			Timestamp: time.Now(),
			IsFromMe:  true,
		},
		{
			ID:        "msg8",
			ChatJID:   "0987654321@s.whatsapp.net",
			Sender:    "0987654321",
			Content:   "Hello from another chat",
			Timestamp: time.Now(),
			IsFromMe:  false,
		},
	}

	err := suite.repo.StoreMessagesBatch(additionalMessages)
	suite.Require().NoError(err)

	// Test case 1: Search for "hello" (case-insensitive) in first chat
	results, err := suite.repo.SearchMessages("1234567890@s.whatsapp.net", "hello", 10)
	suite.NoError(err)
	suite.Len(results, 2) // Should find "Hello World" and "HELLO in caps"

	// Verify results contain expected messages
	foundContents := make(map[string]bool)
	for _, msg := range results {
		foundContents[msg.Content] = true
		suite.Equal("1234567890@s.whatsapp.net", msg.ChatJID) // All results should be from the specified chat
	}
	suite.True(foundContents["Hello World"])
	suite.True(foundContents["This is a test message with HELLO in caps"])

	// Test case 2: Search for "hello" in second chat
	results, err = suite.repo.SearchMessages("0987654321@s.whatsapp.net", "hello", 10)
	suite.NoError(err)
	suite.Len(results, 1) // Should find "Hello from another chat"
	suite.Equal("Hello from another chat", results[0].Content)

	// Test case 3: Search for non-existent text
	results, err = suite.repo.SearchMessages("1234567890@s.whatsapp.net", "nonexistent", 10)
	suite.NoError(err)
	suite.Len(results, 0)

	// Test case 4: Search in non-existent chat
	results, err = suite.repo.SearchMessages("nonexistent@s.whatsapp.net", "hello", 10)
	suite.NoError(err)
	suite.Len(results, 0)

	// Test case 5: Search with limit
	results, err = suite.repo.SearchMessages("1234567890@s.whatsapp.net", "hello", 1)
	suite.NoError(err)
	suite.Len(results, 1) // Should respect the limit

	// Test case 6: Search with partial word match
	results, err = suite.repo.SearchMessages("1234567890@s.whatsapp.net", "test", 10)
	suite.NoError(err)
	suite.Len(results, 1) // Should find "This is a test message with HELLO in caps"
	suite.Equal("This is a test message with HELLO in caps", results[0].Content)

	// Test case 7: Search with very high limit (should be capped)
	results, err = suite.repo.SearchMessages("1234567890@s.whatsapp.net", "hello", 2000)
	suite.NoError(err)
	suite.Len(results, 2) // Should find all matching messages regardless of high limit
}

// TestSearchMessagesStorage tests the storage layer search functionality
func (suite *ChatStorageTestSuite) TestSearchMessagesStorage() {
	// Create test data
	suite.createTestData()

	// Test the storage layer search method
	results, err := suite.storage.SearchMessages("hello", "1234567890@s.whatsapp.net", 10)
	suite.NoError(err)
	suite.Len(results, 1) // Should find "Hello World"
	suite.Equal("Hello World", results[0].Content)

	// Test with empty search text
	results, err = suite.storage.SearchMessages("", "1234567890@s.whatsapp.net", 10)
	suite.NoError(err)
	suite.Len(results, 0) // Should return no results for empty search

	// Test with case variation
	results, err = suite.storage.SearchMessages("HELLO", "1234567890@s.whatsapp.net", 10)
	suite.NoError(err)
	suite.Len(results, 1) // Should find "Hello World" (case-insensitive)
	suite.Equal("Hello World", results[0].Content)
}
