package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"

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
		jid, _ := types.ParseJID(group)
		if got := ResolveGroupSubject(ctx, jid); got != "" {
			t.Fatalf("resolver used unscoped global account: %q", got)
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

type groupSubjectRepo struct {
	domainChatStorage.IChatStorageRepository
	updates []string
	err     error
}

func (r *groupSubjectRepo) UpdateGroupSubject(deviceID, groupJID, subject string) (bool, error) {
	r.updates = append(r.updates, deviceID+"|"+groupJID+"|"+subject)
	return r.err == nil, r.err
}

func TestResolveGroupSubjectDeviceIsolationAndFailure(t *testing.T) {
	group := types.NewJID("120363000000000076", types.GroupServer)
	clientA, ctxA := groupNameTestDevice(t, "15550000051")
	clientB, ctxB := groupNameTestDevice(t, "15550000052")
	name := "A's subject"
	calls := stubGroupInfo(t, map[*whatsmeow.Client]*string{clientA: &name})
	t.Cleanup(func() {
		invalidateCachedGroupName(groupNameCacheDeviceID(clientA), group.String())
		invalidateCachedGroupName(groupNameCacheDeviceID(clientB), group.String())
	})
	if got := ResolveGroupSubject(ctxA, group); got != name {
		t.Fatalf("device A subject = %q", got)
	}
	if got := ResolveGroupSubject(ctxB, group); got != "" || calls[clientB.Store.ID.User] != 1 {
		t.Fatalf("failed device B lookup = %q, calls = %d", got, calls[clientB.Store.ID.User])
	}
	if got := ResolveGroupSubject(ctxA, group); got != name || calls[clientA.Store.ID.User] != 1 {
		t.Fatalf("cached subject = %q after %d lookups", got, calls[clientA.Store.ID.User])
	}
	if got := ResolveGroupSubject(ContextWithDevice(context.Background(), NewDeviceInstance("logged-out", nil, nil)), group); got != "" {
		t.Fatalf("unidentified client subject = %q", got)
	}
}

func TestGroupSubjectEventUpdatesOnlyNamedDeviceAndBeatsInflightFetch(t *testing.T) {
	group := types.NewJID("120363000000000077", types.GroupServer)
	client, ctx := groupNameTestDevice(t, "15550000053")
	other, _ := groupNameTestDevice(t, "15550000054")
	id := groupNameCacheDeviceID(client)
	otherID := groupNameCacheDeviceID(other)
	repo := &groupSubjectRepo{err: fmt.Errorf("storage failed")}
	instance := NewDeviceInstance("slot", client, repo)
	ctx = ContextWithDevice(ctx, instance)
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })
	setCachedGroupName(otherID, group.String(), "Other device")
	oldFetch := fetchGroupInfoFn
	started, release := make(chan struct{}), make(chan struct{})
	fetchGroupInfoFn = func(_ context.Context, _ *whatsmeow.Client, jid types.JID) (*types.GroupInfo, error) {
		close(started)
		<-release
		return &types.GroupInfo{JID: jid, GroupName: types.GroupName{Name: "Stale"}}, nil
	}
	t.Cleanup(func() {
		fetchGroupInfoFn = oldFetch
		invalidateCachedGroupName(id, group.String())
		invalidateCachedGroupName(otherID, group.String())
	})
	result := make(chan string, 1)
	go func() { result <- ResolveGroupSubject(ctx, group) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetch did not start")
	}
	handleGroupInfo(ctx, &events.GroupInfo{JID: group, Name: &types.GroupName{Name: "New subject"}}, id, client)
	close(release)
	if got := <-result; got != "New subject" {
		t.Fatalf("stale fetch returned %q", got)
	}
	if got := ResolveGroupSubject(ctx, group); got != "New subject" {
		t.Fatalf("cached event subject = %q", got)
	}
	if got, _ := getCachedGroupName(otherID, group.String()); got != "Other device" {
		t.Fatalf("other device subject changed to %q", got)
	}
	if len(repo.updates) != 1 || repo.updates[0] != id+"|"+group.String()+"|New subject" {
		t.Fatalf("repository calls = %v", repo.updates)
	}
	handleGroupInfo(ctx, &events.GroupInfo{JID: group, Name: &types.GroupName{}}, id, client)
	handleGroupInfo(ctx, &events.GroupInfo{JID: group, Name: &types.GroupName{Name: "  "}}, id, client)
	handleGroupInfo(ctx, &events.GroupInfo{JID: group, Join: []types.JID{group}}, id, client)
	if len(repo.updates) != 1 {
		t.Fatalf("subjectless event updated repository: %v", repo.updates)
	}
}

func TestRenameBeforeFirstGroupInsertUsesAccountSubject(t *testing.T) {
	group := types.NewJID("120363000000000078", types.GroupServer)
	client, _ := groupNameTestDevice(t, "15550000055")
	account := groupNameCacheDeviceID(client)
	repo := &renameBeforeInsertRepo{groupSubjectRepo: &groupSubjectRepo{}}
	wrapped := newDeviceChatStorage("outer-alias", newDeviceChatStorage("inner-companion", repo))
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance("outer-alias", client, wrapped))
	t.Cleanup(func() { invalidateCachedGroupName(account, group.String()) })

	// Resolution of an older history or message candidate preceded the rename.
	chat := &domainChatStorage.Chat{DeviceID: account, JID: group.String(), Name: "Old subject"}
	recordGroupSubject(account, chat.JID, "New subject", wrapped)
	key := groupNameCacheKey{account, chat.JID}
	entry, _ := groupNameCache.Load(key)
	expired := entry.(groupNameCacheEntry)
	expired.expiresAt = time.Now().Add(-time.Hour)
	groupNameCache.Store(key, expired)
	if err := StoreChatWithGroupSubject(ctx, wrapped, chat); err != nil {
		t.Fatal(err)
	}
	if repo.stored != "New subject" || len(repo.updates) != 2 || repo.updates[0] != account+"|"+chat.JID+"|New subject" {
		t.Fatalf("stored %q, updates %v", repo.stored, repo.updates)
	}
}

type renameBeforeInsertRepo struct {
	*groupSubjectRepo
	stored string
}

func (r *renameBeforeInsertRepo) StoreChat(chat *domainChatStorage.Chat) error {
	if r.stored == "" {
		r.stored = chat.Name
	}
	return nil
}

func TestRepeatedGroupLookupSharesFailure(t *testing.T) {
	group := types.NewJID("120363000000000079", types.GroupServer)
	client, ctx := groupNameTestDevice(t, "15550000056")
	calls := stubGroupInfo(t, nil)
	t.Cleanup(func() { invalidateCachedGroupName(groupNameCacheDeviceID(client), group.String()) })
	for i := 0; i < 2; i++ {
		if got := ResolveGroupSubject(ctx, group); got != "" {
			t.Fatalf("unexpected subject %q", got)
		}
	}
	if calls[client.Store.ID.User] != 1 {
		t.Fatalf("got %d requests for the same failed lookup", calls[client.Store.ID.User])
	}
}

func (r *renameBeforeInsertRepo) UpdateGroupSubject(deviceID, jid, name string) (bool, error) {
	_, err := r.groupSubjectRepo.UpdateGroupSubject(deviceID, jid, name)
	if err != nil || r.stored == "" {
		return false, err
	}
	r.stored = name
	return true, nil
}

func TestFailedGroupRenameRetriesLatestEvent(t *testing.T) {
	key := groupNameCacheKey{"account", "567@g.us"}
	defer groupNameCache.Delete(key)
	repo := &renameBeforeInsertRepo{groupSubjectRepo: &groupSubjectRepo{err: errors.New("temporary write failure")}, stored: "Original"}
	recordGroupSubject(key.deviceID, key.groupJID, "Intermediate", repo)
	recordGroupSubject(key.deviceID, key.groupJID, "Latest", repo)
	chat := &domainChatStorage.Chat{DeviceID: key.deviceID, JID: key.groupJID, Name: "Original"}
	for _, want := range []string{"Original", "Latest", "Latest"} {
		if err := StoreChatWithGroupSubject(context.Background(), repo, chat); err != nil {
			t.Fatal(err)
		}
		if repo.stored != want {
			t.Fatalf("stored %q, want %q", repo.stored, want)
		}
		repo.err = nil
	}
	if len(repo.updates) != 4 {
		t.Fatalf("updates = %v", repo.updates)
	}
}

func TestHistoryLookupAtCapacityAllowsLaterRetry(t *testing.T) {
	client, ctx := groupNameTestDevice(t, "15550000057")
	key := groupNameCacheKey{groupNameCacheDeviceID(client), "321@g.us"}
	defer groupNameCache.Delete(key)
	repo := &historyChatNameRepoSpy{historyMessageBatchRepoSpy: &historyMessageBatchRepoSpy{}, filled: make(chan string, 1)}
	chat := &domainChatStorage.Chat{DeviceID: key.deviceID, JID: key.groupJID, Name: "Group 321"}
	name := "Fetched"
	stubGroupInfo(t, map[*whatsmeow.Client]*string{client: &name})
	func() {
		for i := 0; i < cap(historyLookupSlots); i++ {
			historyLookupSlots <- struct{}{}
			defer func() { <-historyLookupSlots }()
		}
		if err := StoreChatWithGroupSubject(ctx, repo, chat); err != nil {
			t.Fatal(err)
		}
		if _, ok := groupNameCache.Load(key); ok {
			t.Fatal("overload retained lookup reservation")
		}
	}()
	if err := StoreChatWithGroupSubject(ctx, repo, chat); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-repo.filled:
		if got != key.deviceID+"|"+key.groupJID+"|"+name {
			t.Fatalf("unexpected fill %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("later write did not retry lookup")
	}
}

type blockingGroupFill struct {
	*renameBeforeInsertRepo
	started chan struct{}
	release chan struct{}
}

func (r *blockingGroupFill) FillGroupSubject(_, _, _ string) error {
	close(r.started)
	<-r.release
	return nil
}

func TestForegroundGroupLookupCanCancelDuringFill(t *testing.T) {
	client, base := groupNameTestDevice(t, "15550000058")
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	jid := types.NewJID("322", types.GroupServer)
	key := groupNameCacheKey{groupNameCacheDeviceID(client), jid.String()}
	defer groupNameCache.Delete(key)
	oldFetch := fetchGroupInfoFn
	network, reply := make(chan struct{}), make(chan struct{})
	fetchGroupInfoFn = func(context.Context, *whatsmeow.Client, types.JID) (*types.GroupInfo, error) {
		close(network)
		<-reply
		return &types.GroupInfo{GroupName: types.GroupName{Name: "Subject"}}, nil
	}
	defer func() { fetchGroupInfoFn = oldFetch }()
	repo := &blockingGroupFill{renameBeforeInsertRepo: &renameBeforeInsertRepo{}, started: make(chan struct{}), release: make(chan struct{})}
	defer close(repo.release)
	result := make(chan string, 1)
	go func() { result <- ResolveGroupSubject(ctx, jid) }()
	<-network
	if err := StoreChatWithGroupSubject(base, repo, &domainChatStorage.Chat{DeviceID: key.deviceID, JID: key.groupJID, Name: "Group 322"}); err != nil {
		t.Fatal(err)
	}
	close(reply)
	<-repo.started
	cancel()
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("foreground lookup waited for blocked SQLite fill")
	}
}

func TestUnreadGroupCacheEntryExpires(t *testing.T) {
	key := groupNameCacheKey{"account", "323@g.us"}
	defer groupNameCache.Delete(key)
	entry := groupNameCacheEntry{name: "Old", expiresAt: time.Now().Add(-time.Second)}
	groupNameCache.Store(key, entry)
	key.evictAtExpiry(entry)
	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := groupNameCache.Load(key); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expired unread result remains")
		}
		time.Sleep(time.Millisecond)
	}
}
