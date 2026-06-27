package call

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
)

func TestCallStateMachine(t *testing.T) {
	c := NewOutgoingCall("CID", "peer@lid", "me@lid", core.CallMediaTypeAudio)
	if c.StateData.State != core.CallStateInitiating {
		t.Fatal("should start Initiating")
	}
	if err := c.ApplyTransition(Transition{Type: TransitionOfferSent}); err != nil {
		t.Fatal(err)
	}
	if c.StateData.State != core.CallStateRinging {
		t.Fatal("should be Ringing after offer_sent")
	}
	if err := c.ApplyTransition(Transition{Type: TransitionRemoteAccepted}); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyTransition(Transition{Type: TransitionMediaConnected}); err != nil {
		t.Fatal(err)
	}
	if !c.IsActive() {
		t.Fatal("should be Active")
	}

	if err := c.ApplyTransition(Transition{Type: TransitionOfferSent}); err == nil {
		t.Fatal("expected InvalidTransition")
	} else if _, ok := err.(*InvalidTransition); !ok {
		t.Fatalf("expected *InvalidTransition, got %T", err)
	}

	if err := c.ApplyTransition(Transition{Type: TransitionTerminated, Reason: core.EndCallReasonUserEnded}); err != nil {
		t.Fatal(err)
	}
	if !c.IsEnded() {
		t.Fatal("should be Ended")
	}
}
