package usecase

import (
	"context"
	"database/sql"
	"testing"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// Two accounts paired on one server share a group. Each stores its own copy of
// a group message: device A sent it, device B received it. These tests pin that
// a request made as one device only ever reads that device's copy.

const (
	scopeDeviceA = "628111111111@s.whatsapp.net"
	scopeDeviceB = "628222222222@s.whatsapp.net"
)

var (
	scopeGroup  = types.NewJID("120363000000000042", types.GroupServer)
	scopeMember = types.NewJID("628333333333", types.DefaultUserServer)
)

type scopeFixture struct {
	repo    domainChatStorage.IChatStorageRepository
	service serviceMessage
}

func newScopeFixture(t *testing.T) *scopeFixture {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())

	f := &scopeFixture{repo: repo}
	f.service = serviceMessage{
		chatStorageRepo: repo,
		validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
			return scopeGroup, nil
		},
	}
	return f
}

func (f *scopeFixture) storeChat(t *testing.T, deviceID string, ephemeral uint32) {
	t.Helper()
	require.NoError(t, f.repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:            deviceID,
		JID:                 scopeGroup.String(),
		Name:                "Shared group",
		LastMessageTime:     time.Now(),
		EphemeralExpiration: ephemeral,
	}))
}

func (f *scopeFixture) storeMessage(t *testing.T, message domainChatStorage.Message) {
	t.Helper()
	message.ChatJID = scopeGroup.String()
	message.Timestamp = time.Now()
	require.NoError(t, f.repo.StoreMessage(&message))
}

func scopeContext(deviceID string) context.Context {
	jid, _ := types.ParseJID(deviceID)
	client := &whatsmeow.Client{Store: &waStore.Device{ID: &jid}}
	return whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))
}

func TestDownloadMediaCannotReadAnotherDevicesMessage(t *testing.T) {
	f := newScopeFixture(t)
	f.storeChat(t, scopeDeviceA, 0)
	f.storeChat(t, scopeDeviceB, 0)
	// Only device A received this image (for example before device B joined
	// the group), so only device A holds its media keys.
	f.storeMessage(t, domainChatStorage.Message{
		ID:         "image-1",
		DeviceID:   scopeDeviceA,
		Sender:     scopeMember.String(),
		MediaType:  "image",
		URL:        "https://mmg.whatsapp.net/v/t62.7118-24/secret.enc",
		DirectPath: "/v/t62.7118-24/secret.enc",
		MediaKey:   []byte("device-a-media-key-0123456789abcd"),
	})

	_, err := f.service.DownloadMedia(scopeContext(scopeDeviceB), domainMessage.DownloadMediaRequest{
		MessageID: "image-1",
		Phone:     scopeGroup.String(),
	})

	require.ErrorContains(t, err, "not found")
}

func TestDownloadMediaRejectsMessageFromAnotherChat(t *testing.T) {
	f := newScopeFixture(t)
	otherGroup := types.NewJID("120363000000000099", types.GroupServer)
	require.NoError(t, f.repo.StoreChat(&domainChatStorage.Chat{
		DeviceID: scopeDeviceB, JID: otherGroup.String(), Name: "Other group", LastMessageTime: time.Now(),
	}))
	require.NoError(t, f.repo.StoreMessage(&domainChatStorage.Message{
		ID: "image-2", ChatJID: otherGroup.String(), DeviceID: scopeDeviceB, Sender: scopeMember.String(),
		MediaType: "image", DirectPath: "/v/t62.7118-24/other.enc", Timestamp: time.Now(),
	}))

	_, err := f.service.DownloadMedia(scopeContext(scopeDeviceB), domainMessage.DownloadMediaRequest{
		MessageID: "image-2",
		Phone:     scopeGroup.String(),
	})

	require.ErrorContains(t, err, "not found")
}

func TestDefaultEphemeralExpirationIsPerDevice(t *testing.T) {
	f := newScopeFixture(t)
	f.storeChat(t, scopeDeviceA, 86400)
	f.storeChat(t, scopeDeviceB, 0)
	service := serviceSend{chatStorageRepo: f.repo}

	require.Equal(t, uint32(86400), service.getDefaultEphemeralExpiration(scopeContext(scopeDeviceA), scopeGroup.String()))
	require.Equal(t, uint32(0), service.getDefaultEphemeralExpiration(scopeContext(scopeDeviceB), scopeGroup.String()))
	require.Equal(t, uint32(0), service.getDefaultEphemeralExpiration(context.Background(), scopeGroup.String()),
		"a caller with no device must not inherit some device's timer")
}

func TestListChatsRequiresADevice(t *testing.T) {
	f := newScopeFixture(t)
	f.storeChat(t, scopeDeviceA, 0)
	f.storeChat(t, scopeDeviceB, 0)

	response, err := NewChatService(f.repo).ListChats(context.Background(), domainChat.ListChatsRequest{})
	require.ErrorContains(t, err, "device identification required")
	require.Empty(t, response.Data)

	response, err = NewChatService(f.repo).ListChats(scopeContext(scopeDeviceB), domainChat.ListChatsRequest{})
	require.NoError(t, err)
	require.Len(t, response.Data, 1)
}
