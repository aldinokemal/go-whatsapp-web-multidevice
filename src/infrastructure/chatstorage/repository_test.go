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
