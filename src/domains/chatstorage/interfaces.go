package chatstorage

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type IChatStorageRepository interface {
	// Chat operations
	CreateMessage(ctx context.Context, evt *events.Message) error
	CreateReaction(ctx context.Context, evt *events.Message) error
	// CreateIncomingCallRecord persists an incoming call as a synthetic message (media_type "call") for chat history.
	CreateIncomingCallRecord(ctx context.Context, evt *events.CallOffer, autoRejected bool) error
	StoreChat(chat *Chat) error
	GetChat(jid string) (*Chat, error)
	GetChatByDevice(deviceID, jid string) (*Chat, error)
	GetChats(filter *ChatFilter) ([]*Chat, error)
	DeleteChat(jid string) error
	DeleteChatByDevice(deviceID, jid string) error

	// Message operations
	StoreMessage(message *Message) error
	StoreMessageEdit(edit *MessageEdit) error
	StoreMessagesBatch(messages []*Message) error
	GetMessageByID(id string) (*Message, error)                    // New method for efficient ID-only search
	GetMessageByIDAndDevice(deviceID, id string) (*Message, error) // Device-scoped ID lookup for device-isolated flows
	GetMessageEdits(originalMessageID, deviceID string) ([]*MessageEdit, error)
	GetMessages(filter *MessageFilter) ([]*Message, error)
	SearchMessages(deviceID, chatJID, searchText string, limit int) ([]*Message, error) // Database-level search with device isolation
	DeleteMessage(id, chatJID string) error
	DeleteMessageByDevice(deviceID, id, chatJID string) error
	StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time, msg *waE2E.Message) error

	// Chatwoot correlation operations
	UpsertChatwootMessageLink(link *ChatwootMessageLink) error
	GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*ChatwootMessageLink, error)
	GetChatwootMessageLinkByChatwootID(deviceID string, chatwootMessageID int) (*ChatwootMessageLink, error)
	// GetLatestChatwootMessageLinkByConversation resolves a conversation to its
	// most recent link. Conversation ids are numbered per Chatwoot account, so the
	// lookup is account-scoped. allowLegacyZero additionally matches rows whose
	// account id is 0 (pre-migration legacy links) — pass true only in legacy
	// single-account mode; in per-device mode it must be false, or a colliding
	// conversation id from another account could match a legacy row and misroute.
	// configID, when non-zero, further restricts the match to links written under
	// that device config: separate Chatwoot servers can collide on
	// (conversation_id, account_id), so per-device (forced-route) callers must
	// scope by their own config.
	GetLatestChatwootMessageLinkByConversation(conversationID, accountID int, allowLegacyZero bool, configID int64) (*ChatwootMessageLink, error)
	GetLatestUnreadChatwootMessageLinkByChat(deviceID, waChatJID string) (*ChatwootMessageLink, error)
	CountChatwootMessageLinksByConfig(configID int64) (int, error)
	// DeleteChatwootMessageLinksByConfig removes every link written under a
	// device config. Called when the config is deleted so stale links cannot
	// hijack reverse-route lookups after a delete-and-recreate rebind.
	DeleteChatwootMessageLinksByConfig(configID int64) error
	// BackfillChatwootMessageLinkAccount stamps the given account id onto legacy
	// links whose account id is still 0, so they resolve under exact-account
	// scoping instead of relying on the legacy-zero wildcard. Idempotent.
	BackfillChatwootMessageLinkAccount(accountID int) (int64, error)
	EnqueueChatwootForwardEvent(event *ChatwootForwardEvent) error
	ListDueChatwootForwardEvents(now time.Time, limit int) ([]*ChatwootForwardEvent, error)
	MarkChatwootForwardEventFailed(id int64, lastError string, nextAttemptAt time.Time) error
	MarkChatwootForwardEventDone(id int64) error

	// Chatwoot per-device configuration (multi-device / multi-inbox routing)
	SaveChatwootDeviceConfig(cfg *ChatwootDeviceConfig) error
	// UpdateChatwootDeviceConfigJID stamps the device's current WhatsApp JID
	// onto its config row (no-op without a row or when already current).
	// Reports whether the stored JID changed. Called on connect so a config
	// created before pairing — or stale after a re-pair — resolves on the
	// JID-keyed forward path.
	UpdateChatwootDeviceConfigJID(deviceID, deviceJID string) (bool, error)
	GetChatwootDeviceConfig(deviceID string) (*ChatwootDeviceConfig, error)
	GetChatwootDeviceConfigByIdentifier(identifier string) (*ChatwootDeviceConfig, error)
	GetChatwootDeviceConfigByInbox(accountID, inboxID int) (*ChatwootDeviceConfig, error)
	ListChatwootDeviceConfigs() ([]*ChatwootDeviceConfig, error)
	DeleteChatwootDeviceConfig(deviceID string) error
	CountChatwootDeviceConfigs() (int, error)

	// Statistics
	GetChatMessageCount(chatJID string) (int64, error)
	GetChatMessageCountByDevice(deviceID, chatJID string) (int64, error)
	GetTotalMessageCount() (int64, error)
	GetTotalChatCount() (int64, error)
	GetFilteredChatCount(filter *ChatFilter) (int64, error)
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
	// GetDeviceRecordByJID fetches a device record by its JID. A full AD JID resolves
	// the exact slot; a bare-number JID resolves only while unambiguous — when several
	// slots share the number it returns nil rather than an arbitrary sibling's record.
	GetDeviceRecordByJID(jid string) (*DeviceRecord, error)
	DeleteDeviceRecord(deviceID string) error
	// SetDeviceWebhookURL sets the webhook URL for a device.
	SetDeviceWebhookURL(deviceID string, webhookURL *string) error
	// GetDeviceWebhookURL retrieves the webhook URL for a device.
	GetDeviceWebhookURL(deviceID string) (*string, error)
	// SetDeviceWebhookConfig sets the full webhook configuration for a device.
	SetDeviceWebhookConfig(deviceID string, config *DeviceWebhookConfig) error
	// GetDeviceWebhookConfig retrieves the full webhook configuration for a device.
	GetDeviceWebhookConfig(deviceID string) (*DeviceWebhookConfig, error)

	// Schema operations
	InitializeSchema() error
}
