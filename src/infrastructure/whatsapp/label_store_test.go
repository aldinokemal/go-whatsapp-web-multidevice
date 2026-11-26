package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type labelChatCall struct {
	deviceID, labelID, chatJID string
	labeled                    bool
}

type labelRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	labels []*domainChatStorage.Label
	chats  []labelChatCall
}

func (r *labelRepoSpy) UpsertLabel(label *domainChatStorage.Label) error {
	r.labels = append(r.labels, label)
	return nil
}

func (r *labelRepoSpy) SetLabelChat(deviceID, labelID, chatJID string, labeled bool, _ time.Time) error {
	r.chats = append(r.chats, labelChatCall{deviceID, labelID, chatJID, labeled})
	return nil
}

func labelEditEvent(labelID string, action *waSyncAction.LabelEditAction, ts int64) *events.AppState {
	return &events.AppState{
		Index:           []string{"label_edit", labelID},
		SyncActionValue: &waSyncAction.SyncActionValue{Timestamp: proto.Int64(ts), LabelEditAction: action},
	}
}

func labelChatEvent(labelID, chatJID string, labeled bool) *events.AppState {
	return &events.AppState{
		Index: []string{"label_jid", labelID, chatJID},
		SyncActionValue: &waSyncAction.SyncActionValue{
			LabelAssociationAction: &waSyncAction.LabelAssociationAction{Labeled: proto.Bool(labeled)},
		},
	}
}

func TestLabelFromAction(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	listType := waSyncAction.LabelEditAction_CUSTOM

	label := LabelFromAction("dev@s.whatsapp.net", "7", &waSyncAction.LabelEditAction{
		Name: proto.String("Family"), Color: proto.Int32(3), OrderIndex: proto.Int32(2), Type: &listType,
	}, at)
	assert.Equal(t, domainChatStorage.Label{
		DeviceID: "dev@s.whatsapp.net", ID: "7", Name: "Family", Color: 3, Type: "CUSTOM",
		OrderIndex: 2, Active: true, UpdatedAt: at,
	}, *label)

	inactive := LabelFromAction("dev@s.whatsapp.net", "8", &waSyncAction.LabelEditAction{
		Name: proto.String("Old"), IsActive: proto.Bool(false), Deleted: proto.Bool(true),
	}, at)
	assert.False(t, inactive.Active)
	assert.True(t, inactive.Deleted)
	assert.Empty(t, inactive.Type)
}

func TestStoreLabelAppState(t *testing.T) {
	ctx := context.Background()
	deviceID := "628999999999@s.whatsapp.net"

	t.Run("label edit", func(t *testing.T) {
		repo := &labelRepoSpy{}
		evt := labelEditEvent("5", &waSyncAction.LabelEditAction{Name: proto.String("Paid"), Color: proto.Int32(4)}, 1767323045000)
		require.NoError(t, storeLabelAppState(ctx, evt, deviceID, nil, repo))
		require.Len(t, repo.labels, 1)
		assert.Equal(t, "5", repo.labels[0].ID)
		assert.Equal(t, "Paid", repo.labels[0].Name)
		assert.Equal(t, deviceID, repo.labels[0].DeviceID)
		assert.Equal(t, time.UnixMilli(1767323045000).UTC(), repo.labels[0].UpdatedAt)
	})

	t.Run("chat added under its device", func(t *testing.T) {
		repo := &labelRepoSpy{}
		require.NoError(t, storeLabelAppState(ctx, labelChatEvent("5", "628123456789@s.whatsapp.net", true), deviceID, nil, repo))
		assert.Equal(t, []labelChatCall{{deviceID, "5", "628123456789@s.whatsapp.net", true}}, repo.chats)
	})

	t.Run("chat removed", func(t *testing.T) {
		repo := &labelRepoSpy{}
		require.NoError(t, storeLabelAppState(ctx, labelChatEvent("5", "628123456789@s.whatsapp.net", false), deviceID, nil, repo))
		assert.Equal(t, []labelChatCall{{deviceID, "5", "628123456789@s.whatsapp.net", false}}, repo.chats)
	})

	t.Run("unresolved LID keeps the JID as sent", func(t *testing.T) {
		repo := &labelRepoSpy{}
		require.NoError(t, storeLabelAppState(ctx, labelChatEvent("5", "123456789@lid", true), deviceID, nil, repo))
		assert.Equal(t, []labelChatCall{{deviceID, "5", "123456789@lid", true}}, repo.chats)
	})

	t.Run("ignored without a device or repository", func(t *testing.T) {
		repo := &labelRepoSpy{}
		evt := labelEditEvent("5", &waSyncAction.LabelEditAction{Name: proto.String("Paid")}, 0)
		require.NoError(t, storeLabelAppState(ctx, evt, "", nil, repo))
		require.NoError(t, storeLabelAppState(ctx, evt, deviceID, nil, nil))
		assert.Empty(t, repo.labels)
	})

	t.Run("message labels are not chat links", func(t *testing.T) {
		repo := &labelRepoSpy{}
		evt := &events.AppState{
			Index: []string{"label_message", "5", "628123456789@s.whatsapp.net", "MSG", "0", "0"},
			SyncActionValue: &waSyncAction.SyncActionValue{
				LabelAssociationAction: &waSyncAction.LabelAssociationAction{Labeled: proto.Bool(true)},
			},
		}
		require.NoError(t, storeLabelAppState(ctx, evt, deviceID, nil, repo))
		assert.Empty(t, repo.chats)
	})
}

func TestLabelStateFromEvents(t *testing.T) {
	deviceID := "628999999999@s.whatsapp.net"
	evts := []any{
		labelEditEvent("1", &waSyncAction.LabelEditAction{Name: proto.String("New lead")}, 1000),
		labelEditEvent("2", &waSyncAction.LabelEditAction{Name: proto.String("Paid")}, 1000),
		&events.LabelEdit{LabelID: "1"}, // typed events are ignored; the raw AppState carries the same data
		labelChatEvent("1", "628111111111@s.whatsapp.net", true),
		labelChatEvent("2", "628222222222@s.whatsapp.net", true),
		labelChatEvent("2", "628222222222@s.whatsapp.net", false),
		labelEditEvent("1", &waSyncAction.LabelEditAction{Name: proto.String("Lead")}, 2000),
		labelChatEvent("1", "628333333333@s.whatsapp.net", true),
		labelChatEvent("1", "628111111111@s.whatsapp.net", true),
	}

	labels, chats := labelStateFromEvents(context.Background(), nil, deviceID, evts)
	require.Len(t, labels, 2)
	assert.Equal(t, "1", labels[0].ID)
	assert.Equal(t, "Lead", labels[0].Name, "a later mutation of the same label wins")
	assert.Equal(t, "2", labels[1].ID)

	var got []string
	for _, chat := range chats {
		assert.Equal(t, deviceID, chat.DeviceID)
		got = append(got, chat.LabelID+"/"+chat.ChatJID)
	}
	assert.Equal(t, []string{"1/628111111111@s.whatsapp.net", "1/628333333333@s.whatsapp.net"}, got)
}
