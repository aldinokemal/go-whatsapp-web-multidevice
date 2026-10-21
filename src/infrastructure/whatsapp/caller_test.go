package whatsapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/purpshell/meowcaller"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// useTempStorages points config.PathStorages at a temp dir so the call-media
// path confinement is exercised hermetically. The config value is a package
// global, so tests using it stay serial.
func useTempStorages(t *testing.T) string {
	t.Helper()
	previous := config.PathStorages
	directory := t.TempDir()
	config.PathStorages = directory
	t.Cleanup(func() { config.PathStorages = previous })
	return directory
}

func TestOpenCallAudioSourceValidatesInput(t *testing.T) {
	storageDir := useTempStorages(t)

	t.Run("empty file means a silent call", func(t *testing.T) {
		source, err := openCallAudioSource("   ")
		require.NoError(t, err)
		require.Nil(t, source)
	})

	t.Run("missing file is rejected generically", func(t *testing.T) {
		_, err := openCallAudioSource("absent.mp3")
		require.ErrorIs(t, err, ErrCallMediaPath)
		require.NotContains(t, err.Error(), "not readable")
	})

	t.Run("unsupported extension is rejected", func(t *testing.T) {
		path := filepath.Join(storageDir, "voice.ogg")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))

		_, err := openCallAudioSource("voice.ogg")
		require.ErrorContains(t, err, "unsupported audio file extension")
	})

	t.Run("upper-case extension still resolves the decoder", func(t *testing.T) {
		path := filepath.Join(storageDir, "voice.MP3")
		// A non-MP3 payload must fail inside the decoder, proving the extension
		// branch ran instead of being rejected as unsupported.
		require.NoError(t, os.WriteFile(path, []byte("not an mp3"), 0o600))

		_, err := openCallAudioSource("voice.MP3")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "unsupported audio file extension")
	})

	t.Run("absolute path is rejected", func(t *testing.T) {
		_, err := openCallAudioSource("/etc/passwd.mp3")
		require.ErrorIs(t, err, ErrCallMediaPath)
	})

	t.Run("parent traversal is rejected", func(t *testing.T) {
		_, err := openCallAudioSource("../escape.mp3")
		require.ErrorIs(t, err, ErrCallMediaPath)
	})

	t.Run("nested traversal is rejected", func(t *testing.T) {
		_, err := openCallAudioSource("sub/../../escape.mp3")
		require.ErrorIs(t, err, ErrCallMediaPath)
	})
}

func TestNewCallerRequiresBetaFlagAndClient(t *testing.T) {
	previous := config.WhatsappBetaCallEnabled
	t.Cleanup(func() { config.WhatsappBetaCallEnabled = previous })

	config.WhatsappBetaCallEnabled = false
	caller, err := NewCaller("device-1", nil)
	require.ErrorContains(t, err, "disabled")
	require.Nil(t, caller)

	config.WhatsappBetaCallEnabled = true
	caller, err = NewCaller("device-1", nil)
	require.ErrorContains(t, err, "whatsmeow client")
	require.Nil(t, caller)
}

func TestAttachCallerIsInertWhenDisabled(t *testing.T) {
	previous := config.WhatsappBetaCallEnabled
	t.Cleanup(func() { config.WhatsappBetaCallEnabled = previous })

	config.WhatsappBetaCallEnabled = false
	require.Nil(t, AttachCaller("disabled-device", nil))
	require.Nil(t, CallerForDevice("disabled-device"))
}

// swapCallers isolates the registry for a test; the caller map is package state.
func swapCallers(t *testing.T) {
	t.Helper()
	callersMu.Lock()
	previous := callers
	callers = map[string]*Caller{}
	callersMu.Unlock()
	t.Cleanup(func() {
		callersMu.Lock()
		callers = previous
		callersMu.Unlock()
	})
}

func registerCaller(key string) {
	callersMu.Lock()
	callers[key] = &Caller{calls: map[string]*meowcaller.Call{}}
	callersMu.Unlock()
}

func TestCallerKeyPrefersSessionIdentity(t *testing.T) {
	jid := types.NewJID("628123456789", types.DefaultUserServer)
	jid.Device = 7

	paired := &whatsmeow.Client{Store: &store.Device{ID: &jid}}
	require.Equal(t, jid.String(), callerKey("sales-slot", paired))
	require.Equal(t, "sales-slot", callerKey("sales-slot", &whatsmeow.Client{}))
	require.Equal(t, "sales-slot", callerKey("sales-slot", nil))
}

func TestCallerForInstanceFollowsRegistryReKeying(t *testing.T) {
	swapCallers(t)

	adJID := types.NewJID("628123456789", types.DefaultUserServer)
	adJID.Device = 7

	// The startup path attaches the caller under the session identity.
	registerCaller(adJID.String())

	// loadFromRegistry replaces the AD-JID slot with a named one; the session
	// identity, and therefore the attach key, stays the same.
	inst := &DeviceInstance{
		id:    "sales-slot",
		jid:   adJID.ToNonAD().String(),
		adJID: adJID.String(),
	}

	require.NotNil(t, CallerForInstance(inst))
	require.Nil(t, CallerForDevice(inst.ID()))
}

func TestCallerForInstanceFallsBackToSlotID(t *testing.T) {
	swapCallers(t)

	// A client attached before pairing is keyed by the slot id.
	registerCaller("unpaired-slot")

	require.NotNil(t, CallerForInstance(&DeviceInstance{id: "unpaired-slot"}))
	require.Nil(t, CallerForInstance(nil))
}

func TestCallerWebhookDeviceIDMatchesCallEvents(t *testing.T) {
	jid := types.NewJID("628123456789", types.DefaultUserServer)
	jid.Device = 7

	paired := &Caller{deviceID: "sales-slot", wa: &whatsmeow.Client{Store: &store.Device{ID: &jid}}}
	require.Equal(t, jid.ToNonAD().String(), paired.webhookDeviceID())

	unpaired := &Caller{deviceID: "sales-slot", wa: &whatsmeow.Client{}}
	require.Equal(t, "sales-slot", unpaired.webhookDeviceID())
}

func TestCallerLookupReportsUnknownCallID(t *testing.T) {
	caller := &Caller{calls: map[string]*meowcaller.Call{}}

	_, err := caller.lookup("  ")
	require.ErrorContains(t, err, "call id is required")

	_, err = caller.lookup("DOESNOTEXIST")
	require.ErrorIs(t, err, ErrCallNotFound)
}

func TestCreateBetaIncomingCallPayloadShape(t *testing.T) {
	peer := types.NewJID("628123456789", types.DefaultUserServer)

	payload := createBetaIncomingCallPayload(context.Background(), nil, "device-1", "CALL123", peer)

	require.Equal(t, "call.incoming", payload["event"])
	require.Equal(t, "device-1", payload["device_id"])
	require.NotEmpty(t, payload["timestamp"])

	body, ok := payload["payload"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "CALL123", body["call_id"])
	require.Equal(t, peer.ToNonAD().String(), body["from"])
	require.Equal(t, false, body["auto_rejected"])
	require.NotContains(t, body, "from_lid")
}

func TestCreateBetaIncomingCallPayloadMarksLIDPeers(t *testing.T) {
	peer := types.NewJID("99887766", types.HiddenUserServer)

	payload := createBetaIncomingCallPayload(context.Background(), nil, "", "CALL456", peer)

	require.NotContains(t, payload, "device_id")
	body, ok := payload["payload"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, peer.ToNonAD().String(), body["from_lid"])
}
