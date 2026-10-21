package rest

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/stretchr/testify/require"
)

// A nil conn stands in for the released pooled connection: touching it would
// panic, so surviving these calls proves the completion flag guards it.
func TestAudioStreamConnIgnoresCallbacksAfterFinish(t *testing.T) {
	stream := &audioStreamConn{}
	stream.Finish()

	require.ErrorIs(t, stream.Write(websocket.BinaryMessage, []byte("frame")), errAudioStreamClosed)
	require.NotPanics(t, stream.Close)
}

func TestPCMToS16LEClamp(t *testing.T) {
	out := pcmToS16LE([]float32{0, 0.5, -0.5, 2, -2})
	require.Len(t, out, 10)

	read := func(i int) int16 { return int16(binary.LittleEndian.Uint16(out[i*2:])) }
	require.Equal(t, int16(0), read(0))
	require.Equal(t, int16(16383), read(1))
	require.Equal(t, int16(-16383), read(2))
	require.Equal(t, int16(math.MaxInt16), read(3), "positive overshoot must clamp, not wrap")
	require.Equal(t, int16(-math.MaxInt16), read(4), "negative overshoot must clamp, not wrap")
}
