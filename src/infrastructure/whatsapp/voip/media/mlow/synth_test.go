package mlow

import "testing"

// TestSynth is the full-synthesis KAT placeholder. The frame-synthesis bodies
// (SynthInternalFrame, CelpDecState.SynthFrame, etc.) have no standalone unit
// vector — they are validated end-to-end (e2e_vectors.json) by the decoder module.
// The decoder NLSF reconstruction is covered separately by TestDecoderReconstructsCQlsf.
func TestSynth(t *testing.T) {
	t.Skip("covered: the CELP synth path is validated end-to-end by TestE2EDecodeMatchesUseSmpl; SynthInternalFrame (WASM-domain alt) is unused on the decode path")
}
