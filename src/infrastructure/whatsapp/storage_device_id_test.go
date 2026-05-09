package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type recordingChatStorageRepo struct {
	lastChat *domainChatStorage.Chat
}

func (r *recordingChatStorageRepo) CreateMessage(context.Context, *events.Message) error { return nil }
func (r *recordingChatStorageRepo) CreateIncomingCallRecord(context.Context, *events.CallOffer, bool) error {
	return nil
}
func (r *recordingChatStorageRepo) StoreChat(chat *domainChatStorage.Chat) error {
	r.lastChat = chat
	return nil
}
func (r *recordingChatStorageRepo) GetChat(string) (*domainChatStorage.Chat, error) { return nil, nil }
func (r *recordingChatStorageRepo) GetChatByDevice(string, string) (*domainChatStorage.Chat, error) {
	return nil, nil
}
func (r *recordingChatStorageRepo) GetChats(*domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	return nil, nil
}
func (r *recordingChatStorageRepo) DeleteChat(string) error                               { return nil }
func (r *recordingChatStorageRepo) DeleteChatByDevice(string, string) error               { return nil }
func (r *recordingChatStorageRepo) StoreMessage(*domainChatStorage.Message) error         { return nil }
func (r *recordingChatStorageRepo) StoreMessagesBatch([]*domainChatStorage.Message) error { return nil }
func (r *recordingChatStorageRepo) GetMessageByID(string) (*domainChatStorage.Message, error) {
	return nil, nil
}
func (r *recordingChatStorageRepo) GetMessages(*domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return nil, nil
}
func (r *recordingChatStorageRepo) SearchMessages(string, string, string, int) ([]*domainChatStorage.Message, error) {
	return nil, nil
}
func (r *recordingChatStorageRepo) DeleteMessage(string, string) error                 { return nil }
func (r *recordingChatStorageRepo) DeleteMessageByDevice(string, string, string) error { return nil }
func (r *recordingChatStorageRepo) StoreSentMessageWithContext(context.Context, string, string, string, string, time.Time, *waE2E.Message) error {
	return nil
}
func (r *recordingChatStorageRepo) GetChatMessageCount(string) (int64, error) { return 0, nil }
func (r *recordingChatStorageRepo) GetChatMessageCountByDevice(string, string) (int64, error) {
	return 0, nil
}
func (r *recordingChatStorageRepo) GetTotalMessageCount() (int64, error) { return 0, nil }
func (r *recordingChatStorageRepo) GetTotalChatCount() (int64, error)    { return 0, nil }
func (r *recordingChatStorageRepo) GetFilteredChatCount(*domainChatStorage.ChatFilter) (int64, error) {
	return 0, nil
}
func (r *recordingChatStorageRepo) GetChatNameWithPushName(types.JID, string, string, string) string {
	return ""
}
func (r *recordingChatStorageRepo) GetChatNameWithPushNameByDevice(string, types.JID, string, string, string) string {
	return ""
}
func (r *recordingChatStorageRepo) GetStorageStatistics() (int64, int64, error) { return 0, 0, nil }
func (r *recordingChatStorageRepo) TruncateAllChats() error                     { return nil }
func (r *recordingChatStorageRepo) TruncateAllDataWithLogging(string) error     { return nil }
func (r *recordingChatStorageRepo) DeleteDeviceData(string) error               { return nil }
func (r *recordingChatStorageRepo) SaveDeviceRecord(*domainChatStorage.DeviceRecord) error {
	return nil
}
func (r *recordingChatStorageRepo) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	return nil, nil
}
func (r *recordingChatStorageRepo) GetDeviceRecord(string) (*domainChatStorage.DeviceRecord, error) {
	return nil, nil
}
func (r *recordingChatStorageRepo) DeleteDeviceRecord(string) error { return nil }
func (r *recordingChatStorageRepo) InitializeSchema() error         { return nil }

func TestNormalizeStorageDeviceID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips device suffix from jid",
			input:    "6289605618749:11@s.whatsapp.net",
			expected: "6289605618749@s.whatsapp.net",
		},
		{
			name:     "keeps placeholder ids untouched",
			input:    "placeholder-device",
			expected: "placeholder-device",
		},
		{
			name:     "trims whitespace",
			input:    " 6289605618749:11@s.whatsapp.net ",
			expected: "6289605618749@s.whatsapp.net",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeStorageDeviceID(tt.input); got != tt.expected {
				t.Fatalf("normalizeStorageDeviceID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRefreshChatStorageUsesNormalizedDeviceID(t *testing.T) {
	tests := []struct {
		name          string
		instanceID    string
		instanceJID   string
		expectedStore string
	}{
		{
			name:          "prefers current instance jid over placeholder id",
			instanceID:    "placeholder-device",
			instanceJID:   "6289605618749@s.whatsapp.net",
			expectedStore: "6289605618749@s.whatsapp.net",
		},
		{
			name:          "normalizes raw jid from instance id when jid is empty",
			instanceID:    "6289605618749:11@s.whatsapp.net",
			instanceJID:   "",
			expectedStore: "6289605618749@s.whatsapp.net",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &recordingChatStorageRepo{}
			manager := &DeviceManager{storage: repo}
			inst := &DeviceInstance{id: tt.instanceID, jid: tt.instanceJID}

			manager.refreshChatStorage(inst)

			chatRepo := inst.GetChatStorage()
			if chatRepo == nil {
				t.Fatal("expected chat storage to be set")
			}

			chatJID := "12345@s.whatsapp.net"
			if err := chatRepo.StoreChat(&domainChatStorage.Chat{
				JID:             chatJID,
				Name:            "Test Chat",
				LastMessageTime: time.Now(),
			}); err != nil {
				t.Fatalf("store chat: %v", err)
			}

			if repo.lastChat == nil {
				t.Fatal("expected StoreChat to be called")
			}
			if repo.lastChat.DeviceID != tt.expectedStore {
				t.Fatalf("stored device_id = %q, want %q", repo.lastChat.DeviceID, tt.expectedStore)
			}
		})
	}
}
