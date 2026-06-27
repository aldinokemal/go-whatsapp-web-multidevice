package mlow

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"testing"
)

// TestE2EDecodeMatchesUseSmpl is the audible milestone: decode the whole captured
// stream and compare against the libopus useSmpl reference PCM
// (ref_usesmpl_expected.raw). The synthesis is not bit-exact (PRNG noise +
// -ffast-math), so the bar is length match + Pearson correlation > 0.95 at lag 0
// (the harmonic postfilter emits the 48-sample group delay). Mirrors
// e2e_decode_matches_usesmpl.
func TestE2EDecodeMatchesUseSmpl(t *testing.T) {
	var frames []string
	loadJSON(t, "inbound_capture_frames.json", &frames)

	raw, err := os.ReadFile("testdata/ref_usesmpl_expected.raw")
	if err != nil {
		t.Fatalf("read ref_usesmpl_expected.raw: %v", err)
	}
	refp := make([]float64, len(raw)/2)
	for i := range refp {
		refp[i] = float64(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768.0
	}

	dec := NewMlowDecoder()
	var out []float64
	for _, hf := range frames {
		fb, err := hex.DecodeString(hf)
		if err != nil {
			t.Fatalf("bad hex frame: %v", err)
		}
		for _, v := range dec.Decode(fb) {
			out = append(out, float64(v))
		}
	}
	if len(out) != len(refp) {
		t.Fatalf("decode length %d != reference %d", len(out), len(refp))
	}

	n := len(refp)
	var mr, mo float64
	for i := 0; i < n; i++ {
		mr += refp[i]
		mo += out[i]
	}
	mr /= float64(n)
	mo /= float64(n)
	var sxy, sxx, syy float64
	for i := 0; i < n; i++ {
		dr := refp[i] - mr
		dz := out[i] - mo
		sxy += dr * dz
		sxx += dr * dr
		syy += dz * dz
	}
	denom := math.Sqrt(sxx * syy)
	if denom == 0 {
		t.Fatalf("correlation undefined: constant signal (sxx=%v syy=%v)", sxx, syy)
	}
	corr := sxy / denom
	t.Logf("e2e lag-0 correlation vs useSmpl reference: %.4f", corr)
	if corr <= 0.95 {
		t.Errorf("lag-0 corr %.4f <= 0.95 vs useSmpl reference", corr)
	}
}
