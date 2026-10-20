package rest

import (
	"context"
	"encoding/binary"
	"math"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/contrib/v3/websocket"
)

// AudioStream bridges a live call to the browser. Binary frames carry s16le
// 16 kHz mono PCM in both directions: browser microphone audio is played to the
// peer, and the peer's audio is written back to the socket.
func (controller *Call) AudioStream(conn *websocket.Conn) {
	defer func() { _ = conn.Close() }()

	ctx := whatsapp.ContextWithDevice(context.Background(), deviceFromConn(conn))
	mic := make(chan []byte, 32)
	defer close(mic)

	var writeMu sync.Mutex
	write := func(messageType int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(messageType, payload)
	}

	onPeerAudio := func(frame []float32) {
		_ = write(websocket.BinaryMessage, pcmToS16LE(frame))
	}

	callID := conn.Query("call_id")
	if err := controller.Service.AttachAudio(ctx, callID, mic, onPeerAudio); err != nil {
		_ = write(websocket.TextMessage, []byte(err.Error()))
		return
	}
	_ = controller.Service.WatchCallEnd(ctx, callID, func() {
		_ = write(websocket.TextMessage, []byte("call ended"))
		_ = conn.Close()
	})

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		select {
		case mic <- payload:
		default:
			// Audio is realtime: drop frames instead of blocking the socket
			// when the call's player falls behind.
		}
	}
}

func deviceFromConn(conn *websocket.Conn) *whatsapp.DeviceInstance {
	if conn == nil {
		return nil
	}
	device, _ := conn.Locals("device").(*whatsapp.DeviceInstance)
	return device
}

// pcmToS16LE converts meowcaller's float32 frames to the wire format the browser
// worklet consumes. Samples are clamped so loud input cannot wrap around.
func pcmToS16LE(frame []float32) []byte {
	out := make([]byte, len(frame)*2)
	for i, sample := range frame {
		clamped := math.Max(-1, math.Min(1, float64(sample)))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(clamped*math.MaxInt16)))
	}
	return out
}
