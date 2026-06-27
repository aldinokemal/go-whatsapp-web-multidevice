package mlow

import (
	"math"
	"testing"
)

// TestPercFFTRoundtrip: forward→inverse real FFT recovers n*x within float tolerance.
func TestPercFFTRoundtrip(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_perc.rs#L887-L910
	n := percwNfft
	x := make([]float32, n)
	var s uint32 = 12345
	for i := range x {
		s = s*196314165 + 907633515
		x[i] = float32(s>>9)/float32(uint32(1)<<23) - 1.0
	}
	f := make([]float32, n)
	rfftForwardOrdered(x, f)
	back := make([]float32, n)
	rfftBackwardOrdered(f, back)
	for i := 0; i < n; i++ {
		expected := x[i] * float32(n)
		if d := math.Abs(float64(back[i] - expected)); d >= 1e-1*(1.0+math.Abs(float64(expected))) {
			t.Fatalf("idx %d: got %v want %v", i, back[i], expected)
		}
	}
}

// TestPercModelSmoke: zero input → ~0 autocorr; DC step → R[0]>0; ac2a → A[0]==1.
func TestPercModelSmoke(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_perc.rs#L937-L965
	st := NewPercModelState()
	zero := make([]float32, 320)
	r := SmplPercModel(st, zero, 320, 20, 0, smplMaxLResp)
	if len(r) != smplMaxLResp {
		t.Fatalf("len(r)=%d", len(r))
	}
	for _, v := range r {
		if math.Abs(float64(v)) >= 1e-6 {
			t.Fatalf("zero input must give ~0 autocorr, got %v", v)
		}
	}
	st2 := NewPercModelState()
	dc := make([]float32, 320)
	for i := range dc {
		dc[i] = 1.0
	}
	r2 := SmplPercModel(st2, dc, 320, 20, 0, smplMaxLResp)
	if r2[0] <= 0.0 {
		t.Fatalf("DC input must give positive R[0], got %v", r2[0])
	}
	a := SmplPercAc2a(r2, smplMaxLResp, SmplPercEmphV[0], smplPercRespLen, SmplPercReg)
	if len(a) != smplPercRespLen || a[0] != 1.0 {
		t.Fatalf("ac2a: len=%d a[0]=%v", len(a), a[0])
	}
}

// TestBitrateControllerActiveUnvoicedBudget: for the active MLow config (20 kbps,
// 60 ms, complexity 8, high-rate, unvoiced) the per-subframe pulse budget must
// equal the C reference (23/subframe). Mirrors the reference regression test.
func TestBitrateControllerActiveUnvoicedBudget(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_perc.rs#L991-L1012
	bc := NewBitrateController()
	enc := &BitrateControllerInputs{
		InternalSampleRate: 16000, PayloadSizeMs: 60, FecBitRate: 0, MainBitRate: 20000,
		Complexity: 8, UseFecRateCompensation: 0, UseDtx: 0, SubFrameImportanceFactor: 1.0,
	}
	mp, _ := bc.control(enc, 0, 1, 0.9961, 0.2, -0.18, 0, 3.0e-5, 5.0e-5, 0, 320, 80)
	if mp[1] != 23 {
		t.Fatalf("active-unvoiced max_pulses must be 23/subframe, got %d", mp[1])
	}
}
