package usecase

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

type labelLIDStub map[types.JID]types.JID

func (s labelLIDStub) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if pn, ok := s[lid]; ok {
		return pn, nil
	}
	return types.EmptyJID, errors.New("no mapping")
}

const labelDevice = "628111111111@s.whatsapp.net"

func newLabelService(t *testing.T) (serviceUser, domainChatStorage.IChatStorageRepository) {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())
	return serviceUser{chatStorageRepo: repo}, repo
}

func TestListLabelsCountsResolvedChats(t *testing.T) {
	service, repo := newLabelService(t)
	mappedLID := types.NewJID("123456789012345", types.HiddenUserServer)
	phone := types.NewJID("628333333333", types.DefaultUserServer)
	lids := labelLIDStub{mappedLID: phone}

	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDevice, LabelID: "5", Name: "Paid", Type: "CUSTOM", OrderIndex: 1, IsActive: true}))
	require.NoError(t, repo.UpsertLabel(&domainChatStorage.Label{DeviceID: labelDevice, LabelID: "6", Name: "Empty", OrderIndex: 2}))
	for _, chat := range []string{
		phone.String(),
		mappedLID.String(), // the same chat under its LID
		"999999999999999@lid",
		"120363000000000042@g.us",
	} {
		require.NoError(t, repo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{DeviceID: labelDevice, LabelID: "5", ChatJID: chat}))
	}
	require.NoError(t, repo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{DeviceID: "628222222222@s.whatsapp.net", LabelID: "5", ChatJID: "628444444444@s.whatsapp.net"}))

	response, err := service.listLabels(context.Background(), labelDevice, lids)
	require.NoError(t, err)
	require.Equal(t, []domainUser.MyListLabelsResponseData{
		{LabelID: "5", Name: "Paid", Type: "CUSTOM", OrderIndex: 1, IsActive: true, ChatCount: 3},
		{LabelID: "6", Name: "Empty", OrderIndex: 2, ChatCount: 0},
	}, response.Data)

	associations, err := repo.ListLabelAssociations(labelDevice, "5")
	require.NoError(t, err)
	require.Equal(t, []domainUser.MyListLabelChatsResponseData{
		{JID: "120363000000000042@g.us"},
		{JID: phone.String(), LID: mappedLID.String()},
		{JID: "999999999999999@lid", LID: "999999999999999@lid"},
	}, groupLabelChats(context.Background(), associations, lids)["5"])
}

func TestListLabelsWithoutStoredLabels(t *testing.T) {
	service, _ := newLabelService(t)
	response, err := service.listLabels(context.Background(), labelDevice, nil)
	require.NoError(t, err)
	require.NotNil(t, response.Data, "an empty list is returned as [] rather than null")
	require.Empty(t, response.Data)
}

func TestListLabelChatsRequiresLabelID(t *testing.T) {
	service, _ := newLabelService(t)
	_, err := service.MyListLabelChats(context.Background(), " ")
	require.Error(t, err)
}
