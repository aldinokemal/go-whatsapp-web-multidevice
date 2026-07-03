package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func recvBroadcast(t *testing.T) websocket.BroadcastMessage {
	t.Helper()
	select {
	case msg := <-websocket.Broadcast:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket broadcast")
		return websocket.BroadcastMessage{}
	}
}

func TestHandlerPasskeyEvents(t *testing.T) {
	ctx := context.Background()
	instance := NewDeviceInstance("test-passkey", nil, nil)

	// PairPasskeyRequest stores the challenge and broadcasts PASSKEY_REQUEST.
	publicKey := &types.WebAuthnPublicKey{RelyingPartID: "whatsapp.com"}
	go handler(ctx, instance, &events.PairPasskeyRequest{PublicKey: publicKey})
	if msg := recvBroadcast(t); msg.Code != "PASSKEY_REQUEST" {
		t.Errorf("broadcast code = %s, want PASSKEY_REQUEST", msg.Code)
	}
	challenge, code, _ := instance.PasskeyState()
	if challenge != publicKey || code != "" {
		t.Errorf("PasskeyState() after request = (%v, %q), want stored challenge and empty code", challenge, code)
	}

	// PairPasskeyConfirmation stores the code and broadcasts PASSKEY_CONFIRMATION.
	go handler(ctx, instance, &events.PairPasskeyConfirmation{Code: "ABCD-EFGH", SkipHandoffUX: true})
	if msg := recvBroadcast(t); msg.Code != "PASSKEY_CONFIRMATION" {
		t.Errorf("broadcast code = %s, want PASSKEY_CONFIRMATION", msg.Code)
	}
	challenge, code, skip := instance.PasskeyState()
	if challenge != nil || code != "ABCD-EFGH" || !skip {
		t.Errorf("PasskeyState() after confirmation = (%v, %q, %t), want (nil, ABCD-EFGH, true)", challenge, code, skip)
	}

	// PairPasskeyError clears the state and broadcasts PASSKEY_ERROR.
	go handler(ctx, instance, &events.PairPasskeyError{Error: errors.New("boom")})
	if msg := recvBroadcast(t); msg.Code != "PASSKEY_ERROR" {
		t.Errorf("broadcast code = %s, want PASSKEY_ERROR", msg.Code)
	}
	challenge, code, skip = instance.PasskeyState()
	if challenge != nil || code != "" || skip {
		t.Errorf("PasskeyState() after error = (%v, %q, %t), want cleared", challenge, code, skip)
	}

	// PairSuccess clears any leftover passkey state alongside the LOGIN_SUCCESS broadcast.
	instance.SetPasskeyChallenge(publicKey)
	go handler(ctx, instance, &events.PairSuccess{})
	if msg := recvBroadcast(t); msg.Code != "LOGIN_SUCCESS" {
		t.Errorf("broadcast code = %s, want LOGIN_SUCCESS", msg.Code)
	}
	challenge, code, skip = instance.PasskeyState()
	if challenge != nil || code != "" || skip {
		t.Errorf("PasskeyState() after pair success = (%v, %q, %t), want cleared", challenge, code, skip)
	}
}
