package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/purpshell/meowcaller"
	"github.com/rs/zerolog"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// Beta voice-call surface built on meowcaller. The library installs its own raw
// <call> node handlers through reflection, so a Caller must be attached to a
// whatsmeow client before that client connects. Everything below is inert until
// config.WhatsappBetaCallEnabled is on.

var (
	callersMu sync.RWMutex
	// callers maps a device id to the meowcaller client wrapping that device.
	callers = map[string]*Caller{}
)

// ErrCallNotFound reports an unknown or already-finished call id.
var ErrCallNotFound = errors.New("call not found")

// ErrCallMediaPath reports a client-supplied call media path that is not usable
// under config.PathStorages. It stays generic so callers cannot probe the host
// filesystem; the underlying cause is only logged server-side.
var ErrCallMediaPath = errors.New("call media path is invalid")

// callClient is the slice of the meowcaller client the caller stack uses. The
// interface keeps call placement stubbable in tests; *meowcaller.Client is the
// only production implementation.
type callClient interface {
	Call(ctx context.Context, target string) (*meowcaller.Call, error)
}

// Caller owns the meowcaller client for one whatsmeow session and tracks its
// live calls so the REST layer can act on them by call id.
type Caller struct {
	deviceID string
	client   callClient
	wa       *whatsmeow.Client

	mu       sync.Mutex
	calls    map[string]*meowcaller.Call
	sinks    map[string]meowcaller.AudioSink
	endHooks map[string][]func()
	live     map[string]bool
}

// NewCaller wraps an already-constructed but not yet connected whatsmeow client.
// It returns an error when the beta flag is off or meowcaller cannot hook the
// client, and callers are expected to log that and continue without voice calls.
func NewCaller(deviceID string, wa *whatsmeow.Client) (*Caller, error) {
	if !config.WhatsappBetaCallEnabled {
		return nil, errors.New("beta voice calls are disabled")
	}
	if wa == nil {
		return nil, errors.New("beta voice calls need a whatsmeow client")
	}
	// Constructing the meowcaller client installs the raw call handlers; it must
	// happen before Connect or the receive loop misses the first stanzas.
	mc := meowcaller.NewClient(wa, meowcaller.WithLogger(callerLogger()))
	c := &Caller{
		deviceID: deviceID,
		client:   mc,
		wa:       wa,
		calls:    map[string]*meowcaller.Call{},
		sinks:    map[string]meowcaller.AudioSink{},
		endHooks: map[string][]func(){},
		live:     map[string]bool{},
	}
	mc.OnIncomingCall(c.onIncomingCall)
	return c, nil
}

// AttachCaller builds the beta call stack for a device and registers it, or
// returns nil when the feature is off. Registration replaces any previous
// caller for the same session, matching the client replacement rules.
func AttachCaller(deviceID string, wa *whatsmeow.Client) *Caller {
	if !config.WhatsappBetaCallEnabled {
		return nil
	}
	caller, err := NewCaller(deviceID, wa)
	if err != nil {
		logrus.WithError(err).Warnf("[CALL] beta voice calls unavailable for device %s", deviceID)
		return nil
	}
	callersMu.Lock()
	callers[callerKey(deviceID, wa)] = caller
	callersMu.Unlock()
	return caller
}

// callerKey prefers the session identity carried by the client. Registry reloads
// may re-key a device slot, but the WhatsApp session identity never changes, so
// attaching and looking up by it keeps both paths consistent.
func callerKey(deviceID string, wa *whatsmeow.Client) string {
	if wa != nil && wa.Store != nil && wa.Store.ID != nil {
		return wa.Store.ID.String()
	}
	return deviceID
}

// CallerForDevice returns the beta call stack registered for a device, if any.
func CallerForDevice(deviceID string) *Caller {
	callersMu.RLock()
	defer callersMu.RUnlock()
	return callers[deviceID]
}

// CallerForInstance resolves the caller for a slot, trying the identities the
// attach paths may have used. The AD JID wins because it pins the exact
// companion session; the slot id covers a client attached before pairing.
func CallerForInstance(inst *DeviceInstance) *Caller {
	if inst == nil {
		return nil
	}
	for _, key := range []string{inst.ADJID(), inst.JID(), inst.ID()} {
		if key == "" {
			continue
		}
		if caller := CallerForDevice(key); caller != nil {
			return caller
		}
	}
	return nil
}

// callerLogger adapts logrus to the zerolog logger meowcaller expects. Warnings
// and above are always surfaced so call media logs do not flood the server, but
// APP_DEBUG=true or WHATSAPP_LOG_LEVEL=DEBUG raises it to the library's relay and
// media diagnostics, which is the only way to tell "no inbound RTP" from a local
// audio problem.
func callerLogger() zerolog.Logger {
	level := zerolog.WarnLevel
	if parsed, err := zerolog.ParseLevel(strings.ToLower(config.WhatsappLogLevel)); err == nil && parsed < level {
		level = parsed
	}
	return zerolog.New(callerLogWriter{}).Level(level).With().Timestamp().Logger()
}

type callerLogWriter struct{}

func (callerLogWriter) Write(p []byte) (int, error) {
	logrus.Warnf("[CALL] %s", strings.TrimSpace(string(p)))
	return len(p), nil
}

// StartCall places a 1:1 audio call to target. audioFile, when set, is streamed
// to the peer once media is ready; recordPath, when set, saves the peer's audio
// to a WAV file. Both paths resolve relative to config.PathStorages. meowcaller
// keeps sending silence when no source is playing, so a call without an audio
// file still holds the relay bridge.
func (c *Caller) StartCall(
	ctx context.Context,
	target string,
	audioFile string,
	recordPath string,
) (string, error) {
	if !config.WhatsappBetaCallEnabled {
		return "", errors.New("beta voice calls are disabled")
	}
	if c == nil || c.client == nil {
		return "", errors.New("beta voice calls are not available for this device")
	}
	audioFile = strings.TrimSpace(audioFile)
	source, err := openCallAudioSource(audioFile)
	if err != nil {
		return "", err
	}
	return c.placeCall(
		ctx,
		target,
		audioFile,
		source,
		recordPath,
	)
}

// placeCall places the call with an already-opened outbound source. The source
// is closed on every path that does not hand it to the call's player, and by
// the call's end callback once the media is done.
func (c *Caller) placeCall(
	ctx context.Context,
	target string,
	audioFile string,
	source meowcaller.AudioSource,
	recordPath string,
) (string, error) {
	recorder, err := openCallRecorder(recordPath)
	if err != nil {
		closeAudioSource(source)
		return "", err
	}

	call, err := c.client.Call(ctx, target)
	if err != nil {
		closeAudioSource(source)
		closeAudioSink(recorder)
		return "", fmt.Errorf("failed to place call: %w", err)
	}
	c.track(call)
	if recorder != nil {
		call.Receive(recorder)
		c.setSink(call.ID(), recorder)
	}

	if source != nil {
		call.OnReady(func() {
			// A live browser stream attached before the peer answered owns
			// playback; playing the file on top of it would silence the mic.
			if c.usesLiveAudio(call.ID()) {
				closeAudioSource(source)
				return
			}
			call.Play(source)
			logrus.Infof("[CALL] streaming %s to %s (call %s)", audioFile, target, call.ID())
		})
	}
	call.OnEnd(func(reason string) {
		closeAudioSource(source)
		c.forget(call.ID())
		logrus.Infof("[CALL] call %s with %s ended: %s", call.ID(), target, reason)
	})

	logrus.Infof("[CALL] placed call %s to %s", call.ID(), target)
	return call.ID(), nil
}

// AnswerCall accepts a ringing inbound call by id. recordPath, when set, saves
// the peer's audio to a WAV file relative to config.PathStorages.
func (c *Caller) AnswerCall(callID string, recordPath string) error {
	call, err := c.lookup(callID)
	if err != nil {
		return err
	}
	recorder, err := openCallRecorder(recordPath)
	if err != nil {
		return err
	}
	if recorder != nil {
		call.Receive(recorder)
		c.setSink(call.ID(), recorder)
	}
	if err := call.Answer(); err != nil {
		c.dropSink(call.ID())
		return fmt.Errorf("failed to answer call %s: %w", callID, err)
	}
	logrus.Infof("[CALL] answered call %s", callID)
	return nil
}

// HangupCall ends an active call by id.
func (c *Caller) HangupCall(callID string) error {
	call, err := c.lookup(callID)
	if err != nil {
		return err
	}
	if err := call.Hangup(); err != nil {
		return fmt.Errorf("failed to hang up call %s: %w", callID, err)
	}
	c.forget(callID)
	logrus.Infof("[CALL] hung up call %s", callID)
	return nil
}

// AttachAudioStream sends s16le 16 kHz mono frames read from mic to the peer and
// forwards the peer's decoded audio to onPeerAudio. Closing mic ends the outbound
// stream and the call falls back to silence. A live stream replaces any previous
// player or sink attached to the call.
func (c *Caller) AttachAudioStream(callID string, mic <-chan []byte, onPeerAudio func([]float32)) error {
	call, err := c.lookup(callID)
	if err != nil {
		return err
	}
	call.Play(newChannelAudioSource(mic))
	if onPeerAudio != nil {
		call.Receive(meowcaller.SinkFunc(onPeerAudio))
	}
	c.mu.Lock()
	if c.live == nil {
		c.live = map[string]bool{}
	}
	c.live[callID] = true
	c.mu.Unlock()
	logrus.Infof("[CALL] live audio attached to call %s", callID)
	return nil
}

// usesLiveAudio reports whether a browser audio stream owns the call's playback.
func (c *Caller) usesLiveAudio(callID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live[callID]
}

// WatchCallEnd runs fn once the call ends. Unknown call ids fire immediately so
// a stream handler never leaks waiting for a call that is already gone.
func (c *Caller) WatchCallEnd(callID string, fn func()) {
	callID = strings.TrimSpace(callID)
	c.mu.Lock()
	if _, ok := c.calls[callID]; ok && callID != "" {
		c.endHooks[callID] = append(c.endHooks[callID], fn)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	fn()
}

// lookup resolves a tracked call, reporting a missing id explicitly so the REST
// layer can answer with a validation error instead of a generic failure.
func (c *Caller) lookup(callID string) (*meowcaller.Call, error) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, errors.New("call id is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	call, ok := c.calls[callID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrCallNotFound, callID)
	}
	return call, nil
}

// onIncomingCall records the ringing call and forwards it to the configured
// webhooks. It never answers on its own: the operator decides via the answer
// endpoint.
func (c *Caller) onIncomingCall(call *meowcaller.Call) {
	if call == nil {
		return
	}
	c.track(call)
	call.OnEnd(func(reason string) {
		c.forget(call.ID())
		logrus.Infof("[CALL] incoming call %s ended: %s", call.ID(), reason)
	})
	logrus.Infof("[CALL] incoming call %s from %s", call.ID(), call.Peer().String())

	peer := call.Peer()
	callID := call.ID()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		payload := createBetaIncomingCallPayload(ctx, c.wa, c.webhookDeviceID(), callID, peer)
		// Ring the dashboard first: webhook delivery may retry for a while and
		// the caller needs to see Answer as soon as the call arrives.
		notifyIncomingCallUI(incomingCallUIBroadcastFromPayload(payload, c.deviceID))
		if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "call.incoming"); err != nil {
			logrus.WithError(err).Errorf("[CALL] failed to forward incoming call %s to webhook", callID)
		}
	}()
}

// IncomingCallUIBroadcast is the dashboard view of an inbound call lifecycle.
// DeviceID is the registry slot id (usable as X-Device-Id); DeviceJID is the
// bare-number session identity other call events report.
type IncomingCallUIBroadcast struct {
	Event     string `json:"event"`
	CallID    string `json:"call_id"`
	From      string `json:"from"`
	FromLID   string `json:"from_lid,omitempty"`
	DeviceID  string `json:"device_id"`
	DeviceJID string `json:"device_jid,omitempty"`
}

const (
	// IncomingCallEventRinging announces a call waiting to be answered.
	IncomingCallEventRinging = "ringing"
	// IncomingCallEventEnded clears the ring when the caller gives up.
	IncomingCallEventEnded = "ended"
)

var (
	incomingCallUIMu       sync.RWMutex
	incomingCallUIListener func(IncomingCallUIBroadcast)
)

// SetIncomingCallUIListener registers the dashboard bridge for inbound rings.
// The whatsapp package never imports the UI layer; the rest wiring calls this
// with the dashboard websocket hub. Passing nil clears the listener.
func SetIncomingCallUIListener(fn func(IncomingCallUIBroadcast)) {
	incomingCallUIMu.Lock()
	incomingCallUIListener = fn
	incomingCallUIMu.Unlock()
}

func notifyIncomingCallUI(notification IncomingCallUIBroadcast) {
	incomingCallUIMu.RLock()
	listener := incomingCallUIListener
	incomingCallUIMu.RUnlock()
	if listener == nil {
		return
	}
	defer func() {
		// A dashboard bug must never break the call receive path.
		if recovered := recover(); recovered != nil {
			logrus.Errorf("[CALL] incoming-call UI listener failed: %v", recovered)
		}
	}()
	listener(notification)
}

func incomingCallUIBroadcastFromPayload(payload map[string]any, deviceID string) IncomingCallUIBroadcast {
	notification := IncomingCallUIBroadcast{
		Event:    IncomingCallEventRinging,
		DeviceID: deviceID,
	}
	if deviceJID, ok := payload["device_id"].(string); ok {
		notification.DeviceJID = deviceJID
	}
	body, ok := payload["payload"].(map[string]any)
	if !ok {
		return notification
	}
	notification.CallID, _ = body["call_id"].(string)
	notification.From, _ = body["from"].(string)
	notification.FromLID, _ = body["from_lid"].(string)
	return notification
}

// webhookDeviceID mirrors the other call events: the bare-number JID once the
// session is paired, and the attach-time id while it is not.
func (c *Caller) webhookDeviceID() string {
	if c.wa != nil && c.wa.Store != nil && c.wa.Store.ID != nil {
		return c.wa.Store.ID.ToNonAD().String()
	}
	return c.deviceID
}

// createBetaIncomingCallPayload mirrors the call.offer webhook shape so existing
// consumers can handle beta calls without a new parser.
func createBetaIncomingCallPayload(
	ctx context.Context,
	client *whatsmeow.Client,
	deviceID string,
	callID string,
	peer types.JID,
) map[string]any {
	payload := map[string]any{
		"call_id": callID,
	}

	// NormalizeJIDFromLID needs the live client; without one (e.g. an offline
	// caller) the raw peer JID is still the best identity we can report.
	normalizedPeer := peer
	if client != nil {
		normalizedPeer = NormalizeJIDFromLID(ctx, peer, client)
	}
	payload["from"] = normalizedPeer.ToNonAD().String()
	if peer.Server == types.HiddenUserServer {
		payload["from_lid"] = peer.ToNonAD().String()
	}
	addSenderDisplayName(ctx, client, payload, false, "")
	// The beta stack answers explicitly through the answer endpoint.
	payload["auto_rejected"] = false

	body := map[string]any{
		"event":     "call.incoming",
		"timestamp": time.Now().Format(time.RFC3339),
		"payload":   payload,
	}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	return body
}

func (c *Caller) track(call *meowcaller.Call) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[call.ID()] = call
}

// setSink records the peer-audio sink attached to a call, closing any previous
// one so recorders finalize their WAV header.
func (c *Caller) setSink(callID string, sink meowcaller.AudioSink) {
	c.mu.Lock()
	previous := c.sinks[callID]
	c.sinks[callID] = sink
	c.mu.Unlock()
	closeAudioSink(previous)
}

// dropSink detaches and closes the peer-audio sink of a call.
func (c *Caller) dropSink(callID string) {
	c.mu.Lock()
	sink := c.sinks[callID]
	delete(c.sinks, callID)
	c.mu.Unlock()
	closeAudioSink(sink)
}

// forget removes a finished call, closes its sink, and fires its end hooks.
func (c *Caller) forget(callID string) {
	c.mu.Lock()
	delete(c.calls, callID)
	delete(c.live, callID)
	sink := c.sinks[callID]
	delete(c.sinks, callID)
	hooks := c.endHooks[callID]
	delete(c.endHooks, callID)
	c.mu.Unlock()

	closeAudioSink(sink)
	for _, fn := range hooks {
		fn()
	}
	notifyIncomingCallUI(IncomingCallUIBroadcast{
		Event:    IncomingCallEventEnded,
		CallID:   callID,
		DeviceID: c.deviceID,
	})
}

// resolveCallMediaPath confines a client-supplied call media path to
// config.PathStorages. Absolute paths and relative paths that normalize outside
// the directory are rejected.
//
// ponytail: symlinks inside the directory can still point elsewhere; add
// EvalSymlinks if untrusted writers can create files under PathStorages.
func resolveCallMediaPath(raw string) (string, error) {
	if raw == "" {
		return "", ErrCallMediaPath
	}
	if filepath.IsAbs(raw) {
		return "", ErrCallMediaPath
	}
	base, err := filepath.Abs(config.PathStorages)
	if err != nil {
		return "", ErrCallMediaPath
	}
	resolved := filepath.Clean(filepath.Join(base, raw))
	relative, err := filepath.Rel(base, resolved)
	escapes := relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
	if err != nil || relative == "." || escapes {
		return "", ErrCallMediaPath
	}
	return resolved, nil
}

// openCallRecorder opens an optional WAV recorder for the peer's audio. It is nil
// when recordPath is empty; a non-.wav path is rejected so the file extension
// does not lie about its contents.
func openCallRecorder(recordPath string) (meowcaller.AudioSink, error) {
	recordPath = strings.TrimSpace(recordPath)
	if recordPath == "" {
		return nil, nil
	}
	if strings.ToLower(filepath.Ext(recordPath)) != ".wav" {
		return nil, fmt.Errorf("record path %q must end in .wav", recordPath)
	}
	path, err := resolveCallMediaPath(recordPath)
	if err != nil {
		logrus.WithField("path", recordPath).Warn("[CALL] rejected record path outside storages")
		return nil, err
	}
	recorder, err := meowcaller.WAVRecorder(path)
	if err != nil {
		logrus.WithError(err).WithField("path", path).Warn("[CALL] failed to open recorder")
		return nil, ErrCallMediaPath
	}
	return recorder, nil
}

func closeAudioSink(sink meowcaller.AudioSink) {
	if sink == nil {
		return
	}
	if err := sink.Close(); err != nil {
		logrus.WithError(err).Warn("[CALL] failed to close call audio sink")
	}
}

// closeAudioSource releases the decoder/file resources of an outbound audio
// source. Closing repeats safely, so the failure paths and the call's end
// callback may both close the same source.
func closeAudioSource(source meowcaller.AudioSource) {
	if source == nil {
		return
	}
	if err := source.Close(); err != nil {
		logrus.WithError(err).Warn("[CALL] failed to close call audio source")
	}
}

// openCallAudioSource opens the optional outbound audio file. A nil source means
// the call carries no media; an unsupported extension is a caller error.
func openCallAudioSource(audioFile string) (meowcaller.AudioSource, error) {
	audioFile = strings.TrimSpace(audioFile)
	if audioFile == "" {
		return nil, nil
	}
	extension := strings.ToLower(filepath.Ext(audioFile))
	if extension != ".mp3" && extension != ".wav" {
		return nil, fmt.Errorf("unsupported audio file extension for %q (want .mp3 or .wav)", audioFile)
	}
	path, err := resolveCallMediaPath(audioFile)
	if err != nil {
		logrus.WithField("path", audioFile).Warn("[CALL] rejected audio path outside storages")
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		logrus.WithError(err).WithField("path", path).Warn("[CALL] call audio file is unavailable")
		return nil, ErrCallMediaPath
	}

	switch extension {
	case ".mp3":
		source, err := meowcaller.MP3File(path)
		if err != nil {
			logrus.WithError(err).WithField("path", path).Warn("[CALL] failed to open mp3")
			return nil, ErrCallMediaPath
		}
		return source, nil
	default:
		source, err := meowcaller.WAVFile(path)
		if err != nil {
			logrus.WithError(err).WithField("path", path).Warn("[CALL] failed to open wav")
			return nil, ErrCallMediaPath
		}
		return source, nil
	}
}
