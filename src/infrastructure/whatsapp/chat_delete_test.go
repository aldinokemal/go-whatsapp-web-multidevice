package whatsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type deleteChatRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	device     string
	jids       []string
	err        error
	referenced bool
}

func (r *deleteChatRepoSpy) DeleteChatByDevice(device, jid string, aliases ...string) error {
	r.device, r.jids = device, append([]string{jid}, aliases...)
	return r.err
}
func (r *deleteChatRepoSpy) DeleteChatByDeviceBefore(device string, bound time.Time, jids ...string) (bool, error) {
	return true, r.DeleteChatByDevice(device, jids[0], jids[1:]...)
}

func (r *deleteChatRepoSpy) IsChatMediaDirectoryReferenced(string) (bool, error) {
	return r.referenced, nil
}

func TestPurgeLocalChatMediaIsConfined(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	old := config.PathMedia
	config.PathMedia = root
	t.Cleanup(func() { config.PathMedia = old })
	require.NoError(t, os.MkdirAll(filepath.Join(root, "111", "date"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "111", "date", "file"), []byte("chat"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), []byte("outside"), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "222")))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "333"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "flat-uuid"), nil, 0600))
	repo := &deleteChatRepoSpy{}
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"111@s.whatsapp.net", "../../222@lid", "no-digits@g.us"}, true, nil))
	require.NoDirExists(t, filepath.Join(root, "111"))
	require.FileExists(t, filepath.Join(outside, "keep"))
	require.DirExists(t, filepath.Join(root, "333"))
	require.FileExists(t, filepath.Join(root, "flat-uuid"))
	require.DirExists(t, root)
	// Inner symbolic links must not allow removal outside the root either.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "444"), 0700))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "444", "linked")))
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"444@s.whatsapp.net"}, true, nil))
	require.FileExists(t, filepath.Join(outside, "keep"))
	repo.referenced = true
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"333@s.whatsapp.net"}, true, nil))
	require.DirExists(t, filepath.Join(root, "333"))
	repo.referenced = false
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"333@s.whatsapp.net"}, false, nil))
	require.DirExists(t, filepath.Join(root, "333"))
	repo.err = errors.New("database failed")
	require.Error(t, PurgeLocalChat(repo, "device", []string{"333@s.whatsapp.net"}, true, nil))
	require.DirExists(t, filepath.Join(root, "333"))
}

func TestDeleteChatEventPurgesDeviceAndAliases(t *testing.T) {
	root := t.TempDir()
	old := config.PathMedia
	config.PathMedia = root
	t.Cleanup(func() { config.PathMedia = old })
	require.NoError(t, os.Mkdir(filepath.Join(root, "628111"), 0700))
	require.NoError(t, os.Mkdir(filepath.Join(root, "999111"), 0700))
	account := types.NewJID("628999", types.DefaultUserServer)
	pn, lid := types.NewJID("628111", types.DefaultUserServer), types.NewJID("999111", types.HiddenUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &account, LIDs: &chatDisplayNameLIDStore{pn: pn, lid: lid}}}
	repo := &deleteChatRepoSpy{}
	instance := NewDeviceInstance("slot", client, repo)
	handler(context.Background(), instance, &events.DeleteChat{JID: lid, DeleteMedia: true, Timestamp: time.Now()})
	require.Equal(t, account.String(), repo.device)
	require.Equal(t, []string{lid.String(), pn.String()}, repo.jids)
	require.NoDirExists(t, filepath.Join(root, "628111"))
	require.NoDirExists(t, filepath.Join(root, "999111"))
}

func TestChatDeleteJIDs(t *testing.T) {
	pn, lid := types.NewJID("628111", types.DefaultUserServer), types.NewJID("999111", types.HiddenUserServer)
	client := &whatsmeow.Client{Store: &store.Device{LIDs: &chatDisplayNameLIDStore{pn: pn, lid: lid}}}
	for _, jid := range []types.JID{pn, lid} {
		got, err := ChatDeleteJIDs(context.Background(), client, jid)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{pn.String(), lid.String()}, got)
	}
	got, err := ChatDeleteJIDs(context.Background(), nil, pn)
	require.NoError(t, err)
	require.Equal(t, []string{pn.String()}, got)
}

type boundedDeleteRepo struct {
	domainChatStorage.IChatStorageRepository
	bound        time.Time
	jids         []string
	fullyRemoved bool
	referenced   bool
}

func (r *boundedDeleteRepo) DeleteChatByDeviceBefore(device string, bound time.Time, jids ...string) (bool, error) {
	if device != "628999@s.whatsapp.net" {
		return false, errors.New("incorrect device")
	}
	r.bound, r.jids = bound, jids
	return r.fullyRemoved, nil
}
func (r *boundedDeleteRepo) IsChatMediaDirectoryReferenced(string) (bool, error) {
	return r.referenced, nil
}

func TestDeleteChatEventUsesMessageRangeBoundForLiveAndFullSync(t *testing.T) {
	root := t.TempDir()
	old := config.PathMedia
	config.PathMedia = root
	t.Cleanup(func() { config.PathMedia = old })
	account := types.NewJID("628999", types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &account}}
	rangeTime := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                   string
		fullSync, fullyRemoved bool
	}{
		{name: "live replay keeps newer", fullyRemoved: false},
		{name: "full sync replay keeps newer", fullSync: true, fullyRemoved: false},
		{name: "live covering range removes", fullyRemoved: true},
		{name: "full sync covering range removes", fullSync: true, fullyRemoved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.MkdirAll(filepath.Join(root, "628111"), 0700))
			repo := &boundedDeleteRepo{fullyRemoved: tc.fullyRemoved}
			handler(context.Background(), NewDeviceInstance("slot", client, repo), &events.DeleteChat{
				JID: types.NewJID("628111", types.DefaultUserServer), Timestamp: rangeTime.Add(time.Hour), DeleteMedia: true, FromFullSync: tc.fullSync,
				Action: &waSyncAction.DeleteChatAction{MessageRange: &waSyncAction.SyncActionMessageRange{LastMessageTimestamp: new(rangeTime.Unix())}},
			})
			require.True(t, repo.bound.Equal(rangeTime), "bound=%s", repo.bound)
			require.Equal(t, []string{"628111@s.whatsapp.net"}, repo.jids)
			if tc.fullyRemoved {
				require.NoDirExists(t, filepath.Join(root, "628111"))
			} else {
				require.DirExists(t, filepath.Join(root, "628111"))
			}
		})
	}
	repo := &boundedDeleteRepo{fullyRemoved: true}
	eventTime := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	handler(context.Background(), NewDeviceInstance("slot", client, repo), &events.DeleteChat{JID: types.NewJID("628111", types.DefaultUserServer), Timestamp: eventTime})
	require.True(t, repo.bound.Equal(eventTime))
	repo = &boundedDeleteRepo{fullyRemoved: true}
	handler(context.Background(), NewDeviceInstance("slot", client, repo), &events.DeleteChat{JID: types.NewJID("628111", types.DefaultUserServer)})
	require.Nil(t, repo.jids, "an event without a bound must not purge")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "628111"), 0700))
	repo = &boundedDeleteRepo{fullyRemoved: true, referenced: true}
	handler(context.Background(), NewDeviceInstance("slot", client, repo), &events.DeleteChat{JID: types.NewJID("628111", types.DefaultUserServer), Timestamp: eventTime, DeleteMedia: true})
	require.DirExists(t, filepath.Join(root, "628111"), "shared media directory must remain")
}

func TestChatDeleteJIDsUsesOfflineContainer(t *testing.T) {
	old := db
	db = newTestSQLStore(t)
	t.Cleanup(func() { db = old })
	pn, lid := types.NewJID("628111", types.DefaultUserServer), types.NewJID("999111", types.HiddenUserServer)
	require.NoError(t, db.LIDMap.PutLIDMapping(context.Background(), lid, pn))
	for _, client := range []*whatsmeow.Client{nil, {}, {Store: &store.Device{}}} {
		for _, jid := range []types.JID{pn, lid} {
			got, err := ChatDeleteJIDs(context.Background(), client, jid)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{pn.String(), lid.String()}, got)
		}
	}
	db = nil
	got, err := ChatDeleteJIDs(context.Background(), nil, lid)
	require.NoError(t, err)
	require.Equal(t, []string{lid.String()}, got)
}

type lockedMediaDeleteRepo struct {
	deleteChatRepoSpy
	checked chan struct{}
	release chan struct{}
}

func (r *lockedMediaDeleteRepo) IsChatMediaDirectoryReferenced(string) (bool, error) {
	close(r.checked)
	<-r.release
	return false, nil
}
func TestPurgeLocalChatLocksReferenceCheckAndRemoval(t *testing.T) {
	old := config.PathMedia
	config.PathMedia = t.TempDir()
	t.Cleanup(func() { config.PathMedia = old })
	dir := filepath.Join(config.PathMedia, "111")
	require.NoError(t, os.Mkdir(dir, 0700))
	repo := &lockedMediaDeleteRepo{checked: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- PurgeLocalChat(repo, "device", []string{"111@s.whatsapp.net"}, true, nil) }()
	select {
	case <-repo.checked:
	case <-time.After(5 * time.Second):
		t.Fatal("reference check did not start")
	}
	available := utils.ChatMediaMutex.TryRLock()
	if available {
		utils.ChatMediaMutex.RUnlock()
	}
	close(repo.release)
	require.NoError(t, <-done)
	require.False(t, available, "media writes must be blocked during reference check and removal")
	require.NoDirExists(t, dir)
	require.True(t, utils.ChatMediaMutex.TryRLock())
	utils.ChatMediaMutex.RUnlock()
}
