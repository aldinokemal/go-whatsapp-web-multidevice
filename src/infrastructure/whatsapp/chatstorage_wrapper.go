package whatsapp

import (
	"context"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// deviceChatStorage wraps a base repository and injects device_id into operations
// to keep chat/message separation per device without importing chatstorage to avoid cycles.
type deviceChatStorage struct {
	deviceID string
	base     domainChatStorage.IChatStorageRepository
}

func newDeviceChatStorage(deviceID string, base domainChatStorage.IChatStorageRepository) domainChatStorage.IChatStorageRepository {
	if base == nil {
		return nil
	}
	return &deviceChatStorage{
		deviceID: deviceID,
		base:     base,
	}
}

func (r *deviceChatStorage) withDeviceChat(chat *domainChatStorage.Chat) *domainChatStorage.Chat {
	if chat == nil {
		return nil
	}
	if chat.DeviceID == "" {
		chat.DeviceID = r.deviceID
	}
	return chat
}

func (r *deviceChatStorage) CreateMessage(ctx context.Context, evt *events.Message) error {
	return r.base.CreateMessage(ctx, evt)
}

func (r *deviceChatStorage) StoreChat(chat *domainChatStorage.Chat) error {
	return r.base.StoreChat(r.withDeviceChat(chat))
}

func (r *deviceChatStorage) GetChat(jid string) (*domainChatStorage.Chat, error) {
	return r.base.GetChatByDevice(r.deviceID, jid)
}

func (r *deviceChatStorage) GetChatByDevice(deviceID, jid string) (*domainChatStorage.Chat, error) {
	return r.base.GetChatByDevice(deviceID, jid)
}

func (r *deviceChatStorage) GetChats(filter *domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	if filter != nil && filter.DeviceID == "" {
		filter.DeviceID = r.deviceID
	}
	return r.base.GetChats(filter)
}

func (r *deviceChatStorage) DeleteChat(jid string) error {
	return r.base.DeleteChatByDevice(r.deviceID, jid)
}

func (r *deviceChatStorage) DeleteChatByDevice(deviceID, jid string) error {
	return r.base.DeleteChatByDevice(deviceID, jid)
}

func (r *deviceChatStorage) StoreMessage(message *domainChatStorage.Message) error {
	return r.base.StoreMessage(message)
}

func (r *deviceChatStorage) StoreMessagesBatch(messages []*domainChatStorage.Message) error {
	return r.base.StoreMessagesBatch(messages)
}

func (r *deviceChatStorage) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	return r.base.GetMessageByID(id)
}

func (r *deviceChatStorage) GetMessages(filter *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	if filter != nil && filter.DeviceID == "" {
		filter.DeviceID = r.deviceID
	}
	return r.base.GetMessages(filter)
}

func (r *deviceChatStorage) SearchMessages(deviceID, chatJID, searchText string, limit int) ([]*domainChatStorage.Message, error) {
	targetDeviceID := deviceID
	if targetDeviceID == "" {
		targetDeviceID = r.deviceID
	}
	return r.base.SearchMessages(targetDeviceID, chatJID, searchText, limit)
}

func (r *deviceChatStorage) DeleteMessage(id, chatJID string) error {
	return r.base.DeleteMessageByDevice(r.deviceID, id, chatJID)
}

func (r *deviceChatStorage) DeleteMessageByDevice(deviceID, id, chatJID string) error {
	return r.base.DeleteMessageByDevice(deviceID, id, chatJID)
}

func (r *deviceChatStorage) StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time) error {
	return r.base.StoreSentMessageWithContext(ctx, messageID, senderJID, recipientJID, content, timestamp)
}

func (r *deviceChatStorage) GetChatMessageCount(chatJID string) (int64, error) {
	return r.base.GetChatMessageCountByDevice(r.deviceID, chatJID)
}

func (r *deviceChatStorage) GetChatMessageCountByDevice(deviceID, chatJID string) (int64, error) {
	return r.base.GetChatMessageCountByDevice(deviceID, chatJID)
}

func (r *deviceChatStorage) GetTotalMessageCount() (int64, error) {
	return r.base.GetTotalMessageCount()
}

func (r *deviceChatStorage) GetTotalChatCount() (int64, error) {
	return r.base.GetTotalChatCount()
}

func (r *deviceChatStorage) GetChatNameWithPushName(jid types.JID, chatJID string, senderUser string, pushName string) string {
	return r.base.GetChatNameWithPushNameByDevice(r.deviceID, jid, chatJID, senderUser, pushName)
}

func (r *deviceChatStorage) GetChatNameWithPushNameByDevice(deviceID string, jid types.JID, chatJID string, senderUser string, pushName string) string {
	return r.base.GetChatNameWithPushNameByDevice(deviceID, jid, chatJID, senderUser, pushName)
}

func (r *deviceChatStorage) GetStorageStatistics() (chatCount int64, messageCount int64, err error) {
	return r.base.GetStorageStatistics()
}

func (r *deviceChatStorage) TruncateAllChats() error {
	return r.base.TruncateAllChats()
}

func (r *deviceChatStorage) TruncateAllDataWithLogging(logPrefix string) error {
	return r.base.TruncateAllDataWithLogging(logPrefix)
}

func (r *deviceChatStorage) InitializeSchema() error {
	return r.base.InitializeSchema()
}

func (r *deviceChatStorage) DeleteDeviceData(deviceID string) error {
	if r.base == nil {
		return nil
	}
	target := deviceID
	if target == "" {
		target = r.deviceID
	}
	return r.base.DeleteDeviceData(target)
}

func (r *deviceChatStorage) SaveDeviceRecord(record *domainChatStorage.DeviceRecord) error {
	return r.base.SaveDeviceRecord(record)
}

func (r *deviceChatStorage) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	return r.base.ListDeviceRecords()
}

func (r *deviceChatStorage) GetDeviceRecord(deviceID string) (*domainChatStorage.DeviceRecord, error) {
	return r.base.GetDeviceRecord(deviceID)
}

func (r *deviceChatStorage) DeleteDeviceRecord(deviceID string) error {
	return r.base.DeleteDeviceRecord(deviceID)
}
