package call

import (
	"log/slog"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
)

// Incoming media path: once a relay connects for an accepted incoming call
// (state Connecting), onRelayConnected must drive it to Active. This is the
// final link that leaves the UI stuck on "Connecting" when it doesn't fire.
func TestIncomingCallReachesActiveOnRelayConnect(t *testing.T) {
	m := &CallManager{log: slog.Default()}

	call := NewIncomingCall("CID", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	if err := call.ApplyTransition(Transition{Type: TransitionLocalAccepted}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if call.StateData.State != core.CallStateConnecting {
		t.Fatalf("accepted incoming call should be Connecting, got %s", call.StateData.State)
	}
	m.currentCall = call

	// codec is nil, so the silence keepalive started inside is a no-op.
	m.onRelayConnected()

	if !call.IsActive() {
		t.Fatalf("incoming call should be Active after relay connect, got %s", call.StateData.State)
	}
}

// onRelayConnected must only promote a call that is actually Connecting; a late
// relay-connect on a ringing or ended call must not change its state.
func TestOnRelayConnectedOnlyPromotesConnecting(t *testing.T) {
	m := &CallManager{log: slog.Default()}

	ringing := NewIncomingCall("CID", "peer@lid", "creator@lid", "", core.CallMediaTypeAudio)
	m.currentCall = ringing
	m.onRelayConnected()
	if ringing.StateData.State != core.CallStateIncomingRinging {
		t.Fatalf("ringing call must stay IncomingRinging, got %s", ringing.StateData.State)
	}

	if m2 := (&CallManager{log: slog.Default()}); m2.currentCall == nil {
		// nil currentCall must not panic.
		m2.onRelayConnected()
	}
}
