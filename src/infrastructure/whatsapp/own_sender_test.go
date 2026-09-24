package whatsapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func clientWithID(jid types.JID) *whatsmeow.Client {
	return &whatsmeow.Client{Store: &waStore.Device{ID: &jid}}
}

// A linked companion is never device 0, so the raw Store.ID always carries a
// device suffix. That suffix must not reach chat storage.
func TestOwnSenderJIDStripsTheDeviceSuffix(t *testing.T) {
	jid := types.JID{User: "6281234567890", Server: types.DefaultUserServer, Device: 32}

	// Control: without normalisation this is what would be stored. If this ever
	// stops carrying the suffix the test below proves nothing, so assert it.
	require.Equal(t, "6281234567890:32@s.whatsapp.net", jid.String())

	assert.Equal(t, "6281234567890@s.whatsapp.net", OwnSenderJID(clientWithID(jid)))
}

func TestOwnSenderJIDLeavesAPlainJIDAlone(t *testing.T) {
	jid := types.JID{User: "6281234567890", Server: types.DefaultUserServer}
	assert.Equal(t, "6281234567890@s.whatsapp.net", OwnSenderJID(clientWithID(jid)))
}

// Callers store whatever comes back; an empty string is the existing "unknown
// sender" behaviour and must survive a nil client, store, or ID.
func TestOwnSenderJIDIsEmptyWhenUnavailable(t *testing.T) {
	assert.Empty(t, OwnSenderJID(nil))
	assert.Empty(t, OwnSenderJID(&whatsmeow.Client{}))
	assert.Empty(t, OwnSenderJID(&whatsmeow.Client{Store: &waStore.Device{}}))
}
