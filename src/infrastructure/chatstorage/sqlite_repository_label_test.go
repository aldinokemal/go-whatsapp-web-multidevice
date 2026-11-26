package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLabelRoundTripAndIsolation(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceA, deviceB := "device-a@s.whatsapp.net", "device-b@s.whatsapp.net"
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{
		DeviceID: deviceA, ID: "5", Name: "Family", Color: 3, Type: "CUSTOM", OrderIndex: 1, Active: true, UpdatedAt: at,
	}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: deviceA, ID: "5", Name: "Family 2", Color: 4, Type: "CUSTOM", Active: true, UpdatedAt: at}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: deviceB, ID: "5", Name: "Other device", Active: true}))

	got, err := repo.GetLabel(deviceA, "5")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Family 2", got.Name)
	assert.Equal(t, int32(4), got.Color)
	assert.Equal(t, "CUSTOM", got.Type)
	assert.True(t, got.Active)
	assert.True(t, got.UpdatedAt.Equal(at))

	missing, err := repo.GetLabel(deviceA, "404")
	require.NoError(t, err)
	assert.Nil(t, missing)

	labels, err := repo.GetLabels(deviceA)
	require.NoError(t, err)
	require.Len(t, labels, 1, "labels leaked across devices")

	_, err = repo.GetLabels("")
	require.Error(t, err)
}

func TestSetLabelChatAddsAndRemoves(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	device := "device-a@s.whatsapp.net"

	require.NoError(t, repo.SetLabelChat(device, "5", "628111@s.whatsapp.net", true, time.Time{}))
	require.NoError(t, repo.SetLabelChat(device, "5", "628111@s.whatsapp.net", true, time.Time{}))
	require.NoError(t, repo.SetLabelChat(device, "5", "628222@s.whatsapp.net", true, time.Time{}))
	require.NoError(t, repo.SetLabelChat("device-b@s.whatsapp.net", "5", "628333@s.whatsapp.net", true, time.Time{}))
	require.NoError(t, repo.SetLabelChat(device, "5", "628222@s.whatsapp.net", false, time.Time{}))

	chats, err := repo.GetLabelChats(device)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	assert.Equal(t, "5", chats[0].LabelID)
	assert.Equal(t, "628111@s.whatsapp.net", chats[0].ChatJID)

	require.Error(t, repo.SetLabelChat(device, "", "628111@s.whatsapp.net", true, time.Time{}))
}

func TestReplaceLabelsKeepsOtherDevices(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceA, deviceB := "device-a@s.whatsapp.net", "device-b@s.whatsapp.net"

	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: deviceA, ID: "1", Name: "Stale", Active: true}))
	require.NoError(t, repo.SetLabelChat(deviceA, "1", "628111@s.whatsapp.net", true, time.Time{}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: deviceB, ID: "1", Name: "Kept", Active: true}))
	require.NoError(t, repo.SetLabelChat(deviceB, "1", "628999@s.whatsapp.net", true, time.Time{}))

	require.NoError(t, repo.ReplaceLabels(deviceA,
		[]*domainChatStorage.Label{{ID: "2", Name: "Fresh", Active: true}},
		[]*domainChatStorage.LabelChat{{LabelID: "2", ChatJID: "628222@s.whatsapp.net"}},
	))

	labels, err := repo.GetLabels(deviceA)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	assert.Equal(t, "2", labels[0].ID)
	assert.Equal(t, deviceA, labels[0].DeviceID)
	chats, err := repo.GetLabelChats(deviceA)
	require.NoError(t, err)
	require.Len(t, chats, 1)
	assert.Equal(t, "628222@s.whatsapp.net", chats[0].ChatJID)

	other, err := repo.GetLabels(deviceB)
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, "Kept", other[0].Name)
	otherChats, err := repo.GetLabelChats(deviceB)
	require.NoError(t, err)
	require.Len(t, otherChats, 1)
}

func TestDeviceCleanupRemovesLabels(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceA, deviceB := "device-a@s.whatsapp.net", "device-b@s.whatsapp.net"
	for _, device := range []string{deviceA, deviceB} {
		require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: device, ID: "1", Name: "Paid", Active: true}))
		require.NoError(t, repo.SetLabelChat(device, "1", "628111@s.whatsapp.net", true, time.Time{}))
	}

	require.NoError(t, repo.DeleteDeviceData(deviceA))
	labels, err := repo.GetLabels(deviceA)
	require.NoError(t, err)
	assert.Empty(t, labels)
	chats, err := repo.GetLabelChats(deviceA)
	require.NoError(t, err)
	assert.Empty(t, chats)
	labels, err = repo.GetLabels(deviceB)
	require.NoError(t, err)
	assert.Len(t, labels, 1)

	require.NoError(t, repo.TruncateAllChats())
	labels, err = repo.GetLabels(deviceB)
	require.NoError(t, err)
	assert.Empty(t, labels)
	chats, err = repo.GetLabelChats(deviceB)
	require.NoError(t, err)
	assert.Empty(t, chats)
}

func TestDeletingLabelDropsItsChats(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	device := "device-a@s.whatsapp.net"
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: device, ID: "1", Name: "Paid", Active: true}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: device, ID: "2", Name: "Lead", Active: true}))
	require.NoError(t, repo.SetLabelChat(device, "1", "628111@s.whatsapp.net", true, time.Time{}))
	require.NoError(t, repo.SetLabelChat(device, "2", "628222@s.whatsapp.net", true, time.Time{}))

	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: device, ID: "1", Name: "Paid", Deleted: true}))

	got, err := repo.GetLabel(device, "1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Deleted)
	chats, err := repo.GetLabelChats(device)
	require.NoError(t, err)
	require.Len(t, chats, 1, "a reused label ID must not inherit old chats")
	assert.Equal(t, "2", chats[0].LabelID)
}
