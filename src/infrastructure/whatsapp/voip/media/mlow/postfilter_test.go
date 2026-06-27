package mlow

import (
	"encoding/binary"
	"math"
	"os"
	"testing"
)

func pfRdF32(b []byte, o *int) float32 {
	v := math.Float32frombits(binary.LittleEndian.Uint32(b[*o:]))
	*o += 4
	return v
}

func pfRdI32(b []byte, o *int) int32 {
	v := int32(binary.LittleEndian.Uint32(b[*o:]))
	*o += 4
	return v
}

// TestHpPostfilter validates the post-LPC HP (pitch-harmonic) comb against the
// instrumented-C decoder (hp_postfilter_vectors.raw): each frame carries the C HpPst
// state, the 8 per-block lags, the pre-hp and post-hp signals. Seed, run, compare.
// The reference is built -ffast-math, so the near-unit-circle pitch comb can't be
// bit-reproduced; the bound is the i16 output LSB (1/32768) — inaudible/identical
// once written to 16-bit PCM. Mirrors hp_postfilter_matches_c.
func TestHpPostfilter(t *testing.T) {
	const frameLen = SmplIntfLen
	const i16LSB = float32(1.0 / 32768.0)
	data, err := os.ReadFile("testdata/hp_postfilter_vectors.raw")
	if err != nil {
		t.Fatalf("read hp_postfilter_vectors.raw: %v", err)
	}
	o := 0
	count := pfRdI32(data, &o)
	if count == 0 {
		t.Fatal("no hp_postfilter frames")
	}
	var worst float32
	for f := int32(0); f < count; f++ {
		pfRdI32(data, &o) // packet
		pfRdI32(data, &o) // frame
		var lags [8]float32
		for i := range lags {
			lags[i] = pfRdF32(data, &o)
		}
		lo1 := pfRdF32(data, &o)
		lo2 := pfRdF32(data, &o)
		var hp [4]float32
		for i := range hp {
			hp[i] = pfRdF32(data, &o)
		}
		lagOld := pfRdF32(data, &o)
		xOld := make([]float32, frameLen)
		for i := range xOld {
			xOld[i] = pfRdF32(data, &o)
		}
		var coefMa, coefAr [3]float32
		for i := range coefMa {
			coefMa[i] = pfRdF32(data, &o)
		}
		for i := range coefAr {
			coefAr[i] = pfRdF32(data, &o)
		}
		yPre := make([]float32, frameLen)
		for i := range yPre {
			yPre[i] = pfRdF32(data, &o)
		}
		yPost := make([]float32, frameLen)
		for i := range yPost {
			yPost[i] = pfRdF32(data, &o)
		}

		var lag float32
		if lags[0] > 0.0 {
			var sl, sll float32
			for _, l := range lags {
				sl += l
				sll += l * l
			}
			lag = sll / sl
		}

		st := &HpPostfilterState{
			stateLoEmph1: lo1, stateLoEmph2: lo2, stateHp: hp,
			lagOld: lagOld, xOld: xOld, coefMA: coefMa, coefAR: coefAr,
		}
		out := make([]float32, frameLen)
		SmplHpPostfilter(st, yPre, frameLen, lag, out)
		for i := 0; i < frameLen; i++ {
			if d := absF32(out[i] - yPost[i]); d > worst {
				worst = d
			}
		}
	}
	if worst >= i16LSB {
		t.Errorf("hp_postfilter diverges from C by %.2e (>= i16 LSB %.2e)", worst, i16LSB)
	}
}

// TestHarmPostfilter validates the per-packet harmonic postfilter against the
// instrumented-C decoder (harm_postfilter_vectors.raw), processing the active packet
// sequence in order (StateComb/state1/prevLag carry across packets). The reference
// is -ffast-math: steady-state matches within the i16 LSB; the only larger residual
// is the first 48 samples (TOT_POSTFILT_DELAY) of a silence-after-voiced packet
// (the comb's zero-input response), bounded by transitionTol. Mirrors harm_postfilter_matches_c.
func TestHarmPostfilter(t *testing.T) {
	const i16LSB = float32(1.0 / 32768.0)
	const transitionTol = float32(6.0e-4)
	const totPostfiltDelay = harmFBDelay + harmDelay // 48
	data, err := os.ReadFile("testdata/harm_postfilter_vectors.raw")
	if err != nil {
		t.Fatalf("read harm_postfilter_vectors.raw: %v", err)
	}
	o := 0
	count := pfRdI32(data, &o)
	if count == 0 {
		t.Fatal("no harm_postfilter packets")
	}
	st := NewHarmPostfilterState()
	var worst, worstSteady float32
	for p := int32(0); p < count; p++ {
		pfRdI32(data, &o) // packet
		plen := int(pfRdI32(data, &o))
		nlags := int(pfRdI32(data, &o))
		nbr := pfRdF32(data, &o)
		lags := make([]float32, nlags)
		for i := range lags {
			lags[i] = pfRdF32(data, &o)
		}
		inp := make([]float32, plen)
		for i := range inp {
			inp[i] = pfRdF32(data, &o)
		}
		cout := make([]float32, plen)
		for i := range cout {
			cout[i] = pfRdF32(data, &o)
		}

		transition := lags[0] == 0.0
		SmplHarmPostfilter(st, inp, plen, lags, nlags, nbr)
		for i := 0; i < plen; i++ {
			d := absF32(inp[i] - cout[i])
			if d > worst {
				worst = d
			}
			if !(transition && i < totPostfiltDelay) && d > worstSteady {
				worstSteady = d
			}
		}
	}
	if worstSteady >= i16LSB {
		t.Errorf("harm_postfilter steady-state diverges by %.2e (>= i16 LSB %.2e)", worstSteady, i16LSB)
	}
	if worst >= transitionTol {
		t.Errorf("harm_postfilter transition residual %.2e exceeds tol %.2e", worst, transitionTol)
	}
}
