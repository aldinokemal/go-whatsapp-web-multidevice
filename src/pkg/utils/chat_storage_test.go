package utils_test

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	. "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type ChatStorageTestSuite struct {
	suite.Suite
	tempDir     string
	origStorage bool
	origPath    string
}

func (suite *ChatStorageTestSuite) SetupTest() {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "chat_storage_test")
	assert.NoError(suite.T(), err)
	suite.tempDir = tempDir

	// Save original config values
	suite.origStorage = config.WhatsappChatStorage
	suite.origPath = config.PathChatStorage

	// Set test config values
	config.WhatsappChatStorage = true
	config.PathChatStorage = filepath.Join(tempDir, "chat_storage.csv")
}

func (suite *ChatStorageTestSuite) TearDownTest() {
	// Restore original config values
	config.WhatsappChatStorage = suite.origStorage
	config.PathChatStorage = suite.origPath

	// Clean up temp directory
	os.RemoveAll(suite.tempDir)
}

func (suite *ChatStorageTestSuite) createTestData() {
	// Create test CSV data
	file, err := os.Create(config.PathChatStorage)
	assert.NoError(suite.T(), err)
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	testData := [][]string{
		{"msg1", "user1@test.com", "Hello world"},
		{"msg2", "user2@test.com", "Test message"},
	}

	err = writer.WriteAll(testData)
	assert.NoError(suite.T(), err)
}

func (suite *ChatStorageTestSuite) TestFindRecordFromStorage() {
	// Test case: Record found
	suite.createTestData()
	record, err := FindRecordFromStorage("msg1")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "msg1", record.MessageID)
	assert.Equal(suite.T(), "user1@test.com", record.JID)
	assert.Equal(suite.T(), "Hello world", record.MessageContent)

	// Test case: Record not found
	_, err = FindRecordFromStorage("non_existent")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "not found in storage")

	// Test case: Empty file - should still report message not found
	os.Remove(config.PathChatStorage)
	_, err = FindRecordFromStorage("msg1")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "not found in storage")

	// Test case: Corrupted CSV file - should return CSV parsing error
	err = os.WriteFile(config.PathChatStorage, []byte("corrupted,csv,data\nwith,no,proper,format"), 0644)
	assert.NoError(suite.T(), err)
	_, err = FindRecordFromStorage("msg1")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to read CSV records")

	// Test case: File permissions issue
	// Create an unreadable directory for testing file permission issues
	unreadableDir := filepath.Join(suite.tempDir, "unreadable")
	err = os.Mkdir(unreadableDir, 0000)
	assert.NoError(suite.T(), err)
	defer os.Chmod(unreadableDir, 0755) // So it can be deleted during teardown

	// Temporarily change path to unreadable location
	origPath := config.PathChatStorage
	config.PathChatStorage = filepath.Join(unreadableDir, "inaccessible.csv")
	_, err = FindRecordFromStorage("anything")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to open storage file")

	// Restore path
	config.PathChatStorage = origPath
}

func (suite *ChatStorageTestSuite) TestRecordMessage() {
	// Test case: Normal recording
	err := RecordMessage("newMsg", "user@test.com", "New test message")
	assert.NoError(suite.T(), err)

	// Verify the message was recorded
	record, err := FindRecordFromStorage("newMsg")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "newMsg", record.MessageID)
	assert.Equal(suite.T(), "user@test.com", record.JID)
	assert.Equal(suite.T(), "New test message", record.MessageContent)

	// Test case: Duplicate message ID
	err = RecordMessage("newMsg", "user@test.com", "Duplicate message")
	assert.NoError(suite.T(), err)

	// Verify the duplicate wasn't added
	record, err = FindRecordFromStorage("newMsg")
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), "New test message", record.MessageContent, "Should not update existing record")

	// Test case: Disabled storage
	config.WhatsappChatStorage = false
	err = RecordMessage("anotherMsg", "user@test.com", "Should not be stored")
	assert.NoError(suite.T(), err)

	config.WhatsappChatStorage = true // Re-enable for next tests
	_, err = FindRecordFromStorage("anotherMsg")
	assert.Error(suite.T(), err, "Message should not be found when storage is disabled")

	// Test case: Write permission error - Alternative approach to avoid platform-specific issues
	// Instead of creating an unwritable file, we'll temporarily set PathChatStorage to a non-existent directory
	nonExistentPath := filepath.Join(suite.tempDir, "non-existent-dir", "test.csv")
	origPath := config.PathChatStorage
	config.PathChatStorage = nonExistentPath

	err = RecordMessage("failMsg", "user@test.com", "Should fail to write")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to open file for writing")

	// Restore path
	config.PathChatStorage = origPath
}

func TestChatStorageTestSuite(t *testing.T) {
	suite.Run(t, new(ChatStorageTestSuite))
}
