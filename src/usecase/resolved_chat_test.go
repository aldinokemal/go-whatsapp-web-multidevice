package usecase

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

func storeTimedChat(t *testing.T, f *scopeFixture, jid types.JID) {
	t.Helper()
	storeChatWithTimer(t, f, jid, 86400)
}

func storeChatWithTimer(t *testing.T, f *scopeFixture, jid types.JID, timer uint32) {
	t.Helper()
	require.NoError(t, f.repo.StoreChat(&domainChatStorage.Chat{
		DeviceID: scopeDeviceA, JID: jid.String(), Name: "Ana", LastMessageTime: time.Now(), EphemeralExpiration: timer,
	}))
}

func TestForwardDurationOptionReadsTheResolvedChat(t *testing.T) {
	f := newScopeFixture(t)
	registered := types.NewJID("557312345678", types.DefaultUserServer)
	storeTimedChat(t, f, registered)

	// The request uses the other spelling of a Brazilian number.
	got := forwardDurationOption(scopeContext(scopeDeviceA), serviceSend{chatStorageRepo: f.repo},
		domainSend.ForwardRequest{Phone: "5573912345678"}, registered)

	require.NotNil(t, got)
	require.Equal(t, 86400, *got)
}

func TestForwardDurationOptionPrefersTheRequestedChat(t *testing.T) {
	f := newScopeFixture(t)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	phone := types.NewJID("628123456789", types.DefaultUserServer)
	storeTimedChat(t, f, lid)
	// The first send through the @lid request created the phone row with no timer.
	storeChatWithTimer(t, f, phone, 0)

	got := forwardDurationOption(scopeContext(scopeDeviceA), serviceSend{chatStorageRepo: f.repo},
		domainSend.ForwardRequest{Phone: lid.String()}, phone)

	require.NotNil(t, got)
	require.Equal(t, 86400, *got)
}

func TestForwardDurationOptionReadsTheRequestedChat(t *testing.T) {
	f := newScopeFixture(t)
	// A chat saved under @lid before its phone was known keeps the @lid key.
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	storeTimedChat(t, f, lid)

	got := forwardDurationOption(scopeContext(scopeDeviceA), serviceSend{chatStorageRepo: f.repo},
		domainSend.ForwardRequest{Phone: lid.String()}, types.NewJID("628123456789", types.DefaultUserServer))

	require.NotNil(t, got)
	require.Equal(t, 86400, *got)
}
