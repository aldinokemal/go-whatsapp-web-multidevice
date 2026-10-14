package usecase

import (
	"context"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/stretchr/testify/require"
)

type lookupRepo struct {
	domainChatStorage.IChatStorageRepository
	byIDCalled       bool
	byDeviceID, byID string
	byDeviceCalled   bool

	getChatCalled          bool
	chatDeviceID, chatJID  string
	chatByDeviceExpiration uint32
}

func (r *lookupRepo) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	r.byIDCalled = true
	return &domainChatStorage.Message{ID: id, DeviceID: "someone-else"}, nil
}
func (r *lookupRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.byDeviceCalled = true
	r.byDeviceID, r.byID = deviceID, id
	return nil, nil
}

func (r *lookupRepo) GetChat(jid string) (*domainChatStorage.Chat, error) {
	r.getChatCalled = true
	return &domainChatStorage.Chat{JID: jid, DeviceID: "someone-else", EphemeralExpiration: 999}, nil
}
func (r *lookupRepo) GetChatByDevice(deviceID, jid string) (*domainChatStorage.Chat, error) {
	r.chatDeviceID, r.chatJID = deviceID, jid
	return &domainChatStorage.Chat{JID: jid, DeviceID: deviceID, EphemeralExpiration: r.chatByDeviceExpiration}, nil
}

func TestMessageLookupsAreDeviceScoped(t *testing.T) {
	repo := &lookupRepo{}
	inst := whatsapp.NewDeviceInstance("d_a1", nil, nil)
	ctx := whatsapp.ContextWithDevice(context.Background(), inst)
	msg, err := scopedMessageLookup(ctx, repo, "MSG1")
	require.NoError(t, err)
	require.Nil(t, msg, "another device's row is never returned")
	require.False(t, repo.byIDCalled, "the unscoped lookup is not used")
	require.Equal(t, "MSG1", repo.byID)
	// messages.device_id is written as inst.JID(), falling back to inst.ID()
	// (SQLiteRepository.CreateMessage); an unpaired instance has no JID.
	require.Equal(t, "d_a1", repo.byDeviceID)
}

func TestMessageLookupWithoutDeviceIsNotUnscoped(t *testing.T) {
	repo := &lookupRepo{}
	msg, err := scopedMessageLookup(context.Background(), repo, "MSG1")
	require.NoError(t, err)
	require.Nil(t, msg)
	require.False(t, repo.byIDCalled, "the unscoped lookup is not used")
	require.False(t, repo.byDeviceCalled, "no device means no lookup at all")
}

func TestDefaultEphemeralExpirationIsDeviceScoped(t *testing.T) {
	repo := &lookupRepo{chatByDeviceExpiration: 86400}
	service := serviceSend{chatStorageRepo: repo}
	inst := whatsapp.NewDeviceInstance("d_a1", nil, nil)
	ctx := whatsapp.ContextWithDevice(context.Background(), inst)

	got := service.getDefaultEphemeralExpiration(ctx, "628123456789@s.whatsapp.net")
	require.Equal(t, uint32(86400), got)
	require.False(t, repo.getChatCalled, "the unscoped GetChat is not used")
	require.Equal(t, "d_a1", repo.chatDeviceID)
	require.Equal(t, "628123456789@s.whatsapp.net", repo.chatJID)

	repo2 := &lookupRepo{}
	got = serviceSend{chatStorageRepo: repo2}.getDefaultEphemeralExpiration(context.Background(), "628123456789@s.whatsapp.net")
	require.Equal(t, uint32(0), got, "no device in context means no other account's setting leaks in")
	require.False(t, repo2.getChatCalled)
}
