package whatsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
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
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"111@s.whatsapp.net", "../../222@lid", "no-digits@g.us"}, true))
	require.NoDirExists(t, filepath.Join(root, "111"))
	require.FileExists(t, filepath.Join(outside, "keep"))
	require.DirExists(t, filepath.Join(root, "333"))
	require.FileExists(t, filepath.Join(root, "flat-uuid"))
	require.DirExists(t, root)
	// Inner symbolic links must not allow removal outside the root either.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "444"), 0700))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "444", "linked")))
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"444@s.whatsapp.net"}, true))
	require.FileExists(t, filepath.Join(outside, "keep"))
	repo.referenced = true
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"333@s.whatsapp.net"}, true))
	require.DirExists(t, filepath.Join(root, "333"))
	repo.referenced = false
	require.NoError(t, PurgeLocalChat(repo, "device", []string{"333@s.whatsapp.net"}, false))
	require.DirExists(t, filepath.Join(root, "333"))
	repo.err = errors.New("database failed")
	require.Error(t, PurgeLocalChat(repo, "device", []string{"333@s.whatsapp.net"}, true))
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
	handler(context.Background(), instance, &events.DeleteChat{JID: lid, DeleteMedia: true})
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
