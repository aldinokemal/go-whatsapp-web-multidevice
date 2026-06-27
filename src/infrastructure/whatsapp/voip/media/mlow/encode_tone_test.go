package mlow

import (
	"math"
	"testing"
)

func toneCorr(a, b []float32) float64 {
	var sxy, sxx, syy float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		sxy += x * y
		sxx += x * x
		syy += y * y
	}
	if sxx < 1e-12 || syy < 1e-12 {
		return 0.0
	}
	return sxy / math.Sqrt(sxx*syy)
}

// TestEncodeRoundTripsATone is the encoder's end-to-end validation: encode a 550 Hz
// tone and decode it back through the byte-exact decoder; the reconstruction must
// track the input waveform shape (correlation > 0.5). Proves the analysis →
// entropy-encode → decode chain produces a frame that reconstructs the input audio.
// Mirrors encode_round_trips_a_tone.
func TestEncodeRoundTripsATone(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L482-L509
	enc := NewMlowEncoder()
	dec := NewMlowDecoder()
	var best float64
	for f := 0; f < 8; f++ {
		pcm := make([]float32, 960)
		for i := 0; i < 960; i++ {
			tt := float64(f*960+i) / 16000.0
			pcm[i] = float32(0.5 * math.Sin(2.0*math.Pi*550.0*tt))
		}
		frame, err := enc.Encode(pcm)
		if err != nil {
			t.Fatalf("frame %d: encode: %v", f, err)
		}
		if len(frame) == 0 || frame[0] != 0x50 {
			t.Fatalf("frame %d: expected active TOC 0x50, got %x", f, frame)
		}
		out := dec.Decode(frame)
		const harmDelay = 48
		c := toneCorr(pcm[:len(pcm)-harmDelay], out[harmDelay:])
		if c > best {
			best = c
		}
	}
	t.Logf("encode→decode round-trip best correlation: %.4f", best)
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/543302e762ef36913b3e2fdf7f84510c43265272/wacore/src/voip/mlow/encode.rs#L505-L509 (upstream tightened tone-roundtrip threshold)
	if best <= 0.7 {
		t.Errorf("encode→decode round-trip correlation too low: %.4f (want >0.7)", best)
	}
}
