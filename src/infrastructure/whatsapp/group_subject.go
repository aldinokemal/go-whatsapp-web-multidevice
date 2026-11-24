package whatsapp

import (
	"context"
	"hash/maphash"
	"strings"
	"sync"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type groupNameCacheKey struct {
	deviceID string
	groupJID string
}

type groupNameCacheEntry struct {
	name          string
	expiresAt     time.Time
	awaitingStore bool // authoritative rename not yet written to a chat row
	pending       *groupSubjectLookup
}

type groupSubjectLookup struct {
	done chan struct{}
	name string                                   // set before done is closed
	repo domainChatStorage.IChatStorageRepository // guarded by the key's lock
}

var (
	groupNameCache         sync.Map
	groupSubjectLocks      [mutexShardCount]sync.Mutex
	groupSubjectHashSeed   = maphash.MakeSeed()
	historyLookupSlots     = make(chan struct{}, 3) // leave capacity for foreground requests
	groupSubjectFetchSlots = make(chan struct{}, 4)
	fetchGroupInfoFn       = func(ctx context.Context, client *whatsmeow.Client, jid types.JID) (*types.GroupInfo, error) {
		return client.GetGroupInfo(ctx, jid)
	}
)

const groupNameCacheTTL = 5 * time.Minute

func (key groupNameCacheKey) lock() *sync.Mutex {
	return &groupSubjectLocks[maphash.Comparable(groupSubjectHashSeed, key)%mutexShardCount]
}

func groupNameCacheDeviceID(client *whatsmeow.Client) string {
	if client == nil {
		return ""
	}
	return client.Store.GetJID().ToNonAD().String()
}

func getCachedGroupName(deviceID, groupJID string) (string, bool) {
	key := groupNameCacheKey{deviceID, groupJID}
	if entry, ok := groupNameCache.Load(key); ok {
		cached := entry.(groupNameCacheEntry)
		if cached.awaitingStore || time.Now().Before(cached.expiresAt) {
			return cached.name, true
		}
		if cached.pending == nil {
			groupNameCache.CompareAndDelete(key, cached)
		}
	}
	return "", false
}

func setCachedGroupName(deviceID, groupJID, name string) {
	if deviceID != "" {
		groupNameCache.Store(groupNameCacheKey{deviceID, groupJID}, groupNameCacheEntry{name: name, expiresAt: time.Now().Add(groupNameCacheTTL)})
	}
}

func invalidateCachedGroupName(deviceID, groupJID string) {
	groupNameCache.Delete(groupNameCacheKey{deviceID, groupJID})
}

// StoreChatWithGroupSubject serializes a group's first insert with rename events.
// Save the row immediately, even when WhatsApp is slow or cannot supply a name.
func StoreChatWithGroupSubject(ctx context.Context, repo domainChatStorage.IChatStorageRepository, chat *domainChatStorage.Chat) error {
	if !strings.HasSuffix(chat.JID, "@g.us") || chat.DeviceID == "" {
		return repo.StoreChat(chat)
	}
	key := groupNameCacheKey{chat.DeviceID, chat.JID}
	mu := key.lock()
	mu.Lock()
	defer mu.Unlock()

	name, cached := getCachedGroupName(key.deviceID, key.groupJID)
	if name != "" {
		chat.Name = name
	}
	if err := repo.StoreChat(chat); err != nil {
		return err
	}
	if entry, ok := groupNameCache.Load(key); ok && entry.(groupNameCacheEntry).awaitingStore {
		// StoreChat preserves an existing meaningful name. Only an event may replace it.
		key.persistRename(repo, name)
	}
	groupUser := strings.TrimSuffix(chat.JID, "@g.us")
	if cached || chat.Name != "Group "+groupUser {
		return nil
	}
	instance, ok := DeviceFromContext(ctx)
	if !ok || instance == nil {
		return nil
	}
	client := instance.GetClient()
	if groupNameCacheDeviceID(client) != key.deviceID {
		return nil
	}
	lookup, start := key.reserveLookup(repo)
	if start {
		select {
		case historyLookupSlots <- struct{}{}:
			go func() {
				defer func() { <-historyLookupSlots }()
				key.fetch(context.WithoutCancel(ctx), client, types.NewJID(groupUser, types.GroupServer), lookup)
			}()
		default:
			// No waiter can join until this lock is released. Allow the next write to retry.
			groupNameCache.Delete(key)
		}
	}
	return nil
}

func recordGroupSubject(deviceID, groupJID, name string, repo domainChatStorage.IChatStorageRepository) {
	key := groupNameCacheKey{deviceID, groupJID}
	mu := key.lock()
	mu.Lock()
	defer mu.Unlock()
	if repo == nil {
		setCachedGroupName(deviceID, groupJID, name)
		return
	}
	key.persistRename(repo, name)
}

// Caller holds the key lock. Keep only the latest event until its row exists
// and the database acknowledges the update, even across ordinary cache expiry.
func (key groupNameCacheKey) persistRename(repo domainChatStorage.IChatStorageRepository, name string) {
	applied, err := repo.UpdateGroupSubject(key.deviceID, key.groupJID, name)
	if err != nil {
		logrus.Warnf("Failed to update group subject for %s: %v", key.groupJID, err)
	}
	groupNameCache.Store(key, groupNameCacheEntry{name: name, expiresAt: time.Now().Add(groupNameCacheTTL), awaitingStore: err != nil || !applied})
}

// reserveLookup is called under the key's lock, after checking for a cached name.
// Storage can join a lookup already started by a webhook and receive the result.
func (key groupNameCacheKey) reserveLookup(repo domainChatStorage.IChatStorageRepository) (*groupSubjectLookup, bool) {
	if entry, ok := groupNameCache.Load(key); ok {
		if pending := entry.(groupNameCacheEntry).pending; pending != nil {
			if repo != nil {
				pending.repo = repo
			}
			return pending, false
		}
	}
	pending := &groupSubjectLookup{done: make(chan struct{}), repo: repo}
	groupNameCache.Store(key, groupNameCacheEntry{pending: pending})
	return pending, true
}

func (key groupNameCacheKey) fetch(ctx context.Context, client *whatsmeow.Client, jid types.JID, lookup *groupSubjectLookup) {
	var name string
	select {
	case groupSubjectFetchSlots <- struct{}{}:
		// Queued history work may already have been superseded by a rename.
		entry, active := groupNameCache.Load(key)
		if active && entry.(groupNameCacheEntry).pending == lookup && groupNameCacheDeviceID(client) == key.deviceID && ctx.Err() == nil {
			fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			info, err := fetchGroupInfoFn(fetchCtx, client, jid)
			cancel()
			if err != nil {
				logrus.Warnf("Failed to resolve group subject for %s: %v", jid, err)
			} else if info != nil && strings.TrimSpace(info.Name) != "" {
				name = info.Name
			}
		}
		<-groupSubjectFetchSlots
	case <-ctx.Done():
	}

	mu := key.lock()
	mu.Lock()
	defer mu.Unlock()
	ttl := groupNameCacheTTL
	if name == "" {
		ttl = 30 * time.Second
	}
	if groupNameCache.CompareAndSwap(key, groupNameCacheEntry{pending: lookup}, groupNameCacheEntry{name: name, expiresAt: time.Now().Add(ttl)}) {
		if name != "" && lookup.repo != nil {
			if err := lookup.repo.FillGroupSubject(key.deviceID, key.groupJID, name); err != nil {
				logrus.Warnf("Failed to fill group subject for %s: %v", jid, err)
			}
		}
	} else if current, ok := getCachedGroupName(key.deviceID, key.groupJID); ok {
		// A rename arrived while the server was answering.
		name = current
	}
	lookup.name = name
	close(lookup.done)
}

// ResolveGroupSubject looks up a group on the account explicitly identified by ctx.
func ResolveGroupSubject(ctx context.Context, jid types.JID) string {
	instance, ok := DeviceFromContext(ctx)
	if !ok || instance == nil || jid.IsEmpty() || jid.Server != types.GroupServer {
		return ""
	}
	client := instance.GetClient()
	key := groupNameCacheKey{groupNameCacheDeviceID(client), jid.ToNonAD().String()}
	if key.deviceID == "" || ctx.Err() != nil {
		return ""
	}

	mu := key.lock()
	mu.Lock()
	if name, ok := getCachedGroupName(key.deviceID, key.groupJID); ok {
		mu.Unlock()
		return name
	}
	lookup, start := key.reserveLookup(nil)
	mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if start {
		key.fetch(ctx, client, jid, lookup)
	}
	select {
	case <-lookup.done:
		return lookup.name
	case <-ctx.Done():
		return ""
	}
}
