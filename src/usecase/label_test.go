package usecase

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
)

type labelRepoStub struct {
	domainChatStorage.IChatStorageRepository
	labels []*domainChatStorage.Label
	links  []*domainChatStorage.LabelChat
	device string
}

func (r *labelRepoStub) GetLabels(deviceID string) ([]*domainChatStorage.Label, error) {
	r.device = deviceID
	return r.labels, nil
}

func (r *labelRepoStub) GetLabelChats(string) ([]*domainChatStorage.LabelChat, error) {
	return r.links, nil
}

func TestListLabels(t *testing.T) {
	deviceID := "628999999999@s.whatsapp.net"
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	repo := &labelRepoStub{
		labels: []*domainChatStorage.Label{
			{ID: "1", Name: "Family", Color: 2, Type: "CUSTOM", OrderIndex: 1, Active: true, UpdatedAt: at},
			{ID: "2", Name: "Gone", Deleted: true},
			{ID: "3", Name: "Empty", Active: true},
		},
		links: []*domainChatStorage.LabelChat{
			{LabelID: "1", ChatJID: "628111111111@s.whatsapp.net"},
			{LabelID: "1", ChatJID: "120363025982934543@g.us"},
			{LabelID: "2", ChatJID: "628222222222@s.whatsapp.net"},
		},
	}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	resp, err := NewLabelService(repo).ListLabels(ctx)
	require.NoError(t, err)
	assert.Equal(t, deviceID, repo.device)
	require.Len(t, resp.Data, 2, "deleted labels are left out")
	assert.Equal(t, domainLabel.LabelInfo{
		ID: "1", Name: "Family", Color: 2, Type: "CUSTOM", OrderIndex: 1, Active: true,
		Chats:     []string{"628111111111@s.whatsapp.net", "120363025982934543@g.us"},
		UpdatedAt: formatTimestamp(at),
	}, resp.Data[0])
	assert.Equal(t, []string{}, resp.Data[1].Chats, "a label without chats lists an empty array")
}

func TestListLabelsRequiresDevice(t *testing.T) {
	_, err := NewLabelService(&labelRepoStub{}).ListLabels(context.Background())
	require.Error(t, err)
}

func TestLabelMutationsRequireClient(t *testing.T) {
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("628999999999@s.whatsapp.net", nil, nil))
	service := NewLabelService(&labelRepoStub{})
	labeled := true

	_, err := service.EditLabel(ctx, domainLabel.EditLabelRequest{LabelID: "30", Name: "Paid"})
	assert.Equal(t, pkgError.ErrWaCLI, err)
	_, err = service.LabelChat(ctx, domainLabel.LabelChatRequest{ChatJID: "628123456789@s.whatsapp.net", LabelID: "30", Labeled: &labeled})
	assert.Equal(t, pkgError.ErrWaCLI, err)
	_, err = service.ResyncLabels(ctx)
	assert.Equal(t, pkgError.ErrWaCLI, err)

	_, err = service.EditLabel(ctx, domainLabel.EditLabelRequest{LabelID: "30"})
	assert.IsType(t, pkgError.ValidationError(""), err, "validation runs before the client is needed")
}

func TestBuildLabelEditPatch(t *testing.T) {
	color := int32(5)
	order := int32(2)

	t.Run("Business label matches whatsmeow's BuildLabelEdit", func(t *testing.T) {
		patch, label := buildLabelEditPatch(domainLabel.EditLabelRequest{LabelID: "30", Name: "Paid", Color: &color}, nil)
		assert.Equal(t, appstate.BuildLabelEdit("30", "Paid", 5, false), patch)
		assert.Equal(t, &domainChatStorage.Label{ID: "30", Name: "Paid", Color: 5, Active: true}, label)
	})

	t.Run("chat list carries its type and active flag", func(t *testing.T) {
		patch, label := buildLabelEditPatch(domainLabel.EditLabelRequest{LabelID: "30", Name: "Family", Type: "CUSTOM", OrderIndex: &order}, nil)
		require.Len(t, patch.Mutations, 1)
		assert.Equal(t, appstate.WAPatchRegular, patch.Type)
		assert.Equal(t, []string{appstate.IndexLabelEdit, "30"}, patch.Mutations[0].Index)
		action := patch.Mutations[0].Value.GetLabelEditAction()
		assert.Equal(t, "Family", action.GetName())
		assert.Equal(t, waSyncAction.LabelEditAction_CUSTOM, action.GetType())
		assert.True(t, action.GetIsActive())
		assert.Equal(t, int32(2), action.GetOrderIndex())
		assert.False(t, action.GetDeleted())
		assert.Equal(t, "CUSTOM", label.Type)
	})

	t.Run("rename keeps the stored fields", func(t *testing.T) {
		stored := &domainChatStorage.Label{ID: "30", Name: "Family", Color: 7, Type: "CUSTOM", OrderIndex: 3, Active: true}
		patch, label := buildLabelEditPatch(domainLabel.EditLabelRequest{LabelID: "30", Name: "Close family"}, stored)
		action := patch.Mutations[0].Value.GetLabelEditAction()
		assert.Equal(t, "Close family", action.GetName())
		assert.Equal(t, int32(7), action.GetColor())
		assert.Equal(t, int32(3), action.GetOrderIndex())
		assert.Equal(t, waSyncAction.LabelEditAction_CUSTOM, action.GetType())
		assert.Equal(t, "CUSTOM", label.Type)
	})

	t.Run("edit keeps an inactive list inactive", func(t *testing.T) {
		stored := &domainChatStorage.Label{ID: "30", Name: "Family", Type: "CUSTOM", Active: false}
		patch, label := buildLabelEditPatch(domainLabel.EditLabelRequest{LabelID: "30", Name: "Close family"}, stored)
		action := patch.Mutations[0].Value.GetLabelEditAction()
		require.NotNil(t, action.IsActive)
		assert.False(t, action.GetIsActive())
		assert.False(t, label.Active)
	})

	t.Run("delete keeps the stored name", func(t *testing.T) {
		stored := &domainChatStorage.Label{ID: "30", Name: "Family", Type: "CUSTOM", Active: true}
		patch, label := buildLabelEditPatch(domainLabel.EditLabelRequest{LabelID: "30", Deleted: true}, stored)
		action := patch.Mutations[0].Value.GetLabelEditAction()
		assert.True(t, action.GetDeleted())
		assert.Equal(t, "Family", action.GetName())
		assert.True(t, label.Deleted)
	})
}
