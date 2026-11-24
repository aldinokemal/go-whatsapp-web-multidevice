package whatsapp_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domain "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	storage "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	wa "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func labelEdit(labelID string, action *waSyncAction.LabelEditAction) *events.AppState {
	return &events.AppState{
		Index:           []string{"label_edit", labelID},
		SyncActionValue: &waSyncAction.SyncActionValue{LabelEditAction: action},
	}
}

func labelChat(labelID, chatJID string, labeled bool) *events.AppState {
	return &events.AppState{
		Index: []string{"label_jid", labelID, chatJID},
		SyncActionValue: &waSyncAction.SyncActionValue{
			LabelAssociationAction: &waSyncAction.LabelAssociationAction{Labeled: proto.Bool(labeled)},
		},
	}
}

// Label app state is stored for the account of the client that received it,
// whatever the webhook configuration.
func TestLabelAppStateEventsAreStored(t *testing.T) {
	wa.QuietLogForTest(t)
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "labels.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	repo := storage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())

	oldURLs, oldEvents := config.WhatsappWebhook, config.WhatsappWebhookEvents
	config.WhatsappWebhook = nil
	config.WhatsappWebhookEvents = []string{"message"}
	t.Cleanup(func() { config.WhatsappWebhook, config.WhatsappWebhookEvents = oldURLs, oldEvents })
	wa.CaptureDeviceEventsForTest(t, repo, func(map[string]any, string, *domain.DeviceWebhookConfig) {})

	account := types.NewADJID("628111111111", 0, 7)
	inst := wa.NewDeviceInstance("slot-a", whatsmeow.NewClient(&store.Device{ID: &account}, nil), repo)
	const deviceID = "628111111111@s.whatsapp.net"
	dispatch := func(evt *events.AppState) {
		wa.HandleDeviceEventForTest(context.Background(), inst, evt)
	}

	dispatch(labelEdit("5", &waSyncAction.LabelEditAction{
		Name: proto.String("Paid"), Color: proto.Int32(3), OrderIndex: proto.Int32(1),
		IsActive: proto.Bool(true), Type: waSyncAction.LabelEditAction_CUSTOM.Enum(),
	}))
	dispatch(labelEdit("6", &waSyncAction.LabelEditAction{Name: proto.String("Lead"), OrderIndex: proto.Int32(2)}))
	dispatch(labelChat("5", "628333333333:12@s.whatsapp.net", true))
	dispatch(labelChat("5", "123456789012345@lid", true))
	dispatch(labelChat("6", "628444444444@s.whatsapp.net", true))

	labels, err := repo.ListLabels(deviceID)
	require.NoError(t, err)
	require.Len(t, labels, 2)
	require.Equal(t, "Paid", labels[0].Name)
	require.Equal(t, "CUSTOM", labels[0].Type)
	require.True(t, labels[0].IsActive)
	require.Equal(t, int32(3), labels[0].Color)

	associations, err := repo.ListLabelAssociations(deviceID, "5")
	require.NoError(t, err)
	require.Len(t, associations, 2)
	require.Equal(t, "123456789012345@lid", associations[0].ChatJID, "the index JID is kept as is")
	require.Equal(t, "628333333333@s.whatsapp.net", associations[1].ChatJID, "the device part is dropped")

	// Unlabel the LID chat with the same index, then delete label 6.
	dispatch(labelChat("5", "123456789012345@lid", false))
	dispatch(labelEdit("6", &waSyncAction.LabelEditAction{Name: proto.String("Lead"), Deleted: proto.Bool(true)}))

	associations, err = repo.ListLabelAssociations(deviceID, "")
	require.NoError(t, err)
	require.Len(t, associations, 1)
	require.Equal(t, "5", associations[0].LabelID)
	require.Equal(t, "628333333333@s.whatsapp.net", associations[0].ChatJID)
	labels, err = repo.ListLabels(deviceID)
	require.NoError(t, err)
	require.Len(t, labels, 1)
	require.Equal(t, "5", labels[0].LabelID)
}
