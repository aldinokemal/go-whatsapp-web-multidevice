package chatstorage

import (
	"database/sql"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCallRepository(t *testing.T) *SQLiteRepository {
	t.Helper()
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&_foreign_keys=on")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo, ok := NewStorageRepository(db).(*SQLiteRepository)
	require.True(t, ok)
	require.NoError(t, repo.InitializeSchema())
	return repo
}

func TestStoreCallRecordScopesByDevice(t *testing.T) {
	repo := newTestCallRepository(t)
	startedAt := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, repo.StoreCallRecord(&domainChatStorage.CallRecord{
		DeviceID:  "device-a",
		CallID:    "call-1",
		PeerJID:   "5511999999999@s.whatsapp.net",
		Direction: "outbound",
		Status:    "ringing",
		MediaType: "audio",
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}))
	require.NoError(t, repo.StoreCallRecord(&domainChatStorage.CallRecord{
		DeviceID:  "device-b",
		CallID:    "call-1",
		PeerJID:   "5521888888888@s.whatsapp.net",
		Direction: "inbound",
		Status:    "ringing",
		MediaType: "audio",
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}))

	deviceACalls, err := repo.ListCallRecords("device-a", 20)
	require.NoError(t, err)
	require.Len(t, deviceACalls, 1)
	assert.Equal(t, "5511999999999@s.whatsapp.net", deviceACalls[0].PeerJID)

	deviceBCall, err := repo.GetCallRecord("device-b", "call-1")
	require.NoError(t, err)
	require.NotNil(t, deviceBCall)
	assert.Equal(t, "5521888888888@s.whatsapp.net", deviceBCall.PeerJID)
}

func TestStoreCallRecordUpdatesExistingCall(t *testing.T) {
	repo := newTestCallRepository(t)
	startedAt := time.Now().UTC().Truncate(time.Second)
	endedAt := startedAt.Add(30 * time.Second)

	require.NoError(t, repo.StoreCallRecord(&domainChatStorage.CallRecord{
		DeviceID:  "device-a",
		CallID:    "call-1",
		PeerJID:   "5511999999999@s.whatsapp.net",
		Direction: "outbound",
		Status:    "ringing",
		MediaType: "audio",
		StartedAt: startedAt,
		UpdatedAt: startedAt,
	}))
	require.NoError(t, repo.StoreCallRecord(&domainChatStorage.CallRecord{
		DeviceID:  "device-a",
		CallID:    "call-1",
		PeerJID:   "5511999999999@s.whatsapp.net",
		Direction: "outbound",
		Status:    "ended",
		MediaType: "audio",
		StartedAt: startedAt,
		UpdatedAt: endedAt,
		EndedAt:   &endedAt,
		EndReason: "user_ended",
		Metadata:  `{"source":"test"}`,
	}))

	got, err := repo.GetCallRecord("device-a", "call-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "ended", got.Status)
	assert.Equal(t, "user_ended", got.EndReason)
	assert.Equal(t, `{"source":"test"}`, got.Metadata)
	require.NotNil(t, got.EndedAt)
	assert.True(t, got.EndedAt.Equal(endedAt))
}
