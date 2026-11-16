package rest

import (
	"encoding/binary"
	"io"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
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

func TestCallAudioRejectsForeignOrigin(t *testing.T) {
	prevBeta := config.WhatsappBetaCallEnabled
	config.WhatsappBetaCallEnabled = true
	t.Cleanup(func() { config.WhatsappBetaCallEnabled = prevBeta })

	prevOrigins := config.AppTrustedOrigins
	config.AppTrustedOrigins = []string{}
	t.Cleanup(func() { config.AppTrustedOrigins = prevOrigins })

	app := fiber.New()
	InitRestCall(app, nil)

	req := httptest.NewRequest(fiber.MethodGet, "http://gowa.example/call/audio?call_id=c&device_id=d", nil)
	req.Host = "gowa.example"
	req.Header.Set(fiber.HeaderOrigin, "https://evil.example")
	req.Header.Set(fiber.HeaderConnection, "Upgrade")
	req.Header.Set(fiber.HeaderUpgrade, "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusForbidden, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "CROSS_SITE_REQUEST_BLOCKED")
}

func TestValidAudioFrame(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{name: "one worklet frame", payload: make([]byte, 960*2), want: true},
		{name: "batched frames", payload: make([]byte, maxAudioFrameBytes), want: true},
		{name: "empty", payload: []byte{}, want: false},
		{name: "single byte", payload: []byte{0}, want: false},
		{name: "odd length", payload: make([]byte, 3), want: false},
		{name: "oversized", payload: make([]byte, maxAudioFrameBytes+2), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, validAudioFrame(test.payload))
		})
	}
}
