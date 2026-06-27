package media

import (
	"math"
	"testing"
)

// These tests cover the adapter's contract (the Codec port), not the numerical
// fidelity of the MLow codec itself — that is covered by the vendored mlow
// package's own reference-vector tests.
func TestMLowCodecAdapterRoundtrip(t *testing.T) {
	codec, err := NewMLowCodec(DefaultCodecOptions)
	if err != nil {
		t.Fatalf("NewMLowCodec: %v", err)
	}
	defer codec.Close()

	if codec.FrameSize() != 960 || codec.SampleRate() != 16000 {
		t.Fatalf("unexpected frame=%d rate=%d", codec.FrameSize(), codec.SampleRate())
	}

	frame := make([]float32, 960)
	for i := range frame {
		frame[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}

	encoded, err := codec.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded frame is empty")
	}

	decoded, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded) != 960 {
		t.Fatalf("decode returned %d samples, want 960", len(decoded))
	}
}

func TestMLowCodecDecodeNilIsPLCSilence(t *testing.T) {
	codec, _ := NewMLowCodec(DefaultCodecOptions)
	defer codec.Close()

	plc, err := codec.Decode(nil)
	if err != nil {
		t.Fatalf("Decode(nil): %v", err)
	}
	if len(plc) != 960 {
		t.Fatalf("PLC returned %d samples, want 960", len(plc))
	}
}

func TestMLowCodecEncodeEmptyIsNoop(t *testing.T) {
	codec, _ := NewMLowCodec(DefaultCodecOptions)
	defer codec.Close()

	out, err := codec.Encode(nil)
	if err != nil {
		t.Fatalf("Encode(nil): %v", err)
	}
	if out != nil {
		t.Fatalf("Encode(nil) should return nil, got %d bytes", len(out))
	}
}
