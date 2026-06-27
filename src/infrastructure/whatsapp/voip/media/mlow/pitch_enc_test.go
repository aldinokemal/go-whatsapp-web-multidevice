package mlow

import (
	"math"
	"testing"
)

// TestPitchEstimatorGroundTruth feeds the C encoder's exact per-frame ltp_buf + F2
// into the ported estimator (threading the cross-frame predictor, seeding
// prev_lagblk/prev_lagidx per frame from the dump) and requires convergence to the
// C smpl_pitch: exact laginds + blockseg_idx, pitchcorr/avg_lag within 1e-3, harm
// within the cache-aliasing tol. Mirrors pitch_estimator_matches_c_ground_truth.
func TestPitchEstimatorGroundTruth(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pitch_enc.rs#L1246-L1326
	var recs []struct {
		Frame       int       `json:"frame"`
		Cav         int       `json:"cav"`
		PrevLagblk  int32     `json:"prev_lagblk"`
		PrevLagidx  int32     `json:"prev_lagidx"`
		LtpBuf      []float32 `json:"ltp_buf"`
		F2          []float32 `json:"F2"`
		Pitchcorr   float32   `json:"pitchcorr"`
		AvgLag      float32   `json:"avg_lag"`
		Harm        float32   `json:"harm"`
		BlocksegIdx int       `json:"blockseg_idx"`
		Laginds     []int32   `json:"laginds"`
	}
	loadJSON(t, "pitchio_ground_truth.json", &recs)
	if len(recs) < 30 {
		t.Fatalf("expected >=30 records, got %d", len(recs))
	}

	var st PitchEstState
	var maxPcErr, maxAvgErr, maxHarmErr float32
	lagMism, bsxMism, checked := 0, 0, 0
	for _, rec := range recs {
		cav := rec.Cav != 0
		st.PrevLagblk = rec.PrevLagblk
		st.PrevLagidx = rec.PrevLagidx
		if len(rec.LtpBuf) != MaxLTPBufLen {
			t.Fatalf("frame %d: ltp_buf len %d != %d", rec.Frame, len(rec.LtpBuf), MaxLTPBufLen)
		}
		var f2 [SmplFLen]float32
		copy(f2[:], rec.F2)

		res := SmplPitch(&st, rec.LtpBuf, &f2, cav)

		if cav {
			if d := absf32(res.Pitchcorr - rec.Pitchcorr); d > maxPcErr {
				maxPcErr = d
			}
			if d := absf32(res.AvgLag - rec.AvgLag); d > maxAvgErr {
				maxAvgErr = d
			}
			if d := absf32(res.HarmStrength - rec.Harm); d > maxHarmErr {
				maxHarmErr = d
			}
			for sf := 0; sf < NumSubframes; sf++ {
				if res.Laginds[sf] != rec.Laginds[sf] {
					lagMism++
					break
				}
			}
			if res.BlocksegIdx != rec.BlocksegIdx {
				bsxMism++
			}
			checked++
		}
	}
	t.Logf("checked=%d maxPcErr=%g maxAvgErr=%g maxHarmErr=%g lagMism=%d bsxMism=%d",
		checked, maxPcErr, maxAvgErr, maxHarmErr, lagMism, bsxMism)
	if checked < 20 {
		t.Fatalf("too few active frames checked: %d", checked)
	}
	if maxPcErr >= 1e-3 {
		t.Errorf("pitchcorr diverges: max_err=%g", maxPcErr)
	}
	if maxAvgErr >= 1e-3 {
		t.Errorf("avg_lag diverges: max_err=%g", maxAvgErr)
	}
	if lagMism != 0 {
		t.Errorf("per-subframe laginds diverge on %d frames", lagMism)
	}
	if bsxMism != 0 {
		t.Errorf("blockseg_idx diverges on %d frames", bsxMism)
	}
	if maxHarmErr >= 0.05 {
		t.Errorf("harm_strength diverges beyond cache-aliasing tol: %g", maxHarmErr)
	}
}

func absf32(x float32) float32 { return float32(math.Abs(float64(x))) }
