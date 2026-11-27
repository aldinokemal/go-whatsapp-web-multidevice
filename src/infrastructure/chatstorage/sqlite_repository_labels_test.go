package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
)

const (
	labelDeviceA = "628111111111@s.whatsapp.net"
	labelDeviceB = "628222222222@s.whatsapp.net"
)

func labelChatJIDs(t *testing.T, repo *SQLiteRepository, deviceID, labelID string) []string {
	t.Helper()
	associations, err := repo.ListLabelAssociations(deviceID, labelID)
	require.NoError(t, err)
	jids := make([]string, 0, len(associations))
	for _, association := range associations {
		jids = append(jids, association.LabelID+"/"+association.ChatJID)
	}
	return jids
}

func TestLabelUpsertReplacesTheWholeLabel(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{
		DeviceID: labelDeviceA, LabelID: "5", Name: "Paid", Color: 3, Type: "CUSTOM",
		OrderIndex: 2, IsActive: true, PredefinedID: 4,
	}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{
		DeviceID: labelDeviceA, LabelID: "5", Name: "Paid in full", Color: 7, OrderIndex: 1,
	}))

	labels, err := repo.ListLabels(labelDeviceA)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	label := labels[0]
	require.Equal(t, "Paid in full", label.Name)
	require.Equal(t, int32(7), label.Color)
	require.Equal(t, int32(1), label.OrderIndex)
	require.Empty(t, label.Type)
	require.Zero(t, label.PredefinedID)
	require.False(t, label.IsActive)
}

func TestLabelsAreOrderedAndScopedByDevice(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	for _, label := range []*domainChatStorage.Label{
		{DeviceID: labelDeviceA, LabelID: "7", Name: "Second", OrderIndex: 2},
		{DeviceID: labelDeviceA, LabelID: "6", Name: "First", OrderIndex: 1},
		{DeviceID: labelDeviceB, LabelID: "6", Name: "Other device", OrderIndex: 0},
	} {
		require.NoError(t, repo.UpsertLabel(label))
	}

	labels, err := repo.ListLabels(labelDeviceA)
	require.NoError(t, err)
	require.Len(t, labels, 2)
	require.Equal(t, "First", labels[0].Name)
	require.Equal(t, "Second", labels[1].Name)
}

func TestLabelAssociationsFollowTheirLabel(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDeviceA, LabelID: "5", Name: "Paid"}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDeviceA, LabelID: "6", Name: "Lead"}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDeviceB, LabelID: "5", Name: "Paid"}))
	for _, association := range []*domainChatStorage.LabelAssociation{
		{DeviceID: labelDeviceA, LabelID: "5", ChatJID: "628333333333@s.whatsapp.net"},
		{DeviceID: labelDeviceA, LabelID: "5", ChatJID: "123456789012345@lid"},
		{DeviceID: labelDeviceA, LabelID: "6", ChatJID: "628333333333@s.whatsapp.net"},
		// An association can arrive before its label, or after the label was deleted.
		{DeviceID: labelDeviceA, LabelID: "9", ChatJID: "628444444444@s.whatsapp.net"},
		{DeviceID: labelDeviceB, LabelID: "5", ChatJID: "628555555555@s.whatsapp.net"},
	} {
		require.NoError(t, repo.UpsertLabelAssociation(association))
	}
	require.NoError(t, repo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{
		DeviceID: labelDeviceA, LabelID: "5", ChatJID: "628333333333@s.whatsapp.net",
	}), "a repeated association is idempotent")

	require.Equal(t, []string{"5/123456789012345@lid", "5/628333333333@s.whatsapp.net", "6/628333333333@s.whatsapp.net"},
		labelChatJIDs(t, repo, labelDeviceA, ""))
	require.Equal(t, []string{"5/123456789012345@lid", "5/628333333333@s.whatsapp.net"}, labelChatJIDs(t, repo, labelDeviceA, "5"))
	require.Empty(t, labelChatJIDs(t, repo, labelDeviceA, "9"), "chats of an unknown label are hidden")

	require.NoError(t, repo.DeleteLabelAssociation(labelDeviceA, "5", "123456789012345@lid"))
	require.Equal(t, []string{"5/628333333333@s.whatsapp.net"}, labelChatJIDs(t, repo, labelDeviceA, "5"))

	require.NoError(t, repo.DeleteLabel(labelDeviceA, "5"))
	labels, err := repo.ListLabels(labelDeviceA)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	require.Equal(t, "6", labels[0].LabelID)
	require.Equal(t, []string{"6/628333333333@s.whatsapp.net"}, labelChatJIDs(t, repo, labelDeviceA, ""))
	require.Equal(t, []string{"5/628555555555@s.whatsapp.net"}, labelChatJIDs(t, repo, labelDeviceB, ""),
		"deleting a label leaves other devices alone")
}

func TestDeleteLabelDataBeforeKeepsRowsWrittenSinceTheCutoff(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDeviceA, LabelID: "5", Name: "Paid"}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDeviceA, LabelID: "6", Name: "Gone"}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDeviceB, LabelID: "6", Name: "Other device"}))
	require.NoError(t, repo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{DeviceID: labelDeviceA, LabelID: "5", ChatJID: "628333333333@s.whatsapp.net"}))
	require.NoError(t, repo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{DeviceID: labelDeviceA, LabelID: "5", ChatJID: "628444444444@s.whatsapp.net"}))

	time.Sleep(5 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(5 * time.Millisecond)
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDeviceA, LabelID: "5", Name: "Paid"}))
	require.NoError(t, repo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{DeviceID: labelDeviceA, LabelID: "5", ChatJID: "628333333333@s.whatsapp.net"}))

	require.NoError(t, repo.DeleteLabelDataBefore(labelDeviceA, cutoff))

	labels, err := repo.ListLabels(labelDeviceA)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	require.Equal(t, "5", labels[0].LabelID)
	require.Equal(t, []string{"5/628333333333@s.whatsapp.net"}, labelChatJIDs(t, repo, labelDeviceA, ""))
	other, err := repo.ListLabels(labelDeviceB)
	require.NoError(t, err)
	require.Len(t, other, 1)
}

func TestDeviceCleanupRemovesLabels(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	for _, deviceID := range []string{labelDeviceA, labelDeviceB} {
		require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: deviceID, LabelID: "5", Name: "Paid"}))
		require.NoError(t, repo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{DeviceID: deviceID, LabelID: "5", ChatJID: "628333333333@s.whatsapp.net"}))
	}

	require.NoError(t, repo.DeleteDeviceData(labelDeviceA))
	labels, err := repo.ListLabels(labelDeviceA)
	require.NoError(t, err)
	require.Empty(t, labels)
	var orphans int
	require.NoError(t, repo.db.QueryRow(`SELECT COUNT(*) FROM label_chats WHERE device_id = ?`, labelDeviceA).Scan(&orphans))
	require.Zero(t, orphans)
	require.Len(t, labelChatJIDs(t, repo, labelDeviceB, ""), 1)

	require.NoError(t, repo.TruncateAllChats())
	labels, err = repo.ListLabels(labelDeviceB)
	require.NoError(t, err)
	require.Empty(t, labels)
	require.Empty(t, labelChatJIDs(t, repo, labelDeviceB, ""))
}
