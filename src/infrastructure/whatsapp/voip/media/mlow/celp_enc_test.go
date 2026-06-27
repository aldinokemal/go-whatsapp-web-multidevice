package mlow

import (
	"math"
	"testing"
)

func celpSineVec(n int, f, a float32) []float32 {
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = float32(math.Sin(float64(float32(i)*f))) * a
	}
	return v
}

// TestCelpEncodeUnvoicedRuns: unvoiced subframe (lags[1]<=0) → acb_idx[MAIN]==-1,
// exc_lpc length == fcb_subfrlen, n_pulses>=0. Mirrors encode_unvoiced_runs.
func TestCelpEncodeUnvoicedRuns(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L2210-L2239
	percRespLen, fcbSubfrlen := 32, 80
	enc := NewCelpEncoder(false, percRespLen, fcbSubfrlen, 4)
	resLpc := celpSineVec(fcbSubfrlen, 0.3, 0.1)
	var predcoef [17]float32
	predcoef[0] = 1.0
	predcoef[1] = -0.5
	percWghtResp := make([]float32, percRespLen)
	percWghtResp[0] = 1.0
	lags := []float32{0, 0, 0}
	surv := make([]int16, smplMaxPulsesPerSf)
	for i := range surv {
		surv[i] = 1
	}
	out := enc.EncodeSubframe(resLpc, &predcoef, percWghtResp, lags, [2]float32{1.0, 1.0}, [2]int16{8, 8}, surv)
	if out.AcbIdx[smplCelpIdxMain] != -1 {
		t.Errorf("unvoiced acb_idx[MAIN]=%d want -1", out.AcbIdx[smplCelpIdxMain])
	}
	if len(out.ExcLpc) != fcbSubfrlen {
		t.Errorf("exc_lpc len %d want %d", len(out.ExcLpc), fcbSubfrlen)
	}
	if out.NPulses[smplCelpIdxMain] < 0 {
		t.Errorf("n_pulses[MAIN]=%d", out.NPulses[smplCelpIdxMain])
	}
}

// TestCelpEncodeVoicedRuns: voiced (integer lag 60) → acb_idx[MAIN]>=0. Mirrors encode_voiced_runs.
func TestCelpEncodeVoicedRuns(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L2241-L2274
	percRespLen, fcbSubfrlen := 32, 80
	enc := NewCelpEncoder(false, percRespLen, fcbSubfrlen, 4)
	for i := range enc.acbState {
		enc.acbState[i] = float32(math.Sin(float64(float32(i) * 0.2)))
	}
	resLpc := celpSineVec(fcbSubfrlen, 0.25, 0.2)
	var predcoef [17]float32
	predcoef[0] = 1.0
	predcoef[1] = -0.4
	percWghtResp := make([]float32, percRespLen)
	percWghtResp[0] = 1.0
	lags := []float32{60, 60, 60}
	surv := make([]int16, smplMaxPulsesPerSf)
	for i := range surv {
		surv[i] = 2
	}
	out := enc.EncodeSubframe(resLpc, &predcoef, percWghtResp, lags, [2]float32{1.0, 1.0}, [2]int16{6, 6}, surv)
	if out.AcbIdx[smplCelpIdxMain] < 0 {
		t.Errorf("voiced acb_idx[MAIN]=%d want >=0", out.AcbIdx[smplCelpIdxMain])
	}
	if len(out.ExcLpc) != fcbSubfrlen {
		t.Errorf("exc_lpc len %d want %d", len(out.ExcLpc), fcbSubfrlen)
	}
}

// TestCelpEncodeVoicedFractionalGreedy: high-rate + surv[max-2]==1 → greedy path;
// fractional lag exercises the interpolation branch. Mirrors encode_voiced_fractional_lag_greedy_runs.
func TestCelpEncodeVoicedFractionalGreedy(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L2276-L2308
	percRespLen, fcbSubfrlen := 32, 80
	enc := NewCelpEncoder(false, percRespLen, fcbSubfrlen, 4)
	for i := range enc.acbState {
		enc.acbState[i] = float32(math.Sin(float64(float32(i) * 0.17)))
	}
	resLpc := celpSineVec(fcbSubfrlen, 0.25, 0.2)
	var predcoef [17]float32
	predcoef[0] = 1.0
	predcoef[1] = -0.4
	percWghtResp := make([]float32, percRespLen)
	percWghtResp[0] = 1.0
	lags := []float32{55.5, 55.5, 55.5}
	surv := make([]int16, smplMaxPulsesPerSf)
	for i := range surv {
		surv[i] = 1
	}
	out := enc.EncodeSubframe(resLpc, &predcoef, percWghtResp, lags, [2]float32{1.0, 1.0}, [2]int16{4, 4}, surv)
	if out.AcbIdx[smplCelpIdxMain] < 0 {
		t.Errorf("voiced-frac acb_idx[MAIN]=%d want >=0", out.AcbIdx[smplCelpIdxMain])
	}
	if len(out.ExcLpc) != fcbSubfrlen {
		t.Errorf("exc_lpc len %d want %d", len(out.ExcLpc), fcbSubfrlen)
	}
}
