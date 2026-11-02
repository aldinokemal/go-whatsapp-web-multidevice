package whatsapp

import (
	"encoding/binary"
	"io"
	"time"

	"github.com/purpshell/meowcaller"
)

// liveAudioFrameWait bounds how long ReadFrame waits for microphone bytes before
// emitting silence. meowcaller pulls one frame per ~60 ms tick, so a stalled
// browser must not stall the call's outbound media.
const liveAudioFrameWait = 60 * time.Millisecond

// channelAudioSource adapts s16le 16 kHz mono chunks (browser microphone audio)
// to meowcaller's float32 frame source. Closing the channel ends the stream.
type channelAudioSource struct {
	ch      <-chan []byte
	pending []byte
	closed  bool
}

func newChannelAudioSource(ch <-chan []byte) *channelAudioSource {
	return &channelAudioSource{ch: ch}
}

// ReadFrame returns one FrameSamples frame, padding with silence when the
// microphone is quiet, and io.EOF after the channel closes and its tail is used.
func (s *channelAudioSource) ReadFrame() ([]float32, error) {
	if s.closed && len(s.pending) == 0 {
		return nil, io.EOF
	}

	need := meowcaller.FrameSamples * 2
	waited := false
	for len(s.pending) < need && !s.closed {
		select {
		case chunk, ok := <-s.ch:
			if !ok {
				s.closed = true
				continue
			}
			s.pending = append(s.pending, chunk...)
		case <-time.After(liveAudioFrameWait):
			waited = true
		}
		if waited {
			break
		}
	}

	return s.takeFrame(), nil
}

func (s *channelAudioSource) takeFrame() []float32 {
	frame := make([]float32, meowcaller.FrameSamples)
	bytes := len(s.pending)
	if bytes > len(frame)*2 {
		bytes = len(frame) * 2
	}
	for i := 0; i*2+1 < bytes; i++ {
		sample := int16(binary.LittleEndian.Uint16(s.pending[i*2:]))
		// Division by the full negative range, matching meowcaller's own decode:
		// -32768 maps to -1.0 and the value round-trips without peak distortion.
		frame[i] = float32(sample) / 32768.0
	}
	s.pending = s.pending[bytes:]
	return frame
}

// Close ends the source even when the producer never closes the channel.
func (s *channelAudioSource) Close() error {
	s.closed = true
	return nil
}
