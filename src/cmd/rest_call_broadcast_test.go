package cmd

import (
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/stretchr/testify/require"
)

func drainCallBroadcast() {
	for {
		select {
		case <-websocket.Broadcast:
		default:
			return
		}
	}
}

func TestBroadcastIncomingCallUIDropsWithoutReceiver(t *testing.T) {
	drainCallBroadcast()
	t.Cleanup(drainCallBroadcast)

	start := time.Now()
	sent := broadcastIncomingCallUI(websocket.BroadcastMessage{Code: "CALL_INCOMING"}, 20*time.Millisecond)

	require.False(t, sent)
	require.Less(t, time.Since(start), time.Second, "send must be bounded")
}

func TestBroadcastIncomingCallUIDeliversWhenHubListens(t *testing.T) {
	drainCallBroadcast()
	t.Cleanup(drainCallBroadcast)

	received := make(chan websocket.BroadcastMessage, 1)
	go func() { received <- <-websocket.Broadcast }()

	notification := websocket.BroadcastMessage{
		Code:    "CALL_ENDED",
		Message: "Incoming call",
		Result:  "payload",
	}
	require.True(t, broadcastIncomingCallUI(notification, time.Second))
	require.Equal(t, notification, <-received)
}
