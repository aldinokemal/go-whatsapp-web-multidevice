package chatstorage

import (
	"context"
	"fmt"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// ChatStorage Logout Functionality
//
// This package provides comprehensive logout handling that truncates all chatstorage data
// when a user logs out. The truncation occurs in two scenarios:
//
// 1. REMOTE LOGOUT: When the user logs out from their WhatsApp phone/device
//    - Triggered by the LoggedOut event from WhatsApp
//    - Handled automatically by the event system
//
// 2. MANUAL LOGOUT: When the user calls the /app/logout API endpoint
//    - Triggered by explicit API call
//    - Initiated by user action through UI or REST API
//
// The truncation process:
// - Deletes all messages from the messages table
// - Deletes all chats from the chats table
// - Maintains referential integrity (messages deleted first due to foreign key constraints)
// - Provides detailed logging of the process
// - Continues with other cleanup operations even if chatstorage truncation fails
//
// Methods available:
// - TruncateAllData(): Basic truncation without logging
// - TruncateAllDataWithLogging(logPrefix): Truncation with detailed before/after statistics
// - GetStorageStatistics(): Get current chat and message counts
//
// Usage in logout process:
// The truncation is automatically integrated into the WhatsApp client logout handling
// and will be called whenever a logout occurs, ensuring no chat history persists
// after the user session ends.

// CreateMessage processes and stores a WhatsApp message event
func (s *Storage) CreateMessage(ctx context.Context, evt *events.Message) error {
	if evt == nil || evt.Message == nil {
		return nil
	}

	// Extract chat and sender information
	chatJID := evt.Info.Chat.String()
	// Store the full sender JID (user@server) to ensure consistency between received and sent messages
	sender := evt.Info.Sender.String()

	// Get appropriate chat name using pushname if available
	chatName := s.GetChatNameWithPushName(evt.Info.Chat, chatJID, evt.Info.Sender.User, evt.Info.PushName)

	// Extract ephemeral expiration
	ephemeralExpiration := utils.ExtractEphemeralExpiration(evt.Message)

	// Create or update chat
	chat := &Chat{
		JID:                 chatJID,
		Name:                chatName,
		LastMessageTime:     evt.Info.Timestamp,
		EphemeralExpiration: ephemeralExpiration,
	}

	// Store or update the chat
	if err := s.repo.StoreChat(chat); err != nil {
		return fmt.Errorf("failed to store chat: %w", err)
	}

	// Extract message content and media info
	content := utils.ExtractMessageTextFromProto(evt.Message)
	mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := utils.ExtractMediaInfo(evt.Message)

	// Skip if there's no content and no media
	if content == "" && mediaType == "" {
		logrus.Debugf("Skipping message %s - no content or media", evt.Info.ID)
		return nil
	}

	// Create message object
	message := &Message{
		ID:            evt.Info.ID,
		ChatJID:       chatJID,
		Sender:        sender,
		Content:       content,
		Timestamp:     evt.Info.Timestamp,
		IsFromMe:      evt.Info.IsFromMe,
		MediaType:     mediaType,
		Filename:      filename,
		URL:           url,
		MediaKey:      mediaKey,
		FileSHA256:    fileSHA256,
		FileEncSHA256: fileEncSHA256,
		FileLength:    fileLength,
	}

	// Store the message
	return s.repo.StoreMessage(message)
}

// GetChatNameWithPushName determines the appropriate name for a chat with pushname support
func (s *Storage) GetChatNameWithPushName(jid types.JID, chatJID string, senderUser string, pushName string) string {
	// First, check if chat already exists with a name
	existingChat, err := s.repo.GetChat(chatJID)
	if err == nil && existingChat != nil && existingChat.Name != "" {
		// If we have a pushname and the existing name is just a phone number/JID user, update it
		if pushName != "" && (existingChat.Name == jid.User || existingChat.Name == senderUser) {
			return pushName
		}
		return existingChat.Name
	}

	// Determine chat type and name
	var name string

	switch jid.Server {
	case "g.us":
		// This is a group chat
		// For now, use a generic name - this can be enhanced later with group info
		name = fmt.Sprintf("Group %s", jid.User)
	case "newsletter":
		// This is a newsletter/channel
		name = fmt.Sprintf("Newsletter %s", jid.User)
	default:
		// This is an individual contact
		// Priority: pushName > senderUser > JID user
		if pushName != "" && pushName != senderUser && pushName != jid.User {
			name = pushName
		} else if senderUser != "" {
			name = senderUser
		} else {
			name = jid.User
		}
	}

	return name
}

// GetMessageHistory retrieves message history for a chat with pagination
func (s *Storage) GetMessageHistory(chatJID string, limit int, offset int) ([]*Message, error) {
	filter := &MessageFilter{
		ChatJID: chatJID,
		Limit:   limit,
		Offset:  offset,
	}

	return s.repo.GetMessages(filter)
}

// GetRecentChats retrieves recent chats with their last message time
func (s *Storage) GetRecentChats(limit int) ([]*Chat, error) {
	filter := &ChatFilter{
		Limit: limit,
	}

	return s.repo.GetChats(filter)
}

// SearchMessages searches for messages containing specific text using database-level filtering
func (s *Storage) SearchMessages(searchText string, chatJID string, limit int) ([]*Message, error) {
	// Delegate to repository for efficient database-level search
	return s.repo.SearchMessages(chatJID, searchText, limit)
}

// GetMediaMessages retrieves only media messages from a chat
func (s *Storage) GetMediaMessages(chatJID string, limit int) ([]*Message, error) {
	filter := &MessageFilter{
		ChatJID:   chatJID,
		Limit:     limit,
		MediaOnly: true,
	}

	return s.repo.GetMessages(filter)
}

// DownloadMedia prepares media info for download
func (s *Storage) GetMediaDownloadInfo(messageID, chatJID string) (*MediaInfo, error) {
	return s.repo.GetMediaInfo(messageID, chatJID)
}

// UpdateGroupInfo updates group chat information when available
func (s *Storage) UpdateGroupInfo(client *whatsmeow.Client, jid types.JID, logger waLog.Logger) error {
	if jid.Server != "g.us" {
		return nil // Not a group
	}

	groupInfo, err := client.GetGroupInfo(jid)
	if err != nil {
		logger.Warnf("Failed to get group info for %s: %v", jid.String(), err)
		return fmt.Errorf("failed to get group info: %w", err)
	}

	// Update chat with group name
	chat := &Chat{
		JID:             jid.String(),
		Name:            groupInfo.Name,
		LastMessageTime: time.Now(),
	}

	if err := s.repo.StoreChat(chat); err != nil {
		logger.Errorf("Failed to store group chat %s: %v", jid.String(), err)
		return fmt.Errorf("failed to store group chat: %w", err)
	}

	logger.Infof("Updated group info for %s: %s", jid.String(), groupInfo.Name)
	return nil
}

// StoreSentMessage stores a message that was sent by the user
func (s *Storage) StoreSentMessage(messageID string, senderJID string, recipientJID string, content string, timestamp time.Time) error {
	// Ensure JID is properly formatted
	jid, err := types.ParseJID(recipientJID)
	if err != nil {
		return fmt.Errorf("invalid JID format: %w", err)
	}

	chatJID := jid.String()

	// Get chat name (no pushname available for sent messages)
	chatName := s.GetChatNameWithPushName(jid, chatJID, jid.User, "")

	// Store or update chat
	chat := &Chat{
		JID:             chatJID,
		Name:            chatName,
		LastMessageTime: timestamp,
	}

	if err := s.repo.StoreChat(chat); err != nil {
		return fmt.Errorf("failed to store chat: %w", err)
	}

	// Store the sent message
	message := &Message{
		ID:        messageID,
		ChatJID:   chatJID,
		Sender:    senderJID,
		Content:   content,
		Timestamp: timestamp,
		IsFromMe:  true,
	}

	return s.repo.StoreMessage(message)
}

// StoreSentMessageWithContext stores a message that was sent by the user with context cancellation support
func (s *Storage) StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time) error {
	// Check if context is already cancelled before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Ensure JID is properly formatted
	jid, err := types.ParseJID(recipientJID)
	if err != nil {
		return fmt.Errorf("invalid JID format: %w", err)
	}

	chatJID := jid.String()

	// Get chat name (no pushname available for sent messages)
	chatName := s.GetChatNameWithPushName(jid, chatJID, jid.User, "")

	// Check context again before database operations
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Store or update chat
	chat := &Chat{
		JID:             chatJID,
		Name:            chatName,
		LastMessageTime: timestamp,
	}

	if err := s.repo.StoreChat(chat); err != nil {
		return fmt.Errorf("failed to store chat: %w", err)
	}

	// Check context one more time before storing message
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Store the sent message
	message := &Message{
		ID:        messageID,
		ChatJID:   chatJID,
		Sender:    senderJID,
		Content:   content,
		Timestamp: timestamp,
		IsFromMe:  true,
	}

	return s.repo.StoreMessage(message)
}

// FindMessageByID retrieves a message by its ID for reply functionality
func (s *Storage) FindMessageByID(messageID string) (*Message, error) {
	// Use the optimized GetMessageByID method
	msg, err := s.repo.GetMessageByID(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	if msg == nil {
		return nil, fmt.Errorf("message with ID %s not found", messageID)
	}

	return msg, nil
}

// GetChat retrieves a chat by its JID
func (s *Storage) GetChat(jid string) (*Chat, error) {
	return s.repo.GetChat(jid)
}

// StoreChat stores or updates a chat
func (s *Storage) StoreChat(chat *Chat) error {
	return s.repo.StoreChat(chat)
}

// StoreMessage stores a message
func (s *Storage) StoreMessage(message *Message) error {
	return s.repo.StoreMessage(message)
}

// StoreMessagesBatch stores multiple messages in a single transaction
func (s *Storage) StoreMessagesBatch(messages []*Message) error {
	return s.repo.StoreMessagesBatch(messages)
}

// TruncateAllMessages deletes all messages from the chatstorage database
func (s *Storage) TruncateAllMessages() error {
	return s.repo.TruncateAllMessages()
}

// TruncateAllChats deletes all chats from the chatstorage database
func (s *Storage) TruncateAllChats() error {
	return s.repo.TruncateAllChats()
}

// TruncateAllData deletes all chats and messages from the chatstorage database
// This method should be called during user logout to clear all stored data
func (s *Storage) TruncateAllData() error {
	return s.repo.TruncateAllData()
}

// GetStorageStatistics returns current storage statistics for logging purposes
func (s *Storage) GetStorageStatistics() (chatCount int64, messageCount int64, err error) {
	// Count all chats using efficient query
	chatCount, err = s.repo.GetTotalChatCount()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get chat count: %w", err)
	}

	// Count all messages
	messageCount, err = s.repo.GetTotalMessageCount()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get message count: %w", err)
	}

	return chatCount, messageCount, nil
}

// TruncateAllDataWithLogging performs truncation with detailed logging
func (s *Storage) TruncateAllDataWithLogging(logPrefix string) error {
	// Get statistics before truncation
	chatCount, messageCount, err := s.GetStorageStatistics()
	if err != nil {
		logrus.Warnf("[%s] Failed to get storage statistics before truncation: %v", logPrefix, err)
	} else {
		logrus.Infof("[%s] Storage before truncation: %d chats, %d messages", logPrefix, chatCount, messageCount)
	}

	// Perform truncation
	if err := s.TruncateAllData(); err != nil {
		return fmt.Errorf("failed to truncate chatstorage data: %w", err)
	}

	// Verify truncation
	chatCountAfter, messageCountAfter, err := s.GetStorageStatistics()
	if err != nil {
		logrus.Warnf("[%s] Failed to get storage statistics after truncation: %v", logPrefix, err)
	} else {
		logrus.Infof("[%s] Storage after truncation: %d chats, %d messages", logPrefix, chatCountAfter, messageCountAfter)
		if chatCountAfter == 0 && messageCountAfter == 0 {
			logrus.Infof("[%s] ✅ Chatstorage truncation completed successfully", logPrefix)
		} else {
			logrus.Warnf("[%s] ⚠️ Truncation may not have completed fully", logPrefix)
		}
	}

	return nil
}
