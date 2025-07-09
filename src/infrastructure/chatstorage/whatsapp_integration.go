package chatstorage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
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

	// Get appropriate chat name
	chatName := s.GetChatName(evt.Info.Chat, chatJID, evt.Info.Sender.User)

	// Extract ephemeral expiration
	ephemeralExpiration := ExtractEphemeralExpiration(evt.Message)

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
	content := ExtractMessageText(evt.Message)
	mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := ExtractMediaInfo(evt.Message)

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

// GetChatName determines the appropriate name for a chat
func (s *Storage) GetChatName(jid types.JID, chatJID string, senderUser string) string {
	// First, check if chat already exists with a name
	existingChat, err := s.repo.GetChat(chatJID)
	if err == nil && existingChat != nil && existingChat.Name != "" {
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
		// Use sender as name or JID user
		if senderUser != "" {
			name = senderUser
		} else {
			name = jid.User
		}
	}

	return name
}

// ExtractMessageText extracts text content from a WhatsApp message
func ExtractMessageText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}

	// Check for regular text message
	if text := msg.GetConversation(); text != "" {
		return text
	}

	// Check for extended text message (with link preview, etc.)
	if extendedText := msg.GetExtendedTextMessage(); extendedText != nil {
		return extendedText.GetText()
	}

	// Check for image with caption
	if img := msg.GetImageMessage(); img != nil && img.GetCaption() != "" {
		return img.GetCaption()
	}

	// Check for video with caption
	if vid := msg.GetVideoMessage(); vid != nil && vid.GetCaption() != "" {
		return vid.GetCaption()
	}

	// Check for document with caption
	if doc := msg.GetDocumentMessage(); doc != nil && doc.GetCaption() != "" {
		return doc.GetCaption()
	}

	// Check for buttons response message
	if buttonsResponse := msg.GetButtonsResponseMessage(); buttonsResponse != nil {
		return buttonsResponse.GetSelectedDisplayText()
	}

	// Check for list response message
	if listResponse := msg.GetListResponseMessage(); listResponse != nil {
		return listResponse.GetTitle()
	}

	// Check for template button reply
	if templateButtonReply := msg.GetTemplateButtonReplyMessage(); templateButtonReply != nil {
		return templateButtonReply.GetSelectedDisplayText()
	}

	return ""
}

// ExtractMediaInfo extracts media information from a WhatsApp message
func ExtractMediaInfo(msg *waE2E.Message) (mediaType string, filename string, url string, mediaKey []byte, fileSHA256 []byte, fileEncSHA256 []byte, fileLength uint64) {
	if msg == nil {
		return "", "", "", nil, nil, nil, 0
	}

	// Check for image message
	if img := msg.GetImageMessage(); img != nil {
		filename = generateMediaFilename("image", "jpg", img.GetCaption())
		return "image", filename,
			img.GetURL(), img.GetMediaKey(), img.GetFileSHA256(),
			img.GetFileEncSHA256(), img.GetFileLength()
	}

	// Check for video message
	if vid := msg.GetVideoMessage(); vid != nil {
		filename = generateMediaFilename("video", "mp4", vid.GetCaption())
		return "video", filename,
			vid.GetURL(), vid.GetMediaKey(), vid.GetFileSHA256(),
			vid.GetFileEncSHA256(), vid.GetFileLength()
	}

	// Check for audio message
	if aud := msg.GetAudioMessage(); aud != nil {
		extension := "ogg"
		if aud.GetPTT() {
			extension = "ogg" // Voice notes are typically ogg
		}
		filename = generateMediaFilename("audio", extension, "")
		return "audio", filename,
			aud.GetURL(), aud.GetMediaKey(), aud.GetFileSHA256(),
			aud.GetFileEncSHA256(), aud.GetFileLength()
	}

	// Check for document message
	if doc := msg.GetDocumentMessage(); doc != nil {
		filename = doc.GetFileName()
		if filename == "" {
			filename = generateMediaFilename("document", "", doc.GetTitle())
		}
		return "document", filename,
			doc.GetURL(), doc.GetMediaKey(), doc.GetFileSHA256(),
			doc.GetFileEncSHA256(), doc.GetFileLength()
	}

	// Check for sticker message
	if sticker := msg.GetStickerMessage(); sticker != nil {
		filename = generateMediaFilename("sticker", "webp", "")
		return "sticker", filename,
			sticker.GetURL(), sticker.GetMediaKey(), sticker.GetFileSHA256(),
			sticker.GetFileEncSHA256(), sticker.GetFileLength()
	}

	return "", "", "", nil, nil, nil, 0
}

// ExtractEphemeralExpiration extracts ephemeral expiration from a WhatsApp message
func ExtractEphemeralExpiration(msg *waE2E.Message) uint32 {
	if msg == nil {
		return 0
	}

	// Check extended text message
	if extendedText := msg.GetExtendedTextMessage(); extendedText != nil {
		if contextInfo := extendedText.GetContextInfo(); contextInfo != nil {
			return contextInfo.GetExpiration()
		}
	}

	// Check regular conversation message
	if msg.GetConversation() != "" {
		// Regular text messages might have context info too
		// This would need to be checked based on the actual protobuf structure
	}

	// Check image message
	if img := msg.GetImageMessage(); img != nil {
		if contextInfo := img.GetContextInfo(); contextInfo != nil {
			return contextInfo.GetExpiration()
		}
	}

	// Check video message
	if vid := msg.GetVideoMessage(); vid != nil {
		if contextInfo := vid.GetContextInfo(); contextInfo != nil {
			return contextInfo.GetExpiration()
		}
	}

	// Check audio message
	if aud := msg.GetAudioMessage(); aud != nil {
		if contextInfo := aud.GetContextInfo(); contextInfo != nil {
			return contextInfo.GetExpiration()
		}
	}

	// Check document message
	if doc := msg.GetDocumentMessage(); doc != nil {
		if contextInfo := doc.GetContextInfo(); contextInfo != nil {
			return contextInfo.GetExpiration()
		}
	}

	// Check sticker message
	if sticker := msg.GetStickerMessage(); sticker != nil {
		if contextInfo := sticker.GetContextInfo(); contextInfo != nil {
			return contextInfo.GetExpiration()
		}
	}

	return 0
}

// generateMediaFilename creates a filename for media files
func generateMediaFilename(mediaType, extension, caption string) string {
	timestamp := time.Now().Format("20060102_150405")

	// Use caption as part of filename if available
	if caption != "" {
		// Sanitize caption for filename
		caption = strings.ReplaceAll(caption, " ", "_")
		caption = strings.ReplaceAll(caption, "/", "-")
		caption = strings.ReplaceAll(caption, "\\", "-")
		caption = strings.ReplaceAll(caption, ":", "-")

		// Limit caption length
		if len(caption) > 30 {
			caption = caption[:30]
		}

		if extension != "" {
			return fmt.Sprintf("%s_%s_%s.%s", mediaType, timestamp, caption, extension)
		}
		return fmt.Sprintf("%s_%s_%s", mediaType, timestamp, caption)
	}

	// Default filename without caption
	if extension != "" {
		return fmt.Sprintf("%s_%s.%s", mediaType, timestamp, extension)
	}
	return fmt.Sprintf("%s_%s", mediaType, timestamp)
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

// HandleHistorySync processes history sync events to populate message history
func (s *Storage) HandleHistorySync(client *whatsmeow.Client, historySync *events.HistorySync, logger waLog.Logger) error {
	syncedCount := 0

	for _, conversation := range historySync.Data.Conversations {
		if conversation.ID == nil {
			continue
		}

		chatJID := *conversation.ID

		// Parse JID
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			logger.Warnf("Failed to parse JID %s: %v", chatJID, err)
			continue
		}

		// Get chat name from conversation metadata
		name := s.extractChatNameFromConversation(conversation, jid, logger)

		// Process messages
		messages := conversation.Messages
		if len(messages) > 0 {
			// Get latest message timestamp
			latestMsg := messages[0]
			if latestMsg == nil || latestMsg.Message == nil {
				continue
			}

			timestamp := time.Unix(int64(latestMsg.Message.GetMessageTimestamp()), 0)

			// Store chat
			chat := &Chat{
				JID:                 chatJID,
				Name:                name,
				LastMessageTime:     timestamp,
				EphemeralExpiration: 0, // Default to 0 for history sync
			}

			if err := s.repo.StoreChat(chat); err != nil {
				logger.Warnf("Failed to store chat from history: %v", err)
				continue
			}

			// Store messages
			for _, msg := range messages {
				if msg == nil || msg.Message == nil {
					continue
				}

				// Extract content and media info
				content := ExtractMessageText(msg.Message.Message)
				mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := ExtractMediaInfo(msg.Message.Message)

				// Skip empty messages
				if content == "" && mediaType == "" {
					continue
				}

				// Determine sender
				sender := jid.User
				isFromMe := false

				if msg.Message.Key != nil {
					if msg.Message.Key.FromMe != nil {
						isFromMe = *msg.Message.Key.FromMe
					}
					if !isFromMe && msg.Message.Key.Participant != nil && *msg.Message.Key.Participant != "" {
						sender = *msg.Message.Key.Participant
					} else if isFromMe && client.Store.ID != nil {
						sender = client.Store.ID.User
					}
				}

				// Get message ID
				msgID := ""
				if msg.Message.Key != nil && msg.Message.Key.ID != nil {
					msgID = *msg.Message.Key.ID
				}

				// Get timestamp
				msgTimestamp := time.Unix(int64(msg.Message.GetMessageTimestamp()), 0)

				// Create and store message
				message := &Message{
					ID:            msgID,
					ChatJID:       chatJID,
					Sender:        sender,
					Content:       content,
					Timestamp:     msgTimestamp,
					IsFromMe:      isFromMe,
					MediaType:     mediaType,
					Filename:      filename,
					URL:           url,
					MediaKey:      mediaKey,
					FileSHA256:    fileSHA256,
					FileEncSHA256: fileEncSHA256,
					FileLength:    fileLength,
				}

				if err := s.repo.StoreMessage(message); err != nil {
					logger.Warnf("Failed to store history message: %v", err)
				} else {
					syncedCount++
				}
			}
		}
	}

	logger.Infof("History sync complete. Stored %d messages.", syncedCount)
	return nil
}

// extractChatNameFromConversation extracts chat name from history sync conversation data
func (s *Storage) extractChatNameFromConversation(conversation interface{}, jid types.JID, logger waLog.Logger) string {
	// Try to extract name from conversation metadata
	// This is a simplified version - the actual implementation would use reflection
	// to handle different conversation types from history sync

	// For now, return a default name based on JID type
	if jid.Server == "g.us" {
		return fmt.Sprintf("Group %s", jid.User)
	}

	return jid.User
}

// StoreSentMessage stores a message that was sent by the user
func (s *Storage) StoreSentMessage(messageID string, senderJID string, recipientJID string, content string, timestamp time.Time) error {
	// Ensure JID is properly formatted
	jid, err := types.ParseJID(recipientJID)
	if err != nil {
		return fmt.Errorf("invalid JID format: %w", err)
	}

	chatJID := jid.String()

	// Get chat name
	chatName := s.GetChatName(jid, chatJID, jid.User)

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
