package mlow

import (
	"encoding/binary"
	"os"
	"testing"
)

// TestVadGroundTruth validates the SILK VAD fixed-point port against the C enc_dump
// (smpl_VAD_GetSA_Q8_c) on mic_clip.raw: per-frame speech-activity probability and
// the packet coded_as_active_voice flag must match. Mirrors vad_matches_c_ground_truth.
func TestVadGroundTruth(t *testing.T) {
	raw, err := os.ReadFile("testdata/mic_clip.raw")
	if err != nil {
		t.Fatalf("read mic_clip.raw: %v", err)
	}
	samples := make([]int16, len(raw)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	var gt []struct {
		Pkt   int     `json:"pkt"`
		Frame int     `json:"frame"`
		Spact float32 `json:"spact"`
		Cav   int     `json:"cav"`
	}
	loadJSON(t, "vad_ground_truth.json", &gt)

	type res struct {
		spact float32
		cav   int
	}
	var results []res
	vad := NewSmplVadState()
	for off := 0; off+960 <= len(samples); off += 960 {
		r := vad.ProcessPacket(samples[off:off+960], 320)
		cav := 0
		if r.CodedAsActiveVoice {
			cav = 1
		}
		for f := 0; f < 3; f++ {
			results = append(results, res{r.VadResults[f], cav})
		}
	}

	for _, rec := range gt {
		idx := rec.Pkt*3 + rec.Frame
		got := results[idx]
		if d := got.spact - rec.Spact; d > 1e-4 || d < -1e-4 {
			t.Errorf("pkt %d frame %d: spact %.6f != C %.6f", rec.Pkt, rec.Frame, got.spact, rec.Spact)
		}
		if got.cav != rec.Cav {
			t.Errorf("pkt %d frame %d: cav %d != C %d", rec.Pkt, rec.Frame, got.cav, rec.Cav)
		}
	}
}
