package whatsapp

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/purpshell/meowcaller"
	"github.com/rs/zerolog"
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

// TestCallerLoggerFollowsWhatsappLogLevel keeps the meowcaller log level tied to
// the server log level, while never going quieter than warnings. The config value
// is a package global, so this stays serial and restores it.
func TestCallerLoggerFollowsWhatsappLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  zerolog.Level
	}{
		{name: "debug surfaces relay and media diagnostics", level: "DEBUG", want: zerolog.DebugLevel},
		{name: "error keeps warnings visible", level: "ERROR", want: zerolog.WarnLevel},
		{name: "unknown level falls back to warnings", level: "loud", want: zerolog.WarnLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := config.WhatsappLogLevel
			config.WhatsappLogLevel = tt.level
			t.Cleanup(func() { config.WhatsappLogLevel = previous })

			require.Equal(t, tt.want, callerLogger().GetLevel())
		})
	}
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

// failingCallClient fails every placement without a live meowcaller client.
type failingCallClient struct{ err error }

func (c failingCallClient) Call(context.Context, string) (*meowcaller.Call, error) {
	return nil, c.err
}

// writeCallTestWAV writes a minimal 16-bit mono PCM WAV under the storage root
// and returns the relative path the call API accepts.
func writeCallTestWAV(t *testing.T, storageDir string) string {
	t.Helper()

	const (
		samples    = 960
		channels   = 1
		sampleRate = 16000
	)
	payload := make([]byte, samples*channels*2)

	header := make([]byte, 0, 44)
	header = append(header, "RIFF"...)
	header = binary.LittleEndian.AppendUint32(header, uint32(36+len(payload)))
	header = append(header, "WAVEfmt "...)
	header = binary.LittleEndian.AppendUint32(header, 16) // PCM chunk size
	header = binary.LittleEndian.AppendUint16(header, 1)  // PCM format
	header = binary.LittleEndian.AppendUint16(header, channels)
	header = binary.LittleEndian.AppendUint32(header, sampleRate)
	header = binary.LittleEndian.AppendUint32(header, sampleRate*channels*2) // byte rate
	header = binary.LittleEndian.AppendUint16(header, channels*2)            // block align
	header = binary.LittleEndian.AppendUint16(header, 16)                    // bits per sample
	header = append(header, "data"...)
	header = binary.LittleEndian.AppendUint32(header, uint32(len(payload)))

	const name = "voice.wav"
	require.NoError(t, os.WriteFile(filepath.Join(storageDir, name), append(header, payload...), 0o600))
	return name
}

// requireAudioSourceClosed proves the file handle was released: a closed
// decoder cannot yield frames.
func requireAudioSourceClosed(t *testing.T, source meowcaller.AudioSource) {
	t.Helper()
	_, err := source.ReadFrame()
	require.Error(t, err)
}

func TestCloseAudioSourceIgnoresNilSource(t *testing.T) {
	require.NotPanics(t, func() { closeAudioSource(nil) })
}

func TestPlaceCallClosesAudioSourceWhenRecorderFails(t *testing.T) {
	audioFile := writeCallTestWAV(t, useTempStorages(t))
	source, err := openCallAudioSource(audioFile)
	require.NoError(t, err)

	caller := &Caller{client: failingCallClient{err: errors.New("call must not be placed")}}
	_, err = caller.placeCall(context.Background(), "+628123456789", audioFile, source, "peer.mp3")
	require.ErrorContains(t, err, "must end in .wav")

	requireAudioSourceClosed(t, source)
}

func TestPlaceCallClosesAudioSourceWhenCallFails(t *testing.T) {
	audioFile := writeCallTestWAV(t, useTempStorages(t))
	source, err := openCallAudioSource(audioFile)
	require.NoError(t, err)

	caller := &Caller{client: failingCallClient{err: errors.New("relay unavailable")}}
	_, err = caller.placeCall(context.Background(), "+628123456789", audioFile, source, "")
	require.ErrorContains(t, err, "relay unavailable")

	requireAudioSourceClosed(t, source)
}
