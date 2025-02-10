package utils

import (
	"encoding/csv"
	"fmt"
	"os"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

type RecordedMessage struct {
	MessageID      string `json:"message_id,omitempty"`
	JID            string `json:"jid,omitempty"`
	MessageContent string `json:"message_content,omitempty"`
}

// mutex to prevent concurrent file access
var fileMutex sync.Mutex

func FindRecordFromStorage(messageID string) (RecordedMessage, error) {
	fileMutex.Lock()
	defer fileMutex.Unlock()

	file, err := os.OpenFile(config.PathChatStorage, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		return RecordedMessage{}, fmt.Errorf("failed to open storage file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return RecordedMessage{}, fmt.Errorf("failed to read CSV records: %w", err)
	}

	for _, record := range records {
		if len(record) == 3 && record[0] == messageID {
			return RecordedMessage{
				MessageID:      record[0],
				JID:            record[1],
				MessageContent: record[2],
			}, nil
		}
	}
	return RecordedMessage{}, fmt.Errorf("message ID %s not found in storage", messageID)
}

func RecordMessage(messageID string, senderJID string, messageContent string) error {
	if !config.WhatsappChatStorage {
		return nil
	}

	fileMutex.Lock()
	defer fileMutex.Unlock()

	message := RecordedMessage{
		MessageID:      messageID,
		JID:            senderJID,
		MessageContent: messageContent,
	}

	// Read existing messages
	var records [][]string
	if file, err := os.OpenFile(config.PathChatStorage, os.O_RDONLY|os.O_CREATE, 0644); err == nil {
		defer file.Close()
		reader := csv.NewReader(file)
		records, err = reader.ReadAll()
		if err != nil {
			return fmt.Errorf("failed to read existing records: %w", err)
		}

		// Check for duplicates
		for _, record := range records {
			if len(record) == 3 && record[0] == messageID {
				return nil // Skip if duplicate found
			}
		}
	}

	// Prepare the new record
	newRecord := []string{message.MessageID, message.JID, message.MessageContent}
	records = append([][]string{newRecord}, records...) // Prepend new message

	// Write all records back to file
	file, err := os.OpenFile(config.PathChatStorage, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for writing: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.WriteAll(records); err != nil {
		return fmt.Errorf("failed to write CSV records: %w", err)
	}

	return nil
}
