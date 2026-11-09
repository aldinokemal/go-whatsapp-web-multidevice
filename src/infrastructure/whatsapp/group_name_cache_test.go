package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestGroupNameLookupNeverFallsBackToTheGlobalAccount(t *testing.T) {
	const group = "120363000000000075@g.us"
	globalClient, _ := groupNameTestDevice(t, "15550000041")
	globalStateMu.Lock()
	originalClient := cli
	cli = globalClient
	globalStateMu.Unlock()
	originalFetch := fetchGroupInfoFn
	fetchGroupInfoFn = func(context.Context, *whatsmeow.Client, types.JID) (*types.GroupInfo, error) {
		t.Error("unresolved device queried the global account")
		return nil, nil
	}
	deviceID := groupNameCacheDeviceID(globalClient)
	setCachedGroupName(deviceID, group, "Global account's group")
	t.Cleanup(func() {
		globalStateMu.Lock()
		cli = originalClient
		globalStateMu.Unlock()
		fetchGroupInfoFn = originalFetch
		invalidateCachedGroupName(deviceID, group)
	})
	for _, ctx := range []context.Context{
		context.Background(), // Replay when the original device cannot be resolved.
		ContextWithDevice(context.Background(), nil),
		ContextWithDevice(context.Background(), NewDeviceInstance("logged-out", nil, nil)),
	} {
		if got := getGroupName(ctx, group); got != "" {
			t.Fatalf("unresolved device received group name %q", got)
		}
	}
}

func TestGroupNameInvalidationCannotBeUndoneByAnInflightFetch(t *testing.T) {
	const group = "120363000000000074@g.us"
	client, ctx := groupNameTestDevice(t, "15550000031")
	deviceID := groupNameCacheDeviceID(client)
	original := fetchGroupInfoFn
	calls := 0
	fetchGroupInfoFn = func(_ context.Context, _ *whatsmeow.Client, jid types.JID) (*types.GroupInfo, error) {
		calls++
		name := "Renamed"
		if calls == 1 {
			// A subject-change event arrives after the request started, before the
			// server's older response is returned to the cache writer.
			invalidateCachedGroupName(deviceID, group)
			name = "Old subject"
		}
		return &types.GroupInfo{JID: jid, GroupName: types.GroupName{Name: name}}, nil
	}
	t.Cleanup(func() {
		fetchGroupInfoFn = original
		invalidateCachedGroupName(deviceID, group)
	})
	if got := getGroupName(ctx, group); got != "Old subject" {
		t.Fatalf("in-flight result = %q", got)
	}
	if name, ok := getCachedGroupName(deviceID, group); ok {
		t.Fatalf("in-flight fetch restored invalidated subject %q", name)
	}
	if got := getGroupName(ctx, group); got != "Renamed" || calls != 2 {
		t.Fatalf("next lookup = %q after %d fetches", got, calls)
	}
}

// groupNameTestDevice returns a logged-in client for the given account and a
// context that carries its device, the way an event for that device does.
func groupNameTestDevice(t *testing.T, user string) (*whatsmeow.Client, context.Context) {
	t.Helper()
	jid := types.NewJID(user, types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &jid}}
	return client, ContextWithDevice(context.Background(), NewDeviceInstance(user, client, nil))
}

// stubGroupInfo answers group-info lookups per client: a client listed in
// subjects is a member and reads that subject, any other client is refused.
// It returns a counter of lookups per account.
func stubGroupInfo(t *testing.T, subjects map[*whatsmeow.Client]*string) map[string]int {
	t.Helper()
	calls := make(map[string]int)
	original := fetchGroupInfoFn
	fetchGroupInfoFn = func(_ context.Context, client *whatsmeow.Client, jid types.JID) (*types.GroupInfo, error) {
		calls[client.Store.ID.User]++
		subject, ok := subjects[client]
		if !ok {
			return nil, errors.New("not a participant")
		}
		return &types.GroupInfo{JID: jid, GroupName: types.GroupName{Name: *subject}}, nil
	}
	t.Cleanup(func() { fetchGroupInfoFn = original })
	return calls
}

// A device that is not in a group must not be handed the subject another
// device read for it.
func TestGroupNameCacheDoesNotServeAnotherDevice(t *testing.T) {
	const group = "120363000000000071@g.us"
	clientA, ctxA := groupNameTestDevice(t, "15550000011")
	clientB, ctxB := groupNameTestDevice(t, "15550000012")
	subject := "Members only"
	calls := stubGroupInfo(t, map[*whatsmeow.Client]*string{clientA: &subject})
	t.Cleanup(func() {
		invalidateCachedGroupName(groupNameCacheDeviceID(clientA), group)
		invalidateCachedGroupName(groupNameCacheDeviceID(clientB), group)
	})

	if got := getGroupName(ctxA, group); got != "Members only" {
		t.Fatalf("device A group name = %q, want its own lookup result", got)
	}
	if got := getGroupName(ctxB, group); got != "" {
		t.Fatalf("device B (not a member) group name = %q, want empty", got)
	}
	if calls["15550000012"] != 1 {
		t.Fatalf("device B lookups = %d, want 1 (it must ask for itself)", calls["15550000012"])
	}
	// Device A is still served from its own entry.
	if got := getGroupName(ctxA, group); got != "Members only" || calls["15550000011"] != 1 {
		t.Fatalf("device A second read = %q after %d lookups, want the cached name after 1", got, calls["15550000011"])
	}
}

// Two devices cache independently, and a subject change seen by one device
// drops only that device's entry.
func TestGroupNameCacheIsPerDevice(t *testing.T) {
	const group = "120363000000000072@g.us"
	clientA, ctxA := groupNameTestDevice(t, "15550000021")
	clientB, ctxB := groupNameTestDevice(t, "15550000022")
	subjectA, subjectB := "Seen by A", "Seen by B"
	calls := stubGroupInfo(t, map[*whatsmeow.Client]*string{clientA: &subjectA, clientB: &subjectB})
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() {
		log = originalLog
		invalidateCachedGroupName(groupNameCacheDeviceID(clientA), group)
		invalidateCachedGroupName(groupNameCacheDeviceID(clientB), group)
	})

	if got := getGroupName(ctxA, group); got != "Seen by A" {
		t.Fatalf("device A group name = %q", got)
	}
	if got := getGroupName(ctxB, group); got != "Seen by B" {
		t.Fatalf("device B group name = %q, want its own lookup result", got)
	}

	// Device A receives a subject change. No webhook or Chatwoot target is
	// configured, so the event goes no further than the cache.
	subjectA = "Renamed"
	groupJID, err := types.ParseJID(group)
	if err != nil {
		t.Fatalf("parse group JID: %v", err)
	}
	instance, _ := DeviceFromContext(ctxA)
	handler(ctxA, instance, &events.GroupInfo{
		JID:       groupJID,
		Name:      &types.GroupName{Name: "Renamed"},
		Timestamp: time.Now(),
	})

	if got := getGroupName(ctxA, group); got != "Renamed" {
		t.Fatalf("device A group name after a subject change = %q, want the new subject", got)
	}
	if got := getGroupName(ctxB, group); got != "Seen by B" || calls["15550000022"] != 1 {
		t.Fatalf("device B group name = %q after %d lookups, want its own cached entry untouched", got, calls["15550000022"])
	}
}

// A client that is not logged in has no identity to cache under: it neither
// reads another device's entry nor leaves one behind.
func TestGroupNameCacheSkipsAnUnidentifiedClient(t *testing.T) {
	const group = "120363000000000073@g.us"
	if got := groupNameCacheDeviceID(nil); got != "" {
		t.Fatalf("nil client cache identity = %q, want empty", got)
	}
	if got := groupNameCacheDeviceID(&whatsmeow.Client{Store: &store.Device{}}); got != "" {
		t.Fatalf("logged-out client cache identity = %q, want empty", got)
	}
	setCachedGroupName("", group, "Leaked")
	if name, ok := getCachedGroupName("", group); ok {
		t.Fatalf("empty device read %q from the cache", name)
	}
	if _, ok := groupNameCache.Load(groupNameCacheKey{groupJID: group}); ok {
		t.Fatal("an entry was stored without a device")
	}
}
