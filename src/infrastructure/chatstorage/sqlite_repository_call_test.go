package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
)

func seedCallRecord(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, messageID, metadata string, timestamp time.Time) {
	t.Helper()
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            chatJID,
		LastMessageTime: timestamp,
	}))
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID:           messageID,
		ChatJID:      chatJID,
		DeviceID:     deviceID,
		Sender:       chatJID,
		Content:      "Incoming call",
		Timestamp:    timestamp,
		MediaType:    "call",
		CallMetadata: metadata,
	}))
}

func TestGetCallRecordsReturnsOnlyCallRowsForDeviceNewestFirst(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	otherDeviceID := "device-b@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	base := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)

	seedChatMessage(t, repo, deviceID, chatJID, "text-1", "hello", base)
	seedCallRecord(t, repo, deviceID, chatJID, "call:older", `{"call_id":"older","auto_rejected":false}`, base)
	seedCallRecord(t, repo, deviceID, chatJID, "call:newer", `{"call_id":"newer","auto_rejected":true}`, base.Add(time.Hour))
	seedCallRecord(t, repo, otherDeviceID, chatJID, "call:other-device", `{"call_id":"other"}`, base.Add(2*time.Hour))

	records, total, err := repo.GetCallRecords(&domainChatStorage.CallRecordFilter{
		DeviceID: deviceID,
		Limit:    25,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, records, 2)
	require.Equal(t, "call:newer", records[0].ID)
	require.Equal(t, "call:older", records[1].ID)
	for _, record := range records {
		require.Equal(t, "call", record.MediaType)
		require.Equal(t, deviceID, record.DeviceID)
	}
	require.Equal(t, `{"call_id":"newer","auto_rejected":true}`, records[0].CallMetadata)
}

func TestGetCallRecordsRespectsLimitOffsetAndChatFilter(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	otherChatJID := "628987654321@s.whatsapp.net"
	base := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)

	seedCallRecord(t, repo, deviceID, chatJID, "call:1", "", base)
	seedCallRecord(t, repo, deviceID, chatJID, "call:2", "", base.Add(time.Hour))
	seedCallRecord(t, repo, deviceID, chatJID, "call:3", "", base.Add(2*time.Hour))
	seedCallRecord(t, repo, deviceID, otherChatJID, "call:4", "", base.Add(3*time.Hour))

	page, total, err := repo.GetCallRecords(&domainChatStorage.CallRecordFilter{
		DeviceID: deviceID,
		Limit:    2,
		Offset:   1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(4), total)
	require.Len(t, page, 2)
	require.Equal(t, "call:3", page[0].ID)
	require.Equal(t, "call:2", page[1].ID)

	filtered, filteredTotal, err := repo.GetCallRecords(&domainChatStorage.CallRecordFilter{
		DeviceID: deviceID,
		ChatJID:  otherChatJID,
		Limit:    25,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), filteredTotal)
	require.Len(t, filtered, 1)
	require.Equal(t, "call:4", filtered[0].ID)
}

func TestGetCallRecordsPaginatesDeterministicallyWithEqualTimestamps(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	same := time.Date(2026, time.August, 22, 9, 0, 0, 0, time.UTC)

	seedCallRecord(t, repo, deviceID, chatJID, "call:1", "", same)
	seedCallRecord(t, repo, deviceID, chatJID, "call:2", "", same)
	seedCallRecord(t, repo, deviceID, chatJID, "call:3", "", same)
	seedCallRecord(t, repo, deviceID, chatJID, "call:4", "", same)
	seedCallRecord(t, repo, deviceID, chatJID, "call:5", "", same)

	page1, total, err := repo.GetCallRecords(&domainChatStorage.CallRecordFilter{
		DeviceID: deviceID,
		Limit:    3,
		Offset:   0,
	})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, page1, 3)

	page2, total, err := repo.GetCallRecords(&domainChatStorage.CallRecordFilter{
		DeviceID: deviceID,
		Limit:    3,
		Offset:   3,
	})
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, page2, 2)

	seen := make(map[string]bool)
	var ids []string
	for _, record := range append(append([]*domainChatStorage.Message{}, page1...), page2...) {
		require.Falsef(t, seen[record.ID], "record %s duplicated across pages", record.ID)
		seen[record.ID] = true
		ids = append(ids, record.ID)
	}
	require.Equal(t, []string{"call:5", "call:4", "call:3", "call:2", "call:1"}, ids)
}

func TestGetCallRecordsRequiresDeviceID(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	_, _, err := repo.GetCallRecords(&domainChatStorage.CallRecordFilter{Limit: 25})
	require.Error(t, err)

	_, _, err = repo.GetCallRecords(nil)
	require.Error(t, err)
}
