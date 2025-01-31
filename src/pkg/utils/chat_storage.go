package utils

import (
	"fmt"
	"os"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v2/log"
)

type RecordedMessage struct {
	MessageID      string `json:"message_id,omitempty"`
	JID            string `json:"jid,omitempty"`
	MessageContent string `json:"message_content,omitempty"`
}

func FindRecordFromStorage(messageID string) (RecordedMessage, error) {
	data, err := os.ReadFile(config.PathChatStorage)
	if err != nil {
		return RecordedMessage{}, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) == 3 && parts[0] == messageID {
			return RecordedMessage{
				MessageID:      parts[0],
				JID:            parts[1],
				MessageContent: parts[2],
			}, nil
		}
	}
	return RecordedMessage{}, fmt.Errorf("message ID %s not found in storage", messageID)
}

func RecordMessage(messageID string, senderJID string, messageContent string) {
	message := RecordedMessage{
		MessageID:      messageID,
		JID:            senderJID,
		MessageContent: messageContent,
	}

	// Read existing messages
	var messages []RecordedMessage
	if data, err := os.ReadFile(config.PathChatStorage); err == nil {
		// Split file by newlines and parse each line
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.Split(line, ",")

			msg := RecordedMessage{
				MessageID:      parts[0],
				JID:            parts[1],
				MessageContent: parts[2],
			}
			messages = append(messages, msg)
		}
	}

	// Check for duplicates
	for _, msg := range messages {
		if msg.MessageID == message.MessageID {
			return // Skip if duplicate found
		}
	}

	// Write new message at the top
	f, err := os.OpenFile(config.PathChatStorage, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Errorf("Failed to open received-chat.txt: %v", err)
		return
	}
	defer f.Close()

	// Write new message first
	csvLine := fmt.Sprintf("%s,%s,%s\n", message.MessageID, message.JID, message.MessageContent)
	if _, err := f.WriteString(csvLine); err != nil {
		log.Errorf("Failed to write to received-chat.txt: %v", err)
		return
	}

	// Write existing messages after
	for _, msg := range messages {
		csvLine := fmt.Sprintf("%s,%s,%s\n", msg.MessageID, msg.JID, msg.MessageContent)
		if _, err := f.WriteString(csvLine); err != nil {
			log.Errorf("Failed to write to received-chat.txt: %v", err)
			return
		}
	}
}
