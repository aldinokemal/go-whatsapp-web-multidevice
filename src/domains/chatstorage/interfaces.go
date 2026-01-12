package chatstorage

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type IChatStorageRepository interface {
	// Chat operations
	CreateMessage(ctx context.Context, evt *events.Message) error
	StoreChat(chat *Chat) error
	GetChat(jid string) (*Chat, error)
	GetChatByDevice(deviceID, jid string) (*Chat, error)
	GetChats(filter *ChatFilter) ([]*Chat, error)
	DeleteChat(jid string) error
	DeleteChatByDevice(deviceID, jid string) error

	// Message operations
	StoreMessage(message *Message) error
	StoreMessagesBatch(messages []*Message) error
	GetMessageByID(id string) (*Message, error) // New method for efficient ID-only search
	GetMessages(filter *MessageFilter) ([]*Message, error)
	SearchMessages(deviceID, chatJID, searchText string, limit int) ([]*Message, error) // Database-level search with device isolation
	DeleteMessage(id, chatJID string) error
	DeleteMessageByDevice(deviceID, id, chatJID string) error
	StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time) error

	// Statistics
	GetChatMessageCount(chatJID string) (int64, error)
	GetChatMessageCountByDevice(deviceID, chatJID string) (int64, error)
	GetTotalMessageCount() (int64, error)
	GetTotalChatCount() (int64, error)
	GetChatNameWithPushName(jid types.JID, chatJID string, senderUser string, pushName string) string
	GetChatNameWithPushNameByDevice(deviceID string, jid types.JID, chatJID string, senderUser string, pushName string) string
	GetStorageStatistics() (chatCount int64, messageCount int64, err error)

	// Cleanup operations
	TruncateAllChats() error
	TruncateAllDataWithLogging(logPrefix string) error
	DeleteDeviceData(deviceID string) error

	// Device registry operations
	SaveDeviceRecord(record *DeviceRecord) error
	ListDeviceRecords() ([]*DeviceRecord, error)
	GetDeviceRecord(deviceID string) (*DeviceRecord, error)
	DeleteDeviceRecord(deviceID string) error

	// Schema operations
	InitializeSchema() error
}
