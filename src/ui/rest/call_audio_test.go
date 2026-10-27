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
	require.Equal(t, int16(16384), read(1))
	require.Equal(t, int16(-16384), read(2))
	require.Equal(t, int16(math.MaxInt16), read(3), "positive overshoot must clamp, not wrap")
	require.Equal(t, int16(-32768), read(4), "negative overshoot must clamp to the full negative range")
}

// TestPCMToS16LERoundTripsFullScale pins the symmetric int16 <-> float scale:
// meowcaller decodes with /32768, so encoding with the same range must be exact.
func TestPCMToS16LERoundTripsFullScale(t *testing.T) {
	samples := []int16{-32768, -16384, -1, 0, 1, 16384, 32767}
	frames := make([]float32, len(samples))
	for i, sample := range samples {
		frames[i] = float32(sample) / 32768.0
	}

	out := pcmToS16LE(frames)
	require.Len(t, out, len(samples)*2)

	for i, want := range samples {
		got := int16(binary.LittleEndian.Uint16(out[i*2:]))
		require.Equal(t, want, got, "sample %d must round-trip unchanged", want)
	}
}
