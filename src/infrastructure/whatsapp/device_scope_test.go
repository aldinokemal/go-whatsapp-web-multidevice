package whatsapp

import (
	"context"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// scopeRepo is a minimal message store with the base repository's semantics:
// the unscoped calls act on every device's rows, the scoped ones on one
// device's. (The SQLite repository cannot be imported here: it imports this
// package.)
type scopeRepo struct {
	domainChatStorage.IChatStorageRepository
	rows []*domainChatStorage.Message
}

func (r *scopeRepo) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	for _, row := range r.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return nil, nil
}

func (r *scopeRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	for _, row := range r.rows {
		if row.ID == id && row.DeviceID == deviceID {
			return row, nil
		}
	}
	return nil, nil
}

func (r *scopeRepo) DeleteMessage(id, chatJID string) error {
	return r.delete(func(row *domainChatStorage.Message) bool { return row.ID == id && row.ChatJID == chatJID })
}

func (r *scopeRepo) DeleteMessageByDevice(deviceID, id, chatJID string) error {
	return r.delete(func(row *domainChatStorage.Message) bool {
		return row.ID == id && row.ChatJID == chatJID && row.DeviceID == deviceID
	})
}

func (r *scopeRepo) delete(match func(*domainChatStorage.Message) bool) error {
	kept := r.rows[:0]
	for _, row := range r.rows {
		if !match(row) {
			kept = append(kept, row)
		}
	}
	r.rows = kept
	return nil
}

func newScopeRepo(t *testing.T) *scopeRepo {
	t.Helper()
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })
	const group = "120363000000000042@g.us"
	return &scopeRepo{rows: []*domainChatStorage.Message{
		{ID: "msg-1", ChatJID: group, DeviceID: "device-a@s.whatsapp.net", IsFromMe: true},
		{ID: "msg-1", ChatJID: group, DeviceID: "device-b@s.whatsapp.net"},
	}}
}

// "Delete for me" on one account must remove that account's stored copy only.
func TestDeleteForMeRemovesOnlyTheReceivingDevicesCopy(t *testing.T) {
	repo := newScopeRepo(t)

	removed := deleteStoredMessageForMe(repo, "device-b@s.whatsapp.net", "msg-1")

	if removed == nil || removed.DeviceID != "device-b@s.whatsapp.net" {
		t.Fatalf("removed = %+v, want device B's copy", removed)
	}
	if len(repo.rows) != 1 || repo.rows[0].DeviceID != "device-a@s.whatsapp.net" {
		t.Fatalf("remaining rows = %+v, want only device A's copy", repo.rows)
	}
}

func TestDeleteForMeWithoutADeviceDeletesNothing(t *testing.T) {
	repo := newScopeRepo(t)

	if removed := deleteStoredMessageForMe(repo, "", "msg-1"); removed != nil {
		t.Fatalf("removed = %+v, want nil", removed)
	}
	if len(repo.rows) != 2 {
		t.Fatalf("remaining rows = %d, want 2", len(repo.rows))
	}
}

// The per-device wrapper must not hand out another device's row.
func TestDeviceChatStorageGetMessageByIDIsScoped(t *testing.T) {
	repo := newScopeRepo(t)

	message, err := newDeviceChatStorage("device-b@s.whatsapp.net", repo).GetMessageByID("msg-1")
	if err != nil {
		t.Fatalf("GetMessageByID: %v", err)
	}
	if message == nil || message.DeviceID != "device-b@s.whatsapp.net" {
		t.Fatalf("message = %+v, want device B's copy", message)
	}

	message, err = newDeviceChatStorage("device-c@s.whatsapp.net", repo).GetMessageByID("msg-1")
	if err != nil {
		t.Fatalf("GetMessageByID: %v", err)
	}
	if message != nil {
		t.Fatalf("message = %+v, want nil for a device with no copy", message)
	}
}

// historyNameRepoSpy answers the unscoped chat-name lookup with a name that
// belongs to another device, and records the device asked for by the scoped one.
type historyNameRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	scopedDeviceIDs []string
	unscopedCalls   int
}

func (r *historyNameRepoSpy) GetChatNameWithPushName(types.JID, string, string, string) string {
	r.unscopedCalls++
	return "Name saved by another device"
}

func (r *historyNameRepoSpy) GetChatNameWithPushNameByDevice(deviceID string, jid types.JID, _ string, _ string, _ string) string {
	r.scopedDeviceIDs = append(r.scopedDeviceIDs, deviceID)
	return jid.User
}

// History sync names a chat from this device's own stored chat, never from
// another device's row for the same JID.
func TestHistorySyncChatNameLookupIsDeviceScoped(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	repo := &historyNameRepoSpy{}
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance("device-b@s.whatsapp.net", nil, nil))
	syncType := waHistorySync.HistorySync_RECENT
	data := &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{
			{ID: proto.String("628123456789@s.whatsapp.net")},
		},
	}

	if err := processConversationMessages(ctx, data, repo, nil); err != nil {
		t.Fatalf("process conversation messages: %v", err)
	}

	if repo.unscopedCalls != 0 {
		t.Fatalf("unscoped chat-name lookups = %d, want 0", repo.unscopedCalls)
	}
	if len(repo.scopedDeviceIDs) != 1 || repo.scopedDeviceIDs[0] != "device-b@s.whatsapp.net" {
		t.Fatalf("scoped lookups = %v, want one for device B", repo.scopedDeviceIDs)
	}
}
