package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestListDevices_SortsByCreatedAtAscending(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	// Create devices with different creation times (in random order)
	now := time.Now()
	devices := []*DeviceInstance{
		{id: "device-c", createdAt: now.Add(2 * time.Hour)},
		{id: "device-a", createdAt: now},
		{id: "device-b", createdAt: now.Add(1 * time.Hour)},
	}

	// Add in the given order (which is not sorted by createdAt)
	for _, d := range devices {
		manager.devices[d.id] = d
	}

	// Get list multiple times to verify consistent sorting
	for i := 0; i < 10; i++ {
		result := manager.ListDevices()

		// Verify order: device-a, device-b, device-c (oldest to newest)
		if len(result) != 3 {
			t.Fatalf("iteration %d: expected 3 devices, got %d", i, len(result))
		}
		if result[0].ID() != "device-a" {
			t.Errorf("iteration %d: expected first device to be device-a, got %s", i, result[0].ID())
		}
		if result[1].ID() != "device-b" {
			t.Errorf("iteration %d: expected second device to be device-b, got %s", i, result[1].ID())
		}
		if result[2].ID() != "device-c" {
			t.Errorf("iteration %d: expected third device to be device-c, got %s", i, result[2].ID())
		}
	}
}

func TestListDevices_EmptyList(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	result := manager.ListDevices()

	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d devices", len(result))
	}
}

func TestListDevices_SingleDevice(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	device := &DeviceInstance{id: "only-device", createdAt: time.Now()}
	manager.devices[device.id] = device

	result := manager.ListDevices()

	if len(result) != 1 {
		t.Fatalf("expected 1 device, got %d", len(result))
	}
	if result[0].ID() != "only-device" {
		t.Errorf("expected device id to be only-device, got %s", result[0].ID())
	}
}

func TestListDevices_SameCreatedAt(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	// Devices with same creation time should be sorted by ID as tie-breaker
	sameTime := time.Now()
	devices := []*DeviceInstance{
		{id: "device-3", createdAt: sameTime},
		{id: "device-1", createdAt: sameTime},
		{id: "device-2", createdAt: sameTime},
	}

	for _, d := range devices {
		manager.devices[d.id] = d
	}

	expectedOrder := []string{"device-1", "device-2", "device-3"}

	// Call ListDevices multiple times to verify consistent ordering
	for i := 0; i < 10; i++ {
		result := manager.ListDevices()

		if len(result) != 3 {
			t.Fatalf("iteration %d: expected 3 devices, got %d", i, len(result))
		}

		// Verify order: devices should be sorted by ID when createdAt is equal
		for j, expected := range expectedOrder {
			if result[j].ID() != expected {
				t.Errorf("iteration %d: expected device at index %d to be %s, got %s",
					i, j, expected, result[j].ID())
			}
		}
	}
}

func TestRefreshChatStorage_RebindsPlaceholderToRealJID(t *testing.T) {
	base := &recordingChatStorage{}
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
		storage: base,
	}

	placeholderID := "placeholder-device"
	instance := NewDeviceInstance(placeholderID, nil, newDeviceChatStorage(placeholderID, base))
	manager.devices[placeholderID] = instance

	realJID, err := types.ParseJID("6289605618749@s.whatsapp.net")
	require.NoError(t, err)

	instance.SetClient(&whatsmeow.Client{Store: &store.Device{ID: &realJID}})
	instance.UpdateStateFromClient()

	require.Equal(t, realJID.String(), instance.JID())

	_, err = instance.GetChatStorage().GetChat("chat-before-refresh")
	require.NoError(t, err)
	require.Equal(t, placeholderID, base.lastGetChatDeviceID)

	manager.refreshChatStorage(instance)

	_, err = instance.GetChatStorage().GetChat("chat-after-refresh")
	require.NoError(t, err)
	require.Equal(t, realJID.String(), base.lastGetChatDeviceID)
}

type recordingChatStorage struct {
	lastGetChatDeviceID string
}

func (r *recordingChatStorage) CreateMessage(ctx context.Context, evt *events.Message) error {
	return nil
}

func (r *recordingChatStorage) CreateReaction(ctx context.Context, evt *events.Message) error {
	return nil
}

func (r *recordingChatStorage) CreateIncomingCallRecord(ctx context.Context, evt *events.CallOffer, autoRejected bool) error {
	return nil
}

func (r *recordingChatStorage) StoreChat(chat *domainChatStorage.Chat) error {
	return nil
}

func (r *recordingChatStorage) GetChat(jid string) (*domainChatStorage.Chat, error) {
	return r.GetChatByDevice("", jid)
}

func (r *recordingChatStorage) GetChatByDevice(deviceID, jid string) (*domainChatStorage.Chat, error) {
	r.lastGetChatDeviceID = deviceID
	return nil, nil
}

func (r *recordingChatStorage) GetChats(filter *domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	return nil, nil
}

func (r *recordingChatStorage) DeleteChat(jid string) error {
	return nil
}

func (r *recordingChatStorage) DeleteChatByDevice(deviceID, jid string) error {
	return nil
}

func (r *recordingChatStorage) StoreMessage(message *domainChatStorage.Message) error {
	return nil
}

func (r *recordingChatStorage) StoreMessagesBatch(messages []*domainChatStorage.Message) error {
	return nil
}

func (r *recordingChatStorage) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	return nil, nil
}

func (r *recordingChatStorage) GetMessages(filter *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return nil, nil
}

func (r *recordingChatStorage) SearchMessages(deviceID, chatJID, searchText string, limit int) ([]*domainChatStorage.Message, error) {
	return nil, nil
}

func (r *recordingChatStorage) DeleteMessage(id, chatJID string) error {
	return nil
}

func (r *recordingChatStorage) DeleteMessageByDevice(deviceID, id, chatJID string) error {
	return nil
}

func (r *recordingChatStorage) StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time, msg *waE2E.Message) error {
	return nil
}

func (r *recordingChatStorage) GetChatMessageCount(chatJID string) (int64, error) {
	return 0, nil
}

func (r *recordingChatStorage) GetChatMessageCountByDevice(deviceID, chatJID string) (int64, error) {
	return 0, nil
}

func (r *recordingChatStorage) GetTotalMessageCount() (int64, error) {
	return 0, nil
}

func (r *recordingChatStorage) GetTotalChatCount() (int64, error) {
	return 0, nil
}

func (r *recordingChatStorage) GetFilteredChatCount(filter *domainChatStorage.ChatFilter) (int64, error) {
	return 0, nil
}

func (r *recordingChatStorage) GetChatNameWithPushName(jid types.JID, chatJID string, senderUser string, pushName string) string {
	return pushName
}

func (r *recordingChatStorage) GetChatNameWithPushNameByDevice(deviceID string, jid types.JID, chatJID string, senderUser string, pushName string) string {
	return pushName
}

func (r *recordingChatStorage) GetStorageStatistics() (chatCount int64, messageCount int64, err error) {
	return 0, 0, nil
}

func (r *recordingChatStorage) TruncateAllChats() error {
	return nil
}

func (r *recordingChatStorage) TruncateAllDataWithLogging(logPrefix string) error {
	return nil
}

func (r *recordingChatStorage) DeleteDeviceData(deviceID string) error {
	return nil
}

func (r *recordingChatStorage) SaveDeviceRecord(record *domainChatStorage.DeviceRecord) error {
	return nil
}

func (r *recordingChatStorage) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	return nil, nil
}

func (r *recordingChatStorage) GetDeviceRecord(deviceID string) (*domainChatStorage.DeviceRecord, error) {
	return nil, nil
}

func (r *recordingChatStorage) DeleteDeviceRecord(deviceID string) error {
	return nil
}

func (r *recordingChatStorage) InitializeSchema() error {
	return nil
}
