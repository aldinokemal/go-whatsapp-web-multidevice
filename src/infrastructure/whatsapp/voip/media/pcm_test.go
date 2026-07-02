package media

import (
	"math"
	"testing"
)

func TestPCMRoundtrip(t *testing.T) {
	in := []float32{0, 0.5, -0.5, 0.25, -0.999}
	got := PCMInt16LEToFloat32(PCMFloat32ToInt16LE(in))
	if len(got) != len(in) {
		t.Fatalf("length mismatch: got %d want %d", len(got), len(in))
	}
	for i := range in {
		// One quantization step plus the 32767/32768 encode/decode scale skew.
		if math.Abs(float64(got[i]-in[i])) > 1e-4 {
			t.Errorf("sample %d: got %f want ~%f", i, got[i], in[i])
		}
	}
}

func TestPCMClampsAndSanitizes(t *testing.T) {
	b := PCMFloat32ToInt16LE([]float32{2.0, -2.0, float32(math.NaN())})
	got := PCMInt16LEToFloat32(b)
	if got[0] < 0.999 { // +full scale
		t.Errorf("clip high: got %f", got[0])
	}
	if got[1] > -0.999 { // -full scale
		t.Errorf("clip low: got %f", got[1])
	}
	if got[2] != 0 { // NaN -> silence
		t.Errorf("NaN should map to 0, got %f", got[2])
	}
}

func TestPCMInt16LEToFloat32IgnoresOddTrailingByte(t *testing.T) {
	// 5 bytes => 2 whole samples, trailing byte dropped.
	got := PCMInt16LEToFloat32([]byte{0x00, 0x40, 0x00, 0xC0, 0x7F})
	if len(got) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(got))
	}
}
