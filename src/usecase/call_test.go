package usecase

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// callLogsLIDs maps LID 111 to phone 628123, like the store CreateIncomingCallRecord resolves through.
type callLogsLIDs struct {
	store.LIDStore
}

func (callLogsLIDs) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if lid.User == "111" {
		return types.NewJID("628123", types.DefaultUserServer), nil
	}
	return types.EmptyJID, nil
}

func (callLogsLIDs) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	if pn.User == "628123" {
		return types.NewJID("111", types.HiddenUserServer), nil
	}
	return types.EmptyJID, nil
}

func TestListCallLogsNormalizesChatFilter(t *testing.T) {
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "calls.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())

	own := types.NewADJID("628999", 0, 3)
	client := &whatsmeow.Client{Store: &store.Device{ID: &own, LIDs: callLogsLIDs{}}}
	ctx := whatsapp.ContextWithDevice(t.Context(), whatsapp.NewDeviceInstance("slot-a", client, nil))
	service := NewCallService(repo)
	device := own.ToNonAD().String()
	const phone, lid, group = "628123@s.whatsapp.net", "111@lid", "120363999000111@g.us"
	for i, row := range []struct{ device, chat, id string }{
		{device, lid, "before-mapping"},
		{device, phone, "after-mapping"},
		{device, group, "group-call"},
		{device, "222@lid", "unknown-lid"},
		{device, "628777@s.whatsapp.net", "unknown-phone"},
		{"628888@s.whatsapp.net", lid, "other-device"},
	} {
		require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{DeviceID: row.device, JID: row.chat}))
		require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
			DeviceID: row.device, ChatJID: row.chat, ID: row.id, MediaType: "call", Timestamp: time.Unix(int64(i), 0),
		}))
	}
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{" " + phone + " ", []string{"after-mapping", "before-mapping"}},
		{lid, []string{"after-mapping", "before-mapping"}},
		{group, []string{"group-call"}},
		{"628777@s.whatsapp.net", []string{"unknown-phone"}},
		{"222@lid", []string{"unknown-lid"}},
		{"", []string{"unknown-phone", "unknown-lid", "group-call", "after-mapping", "before-mapping"}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			result, err := service.ListCallLogs(ctx, domainCall.ListCallLogsRequest{ChatJID: tc.in})
			require.NoError(t, err)
			require.Equal(t, len(tc.want), result.Pagination.Total)
			var ids []string
			for _, row := range result.Data {
				ids = append(ids, row.ID)
			}
			require.Equal(t, tc.want, ids)
		})
	}
}
