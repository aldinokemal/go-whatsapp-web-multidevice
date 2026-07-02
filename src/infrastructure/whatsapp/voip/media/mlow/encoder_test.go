package mlow

import (
	"math"
	"testing"
)

// TestSignalModeGroundTruth feeds the C encoder's exact per-frame
// pitchcorr/avg_lag/harm/lags/F2/sp_act_prob (in stream order, threading one
// VuvMode) and requires our voicing_strength + voiced decision to match the C
// smpl_get_signal_mode output. harm_strength_at is checked on frames where the
// pitch search ran (avg_lag > 33). Mirrors signal_mode_matches_c_ground_truth.
func TestSignalModeGroundTruth(t *testing.T) {
	var recs []struct {
		Frame     int       `json:"frame"`
		Pitchcorr float32   `json:"pitchcorr"`
		AvgLag    float32   `json:"avg_lag"`
		Harm      float32   `json:"harm"`
		SpActProb float32   `json:"sp_act_prob"`
		Vstr      float32   `json:"vstr"`
		Voiced    int       `json:"voiced"`
		Lags      []float32 `json:"lags"`
		F2        []float32 `json:"F2"`
	}
	loadJSON(t, "sigmode_ground_truth.json", &recs)
	if len(recs) < 12 {
		t.Fatalf("want >= 12 records, got %d", len(recs))
	}

	var vuv VuvMode
	var maxErr, maxHarmErr float32
	for _, rec := range recs {
		if len(rec.F2) != SmplFLen {
			t.Fatalf("frame %d: F2 len %d != %d", rec.Frame, len(rec.F2), SmplFLen)
		}
		var f2 [SmplFLen]float32
		copy(f2[:], rec.F2)

		// On inactive frames the C smpl_pitch early-returns (lag clamped to the
		// 32-sample floor) and never computes harmonicity, so only validate
		// harm_strength_at where the pitch search actually ran.
		if rec.AvgLag > 33.0 {
			f2w := BuildF2w(&f2)
			harmRs := HarmStrengthAt(rec.AvgLag, &f2w)
			if d := float32(math.Abs(float64(harmRs - rec.Harm))); d > maxHarmErr {
				maxHarmErr = d
			}
		}

		vstrRs := SmplGetSignalMode(rec.Pitchcorr, rec.Lags, rec.AvgLag, rec.Harm, &f2, rec.SpActProb, &vuv)
		if d := float32(math.Abs(float64(vstrRs - rec.Vstr))); d > maxErr {
			maxErr = d
		}
		if (vstrRs > 0.0) != (rec.Voiced != 0) {
			t.Errorf("frame %d: voiced flip vstr_rs=%g vstr_c=%g", rec.Frame, vstrRs, rec.Vstr)
		}
	}

	t.Logf("voicing_strength max_err=%g, harm_strength max_err=%g", maxErr, maxHarmErr)
	if maxErr >= 1e-4 {
		t.Errorf("voicing_strength diverges from C: max_err=%g", maxErr)
	}
	// harm_strength_at is exact per call, but the C reuses a survivor-loop cache
	// keyed by a quantized harm bin, so a fresh-cache recompute is close but not
	// bit-exact without the full survivor sequence; bound the residual.
	if maxHarmErr >= 0.05 {
		t.Errorf("harm_strength diverges beyond cache-aliasing tolerance: %g", maxHarmErr)
	}
}
