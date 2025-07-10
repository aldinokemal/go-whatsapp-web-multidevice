package chatstorage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// CreateMessage processes and stores a WhatsApp message event
func (s *Storage) CreateMessage(ctx context.Context, evt *events.Message) error {
	if evt == nil || evt.Message == nil {
		return nil
	}

	// Extract chat and sender information
	chatJID := evt.Info.Chat.String()
	sender := evt.Info.Sender.User

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

// SearchMessages searches for messages containing specific text
func (s *Storage) SearchMessages(searchText string, chatJID string, limit int) ([]*Message, error) {
	// This is a simple implementation - can be enhanced with full-text search
	messages, err := s.repo.GetMessages(&MessageFilter{
		ChatJID: chatJID,
		Limit:   limit,
	})

	if err != nil {
		return nil, err
	}

	// Filter messages containing search text
	var results []*Message
	searchLower := strings.ToLower(searchText)

	for _, msg := range messages {
		if strings.Contains(strings.ToLower(msg.Content), searchLower) {
			results = append(results, msg)
		}
	}

	return results, nil
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
		return fmt.Errorf("failed to get group info: %w", err)
	}

	// Update chat with group name
	chat := &Chat{
		JID:             jid.String(),
		Name:            groupInfo.Name,
		LastMessageTime: time.Now(),
	}

	return s.repo.StoreChat(chat)
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

// FindMessageByID retrieves a message by its ID for reply functionality
func (s *Storage) FindMessageByID(messageID string) (*Message, error) {
	// We need to search across all chats since we don't know which chat the message belongs to
	// First, get all chats
	chats, err := s.repo.GetChats(&ChatFilter{Limit: 1000})
	if err != nil {
		return nil, fmt.Errorf("failed to get chats: %w", err)
	}

	// Search for the message in each chat
	for _, chat := range chats {
		msg, err := s.repo.GetMessage(messageID, chat.JID)
		if err != nil {
			continue
		}
		if msg != nil {
			return msg, nil
		}
	}

	return nil, fmt.Errorf("message with ID %s not found", messageID)
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
