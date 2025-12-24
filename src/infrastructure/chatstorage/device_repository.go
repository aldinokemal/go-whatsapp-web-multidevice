package chatstorage

import (
	"context"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// DeviceRepository wraps a base repository and injects device_id into all operations.
// This keeps the existing interface while making sure each device is logically separated.
type DeviceRepository struct {
	deviceID string
	base     domainChatStorage.IChatStorageRepository
}

func NewDeviceRepository(deviceID string, base domainChatStorage.IChatStorageRepository) domainChatStorage.IChatStorageRepository {
	return &DeviceRepository{
		deviceID: deviceID,
		base:     base,
	}
}

func (r *DeviceRepository) withDeviceChat(chat *domainChatStorage.Chat) *domainChatStorage.Chat {
	if chat == nil {
		return nil
	}

	// Prefer a JID already set on the chat.
	if chat.DeviceID != "" && strings.Contains(chat.DeviceID, "@") {
		r.deviceID = chat.DeviceID
		return chat
	}

	// Apply wrapper JID if already known.
	if strings.Contains(r.deviceID, "@") {
		chat.DeviceID = r.deviceID
		return chat
	}

	// Fallback to wrapper device ID when chat is empty.
	if chat.DeviceID == "" {
		chat.DeviceID = r.deviceID
	}
	return chat
}

func (r *DeviceRepository) CreateMessage(ctx context.Context, evt *events.Message) error {
	// Base repository will attempt to derive device id from client; keep call unchanged for now.
	return r.base.CreateMessage(ctx, evt)
}

func (r *DeviceRepository) StoreChat(chat *domainChatStorage.Chat) error {
	return r.base.StoreChat(r.withDeviceChat(chat))
}

func (r *DeviceRepository) GetChat(jid string) (*domainChatStorage.Chat, error) {
	return r.base.GetChat(jid)
}

func (r *DeviceRepository) GetChats(filter *domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	if filter != nil {
		switch {
		case filter.DeviceID != "" && strings.Contains(filter.DeviceID, "@"):
			r.deviceID = filter.DeviceID
		case strings.Contains(r.deviceID, "@"):
			filter.DeviceID = r.deviceID
		case filter.DeviceID == "":
			filter.DeviceID = r.deviceID
		}
	}
	return r.base.GetChats(filter)
}

func (r *DeviceRepository) DeleteChat(jid string) error {
	return r.base.DeleteChat(jid)
}

func (r *DeviceRepository) StoreMessage(message *domainChatStorage.Message) error {
	return r.base.StoreMessage(message)
}

func (r *DeviceRepository) StoreMessagesBatch(messages []*domainChatStorage.Message) error {
	return r.base.StoreMessagesBatch(messages)
}

func (r *DeviceRepository) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	return r.base.GetMessageByID(id)
}

func (r *DeviceRepository) GetMessages(filter *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return r.base.GetMessages(filter)
}

func (r *DeviceRepository) SearchMessages(chatJID, searchText string, limit int) ([]*domainChatStorage.Message, error) {
	return r.base.SearchMessages(chatJID, searchText, limit)
}

func (r *DeviceRepository) DeleteMessage(id, chatJID string) error {
	return r.base.DeleteMessage(id, chatJID)
}

func (r *DeviceRepository) StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time) error {
	return r.base.StoreSentMessageWithContext(ctx, messageID, senderJID, recipientJID, content, timestamp)
}

func (r *DeviceRepository) GetChatMessageCount(chatJID string) (int64, error) {
	return r.base.GetChatMessageCount(chatJID)
}

func (r *DeviceRepository) GetTotalMessageCount() (int64, error) {
	return r.base.GetTotalMessageCount()
}

func (r *DeviceRepository) GetTotalChatCount() (int64, error) {
	return r.base.GetTotalChatCount()
}

func (r *DeviceRepository) GetChatNameWithPushName(jid types.JID, chatJID string, senderUser string, pushName string) string {
	return r.base.GetChatNameWithPushName(jid, chatJID, senderUser, pushName)
}

func (r *DeviceRepository) GetStorageStatistics() (chatCount int64, messageCount int64, err error) {
	return r.base.GetStorageStatistics()
}

func (r *DeviceRepository) TruncateAllChats() error {
	return r.base.TruncateAllChats()
}

func (r *DeviceRepository) TruncateAllDataWithLogging(logPrefix string) error {
	return r.base.TruncateAllDataWithLogging(logPrefix)
}

func (r *DeviceRepository) InitializeSchema() error {
	return r.base.InitializeSchema()
}

func (r *DeviceRepository) DeleteDeviceData(deviceID string) error {
	target := deviceID
	if target == "" {
		target = r.deviceID
	}
	return r.base.DeleteDeviceData(target)
}

func (r *DeviceRepository) SaveDeviceRecord(record *domainChatStorage.DeviceRecord) error {
	return r.base.SaveDeviceRecord(record)
}

func (r *DeviceRepository) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	return r.base.ListDeviceRecords()
}

func (r *DeviceRepository) GetDeviceRecord(deviceID string) (*domainChatStorage.DeviceRecord, error) {
	return r.base.GetDeviceRecord(deviceID)
}

func (r *DeviceRepository) DeleteDeviceRecord(deviceID string) error {
	return r.base.DeleteDeviceRecord(deviceID)
}
