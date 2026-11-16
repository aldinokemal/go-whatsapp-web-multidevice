package usecase

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

type deleteChatUsecaseRepo struct {
	domainChatStorage.IChatStorageRepository
	message             *domainChatStorage.Message
	lookupErr, purgeErr error
	purged              bool
}

func (r *deleteChatUsecaseRepo) GetMessages(f *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	if f.DeviceID != "628999@s.whatsapp.net" || f.ChatJID != "628111@s.whatsapp.net" || f.Limit != 1 {
		return nil, errors.New("incorrect lookup scope")
	}
	if r.message == nil {
		return nil, r.lookupErr
	}
	return []*domainChatStorage.Message{r.message}, r.lookupErr
}
func (r *deleteChatUsecaseRepo) DeleteChatByDevice(device, jid string, aliases ...string) error {
	if device != "628999@s.whatsapp.net" || jid != "628111@s.whatsapp.net" {
		return errors.New("incorrect purge scope")
	}
	r.purged = true
	return r.purgeErr
}
func deleteChatTestContext(client *whatsmeow.Client) context.Context {
	return whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("628999@s.whatsapp.net", client, nil))
}
func deleteChatTestClient() *whatsmeow.Client {
	id := types.NewJID("628999", types.DefaultUserServer)
	return &whatsmeow.Client{Store: &store.Device{ID: &id}}
}

func TestDeleteChatPhoneFirstAndErrors(t *testing.T) {
	old := config.PathMedia
	config.PathMedia = filepath.Join(t.TempDir(), "absent")
	t.Cleanup(func() { config.PathMedia = old })
	sendErr, purgeErr := errors.New("send failed"), errors.New("purge failed")
	for _, tc := range []struct {
		name                string
		sendErr, purgeErr   error
		noMessage           bool
		wantCode            string
		wantSend, wantPurge bool
	}{
		{name: "success", wantSend: true, wantPurge: true},
		{name: "send failure", sendErr: sendErr, wantSend: true},
		{name: "local failure", purgeErr: purgeErr, wantCode: "LOCAL_DELETE_FAILED", wantSend: true, wantPurge: true},
		{name: "missing anchor", noMessage: true, wantCode: "VALIDATION_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &deleteChatUsecaseRepo{purgeErr: tc.purgeErr}
			ts := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
			if !tc.noMessage {
				repo.message = &domainChatStorage.Message{ID: "last", Timestamp: ts, IsFromMe: true}
			}
			sent := false
			service := serviceChat{chatStorageRepo: repo, sendDeleteChatPatch: func(_ context.Context, p appstate.PatchInfo) error {
				require.False(t, repo.purged, "purge must follow send")
				sent = true
				require.Equal(t, []string{appstate.IndexDeleteChat, "628111@s.whatsapp.net", "1"}, p.Mutations[0].Index)
				messageRange := p.Mutations[0].Value.GetDeleteChatAction().GetMessageRange()
				require.Equal(t, ts.Unix(), messageRange.GetLastMessageTimestamp())
				require.Equal(t, "last", messageRange.GetMessages()[0].GetKey().GetID())
				return tc.sendErr
			}}
			response, err := service.DeleteChat(deleteChatTestContext(deleteChatTestClient()), domainChat.DeleteChatRequest{ChatJID: "628111@s.whatsapp.net"})
			require.Equal(t, tc.wantSend, sent)
			require.Equal(t, tc.wantPurge, repo.purged)
			switch {
			case tc.wantCode != "":
				var coded pkgError.GenericError
				require.ErrorAs(t, err, &coded)
				require.Equal(t, tc.wantCode, coded.ErrCode())
				if tc.wantCode == "LOCAL_DELETE_FAILED" {
					require.Equal(t, 500, coded.StatusCode())
					require.Contains(t, err.Error(), "deleted on WhatsApp but the local copy was not fully removed")
				}
			case tc.sendErr != nil:
				require.ErrorIs(t, err, tc.sendErr)
			default:
				require.NoError(t, err)
				require.Equal(t, "success", response.Status)
			}
		})
	}
}

func TestDeleteChatLocalOnlyWithoutClientOrAnchor(t *testing.T) {
	for _, purgeErr := range []error{nil, errors.New("purge failed")} {
		repo := &deleteChatUsecaseRepo{purgeErr: purgeErr}
		service := serviceChat{chatStorageRepo: repo, sendDeleteChatPatch: func(context.Context, appstate.PatchInfo) error {
			t.Fatal("local_only must never send app state")
			return nil
		}}
		response, err := service.DeleteChat(deleteChatTestContext(nil), domainChat.DeleteChatRequest{ChatJID: "628111@s.whatsapp.net", LocalOnly: true, DeleteMedia: new(false)})
		require.True(t, repo.purged)
		if purgeErr == nil {
			require.NoError(t, err)
			require.True(t, response.LocalOnly)
		} else {
			var coded pkgError.GenericError
			require.ErrorAs(t, err, &coded)
			require.Equal(t, 500, coded.StatusCode())
			require.Equal(t, "INTERNAL_SERVER_ERROR", coded.ErrCode())
		}
	}
}

func TestDeleteChatLocalOnlyPurgesStoredRowsAndMedia(t *testing.T) {
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chat.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())
	old := config.PathMedia
	config.PathMedia = t.TempDir()
	t.Cleanup(func() { config.PathMedia = old })
	for _, jid := range []string{"628111@s.whatsapp.net", "628222@s.whatsapp.net"} {
		require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{DeviceID: "628999@s.whatsapp.net", JID: jid}))
	}
	media := filepath.Join(config.PathMedia, "628111")
	require.NoError(t, os.Mkdir(media, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(media, "download"), []byte("media"), 0600))
	service := serviceChat{chatStorageRepo: repo, sendDeleteChatPatch: func(context.Context, appstate.PatchInfo) error { t.Fatal("unexpected app state"); return nil }}
	_, err = service.DeleteChat(deleteChatTestContext(nil), domainChat.DeleteChatRequest{ChatJID: "628111@s.whatsapp.net", LocalOnly: true})
	require.NoError(t, err)
	require.NoDirExists(t, media)
	deleted, err := repo.GetChatByDevice("628999@s.whatsapp.net", "628111@s.whatsapp.net")
	require.NoError(t, err)
	require.Nil(t, deleted)
	other, err := repo.GetChatByDevice("628999@s.whatsapp.net", "628222@s.whatsapp.net")
	require.NoError(t, err)
	require.NotNil(t, other)
}

func TestDeleteChatDisconnectedChangesNothing(t *testing.T) {
	repo := &deleteChatUsecaseRepo{message: &domainChatStorage.Message{ID: "last", Timestamp: time.Now()}}
	service := serviceChat{chatStorageRepo: repo}
	_, err := service.DeleteChat(deleteChatTestContext(deleteChatTestClient()), domainChat.DeleteChatRequest{ChatJID: "628111@s.whatsapp.net"})
	require.ErrorIs(t, err, pkgError.ErrNotConnected)
	require.False(t, repo.purged)
}

func TestDeleteChatMessageKey(t *testing.T) {
	for _, tc := range []struct {
		name, chat, sender string
		fromMe             bool
		participant        string
	}{
		{name: "own direct", chat: "628111@s.whatsapp.net", sender: "628999:2@s.whatsapp.net", fromMe: true},
		{name: "other direct", chat: "628111@s.whatsapp.net", sender: "628111:3@s.whatsapp.net"},
		{name: "own group", chat: "120123@g.us", sender: "628999:2@s.whatsapp.net", fromMe: true},
		{name: "other group", chat: "120123@g.us", sender: "628111:3@s.whatsapp.net", participant: "628111@s.whatsapp.net"},
		{name: "other LID group", chat: "120123@g.us", sender: "999111:3@lid", participant: "999111@lid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat, err := types.ParseJID(tc.chat)
			require.NoError(t, err)
			key, err := deleteChatMessageKey(context.Background(), nil, chat, &domainChatStorage.Message{ID: "last", Sender: tc.sender, IsFromMe: tc.fromMe})
			require.NoError(t, err)
			require.Equal(t, tc.chat, key.GetRemoteJID())
			require.Equal(t, tc.fromMe, key.GetFromMe())
			require.Equal(t, "last", key.GetID())
			require.Equal(t, tc.participant, key.GetParticipant())
		})
	}
}

type deleteChatLIDStore struct {
	store.LIDStore
	pn, lid types.JID
}

func (s *deleteChatLIDStore) GetPNForLID(context.Context, types.JID) (types.JID, error) {
	return s.pn, nil
}
func (s *deleteChatLIDStore) GetLIDForPN(context.Context, types.JID) (types.JID, error) {
	return s.lid, nil
}

type deleteChatAliasRepo struct {
	domainChatStorage.IChatStorageRepository
	messages map[string]*domainChatStorage.Message
	deleted  []string
}

func (r *deleteChatAliasRepo) GetMessages(f *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	if f.DeviceID != "628999@s.whatsapp.net" || f.Limit != 1 {
		return nil, errors.New("incorrect device scope")
	}
	if m := r.messages[f.ChatJID]; m != nil {
		return []*domainChatStorage.Message{m}, nil
	}
	return nil, nil
}
func (r *deleteChatAliasRepo) DeleteChatByDevice(device, jid string, aliases ...string) error {
	if device != "628999@s.whatsapp.net" {
		return errors.New("incorrect device scope")
	}
	r.deleted = append([]string{jid}, aliases...)
	return nil
}
func TestDeleteChatUsesNewestAnchorAcrossPNAndLID(t *testing.T) {
	pn, lid := types.NewJID("628111", types.DefaultUserServer), types.NewJID("999111", types.HiddenUserServer)
	client := deleteChatTestClient()
	client.Store.LIDs = &deleteChatLIDStore{pn: pn, lid: lid}
	now := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	for _, target := range []types.JID{pn, lid} {
		repo := &deleteChatAliasRepo{messages: map[string]*domainChatStorage.Message{
			pn.String(): {ID: "old", Timestamp: now}, lid.String(): {ID: "new", Timestamp: now.Add(time.Minute), Sender: lid.String()},
		}}
		service := serviceChat{chatStorageRepo: repo, sendDeleteChatPatch: func(_ context.Context, p appstate.PatchInfo) error {
			require.Nil(t, repo.deleted)
			require.Equal(t, []string{appstate.IndexDeleteChat, pn.String(), "0"}, p.Mutations[0].Index)
			last := p.Mutations[0].Value.GetDeleteChatAction().GetMessageRange().GetMessages()[0]
			require.Equal(t, "new", last.GetKey().GetID())
			require.Equal(t, pn.String(), last.GetKey().GetRemoteJID())
			require.False(t, last.GetKey().GetFromMe())
			require.Equal(t, now.Add(time.Minute).Unix(), last.GetTimestamp())
			return nil
		}}
		_, err := service.DeleteChat(deleteChatTestContext(client), domainChat.DeleteChatRequest{ChatJID: target.String(), DeleteMedia: new(false)})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{pn.String(), lid.String()}, repo.deleted)
	}
	group := types.NewJID("120123", types.GroupServer)
	key, err := deleteChatMessageKey(context.Background(), client, group, &domainChatStorage.Message{ID: "id", Sender: lid.String()})
	require.NoError(t, err)
	require.Equal(t, pn.String(), key.GetParticipant())
}

func TestDeleteChatMediaFailureCodes(t *testing.T) {
	old := config.PathMedia
	config.PathMedia = filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(config.PathMedia, nil, 0600))
	t.Cleanup(func() { config.PathMedia = old })
	for _, localOnly := range []bool{false, true} {
		repo := &deleteChatUsecaseRepo{message: &domainChatStorage.Message{ID: "last", Timestamp: time.Now()}}
		service := serviceChat{chatStorageRepo: repo, sendDeleteChatPatch: func(context.Context, appstate.PatchInfo) error {
			require.False(t, localOnly, "local_only must not send")
			return nil
		}}
		_, err := service.DeleteChat(deleteChatTestContext(deleteChatTestClient()), domainChat.DeleteChatRequest{ChatJID: "628111@s.whatsapp.net", LocalOnly: localOnly})
		require.True(t, repo.purged)
		var coded pkgError.GenericError
		require.ErrorAs(t, err, &coded)
		require.Equal(t, 500, coded.StatusCode())
		want := "LOCAL_DELETE_FAILED"
		if localOnly {
			want = "INTERNAL_SERVER_ERROR"
		}
		require.Equal(t, want, coded.ErrCode())
	}
}
