package usecase

import (
	"context"
	"slices"
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

type callLogsRepo struct {
	domainChatStorage.IChatStorageRepository
	filter *domainChatStorage.CallRecordFilter
}

func (r *callLogsRepo) GetCallRecords(filter *domainChatStorage.CallRecordFilter) ([]*domainChatStorage.Message, int64, error) {
	r.filter = filter
	return nil, 0, nil
}

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

func TestListCallLogsNormalizesChatFilter(t *testing.T) {
	own := types.NewADJID("628999", 0, 3)
	client := &whatsmeow.Client{Store: &store.Device{ID: &own, LIDs: callLogsLIDs{}}}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("slot-a", client, nil))
	repo := &callLogsRepo{}
	service := NewCallService(repo)

	for _, tc := range []struct {
		in   string
		want []string
	}{
		{in: "111@lid", want: []string{"111@lid", "628123@s.whatsapp.net"}},
		{in: " 628777@s.whatsapp.net ", want: []string{"628777@s.whatsapp.net"}},
		{in: "120363999000111@g.us", want: []string{"120363999000111@g.us"}},
		{in: "222@lid", want: []string{"222@lid"}},
		{in: "", want: nil},
	} {
		repo.filter = nil
		if _, err := service.ListCallLogs(ctx, domainCall.ListCallLogsRequest{ChatJID: tc.in}); err != nil {
			t.Fatalf("ListCallLogs(%q): %v", tc.in, err)
		}
		if repo.filter == nil {
			t.Fatalf("ListCallLogs(%q) did not query storage", tc.in)
		}
		if !slices.Equal(repo.filter.ChatJIDs, tc.want) {
			t.Fatalf("ListCallLogs(%q) filtered by %q, want %q", tc.in, repo.filter.ChatJIDs, tc.want)
		}
		if repo.filter.DeviceID != "628999@s.whatsapp.net" {
			t.Fatalf("ListCallLogs(%q) used device %q", tc.in, repo.filter.DeviceID)
		}
	}
}
