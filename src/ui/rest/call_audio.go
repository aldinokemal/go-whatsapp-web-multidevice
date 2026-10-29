package rest

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/contrib/v3/websocket"
)

// errAudioStreamClosed reports that the audio stream finished and its pooled
// websocket connection can no longer be used.
var errAudioStreamClosed = errors.New("audio stream closed")

// audioStreamConn guards the pooled websocket connection used by AudioStream.
// The connection returns to the pool as soon as the handler returns, so
// callbacks that outlive the handler must not touch it. The mutex guards the
// completion flag together with every write and close, so a frame is never
// written to a released connection.
type audioStreamConn struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	finished bool
}

// Write sends a frame while the stream still owns the connection.
func (stream *audioStreamConn) Write(messageType int, payload []byte) error {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.finished {
		return errAudioStreamClosed
	}

	return stream.conn.WriteMessage(messageType, payload)
}

// Close closes the connection while the stream still owns it. It is a no-op
// once the stream finished, so a late callback cannot close a recycled conn.
func (stream *audioStreamConn) Close() {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.finished {
		return
	}

	stream.closeLocked()
}

// Finish marks the stream as finished and closes the connection. Later writes
// and closes are ignored.
func (stream *audioStreamConn) Finish() {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.finished = true
	stream.closeLocked()
}

// closeLocked closes the connection unless it was already released.
func (stream *audioStreamConn) closeLocked() {
	if stream.conn == nil {
		return
	}

	_ = stream.conn.Close()
}

// AudioStream bridges a live call to the browser. Binary frames carry s16le
// 16 kHz mono PCM in both directions: browser microphone audio is played to the
// peer, and the peer's audio is written back to the socket.
func (controller *Call) AudioStream(conn *websocket.Conn) {
	stream := &audioStreamConn{conn: conn}
	defer stream.Finish()

	ctx := whatsapp.ContextWithDevice(context.Background(), deviceFromConn(conn))
	mic := make(chan []byte, 32)
	defer close(mic)

	onPeerAudio := func(frame []float32) {
		_ = stream.Write(websocket.BinaryMessage, pcmToS16LE(frame))
	}

	callID := conn.Query("call_id")
	if err := controller.Service.AttachAudio(ctx, callID, mic, onPeerAudio); err != nil {
		_ = stream.Write(websocket.TextMessage, []byte(err.Error()))
		return
	}
	_ = controller.Service.WatchCallEnd(ctx, callID, func() {
		_ = stream.Write(websocket.TextMessage, []byte("call ended"))
		stream.Close()
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
// worklet consumes. Samples are clamped so loud input cannot wrap around, and the
// full negative range is used so -1.0 encodes as -32768 and a library frame
// round-trips unchanged.
func pcmToS16LE(frame []float32) []byte {
	out := make([]byte, len(frame)*2)
	for i, sample := range frame {
		clamped := math.Max(-1, math.Min(1, float64(sample)))
		value := int(math.Round(clamped * 32768))
		if value > math.MaxInt16 {
			value = math.MaxInt16
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(value)))
	}
	return out
}
