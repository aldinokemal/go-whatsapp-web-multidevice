package mlow

import (
	"math"
	"sync"
)

// Decoder-side CELP synthesis in the codec's native float domain — a faithful port
// of the per-subframe loop in smpl_core_decoder.c (excitation → CELP/ACB decode →
// gen_noise → LPC synthesis) plus LSF interpolation and the FCB gain tables. Output
// is float in [-1, 1]. Reuses SmplNLSF2A (synth), the noise generator (noise.go),
// and the HP postfilter (postfilter.go).

const (
	celpLagSubfrLen      = 40
	celpLTPInterpolDelay = 8
	celpMaxPitchLag      = 320
	acbgM                = 2
	acbgN                = 16
	pitchSharpCoef       = float32(0.9881)
	fcbgVN               = 34
	uvGainIdxLen         = 90
	vGainMinDB           = float32(-100.0)
	vGainStepDB          = float32(3.0)
	uvGainMinDB          = float32(-90.0)
	uvGainStepDB         = float32(1.0)
)

// decAcbHighBoost: ACB high-boost endpoints (smpl_dec_acb_high_boost).
var decAcbHighBoost = [2]float32{0.35, 0.18}

// lsfInterpol4: LSF→LPC interpolation factors per subframe, [lsf_interpol_idx][sf].
var lsfInterpol4 = [2][4]float32{{0.55, 0.88, 1.0, 1.0}, {0.3, 0.65, 0.95, 1.0}}

// celpInterpolKernel: 16-tap symmetric LTP interpolation kernel.
var celpInterpolKernel = [2 * celpLTPInterpolDelay]float32{
	-6.3925986e-6, 0.00011064114, -0.0009153038, 0.00484772, -0.018698348, 0.05759091, -0.15997477, 0.6170455,
	0.61704546, -0.15997475, 0.057590906, -0.018698348, 0.00484772, -0.0009153038, 0.000110641144, -6.392598e-6,
}

// Per-subframe ACB-gain codebook (Q14), [acbgN*acbgM]. Mirrors smpl_celp's
// cb_acbgains_{hr,lr}_q14 (only these two small tables are needed on the decode path).
var cbAcbgainsHRQ14 = [acbgN * acbgM]int16{
	16039, 91, 0, 0, 4310, 4930, -1431, 2862, 2893, 0, 8009, 4075, 2754, 4223, 8367, 354,
	4640, 1254, -176, 2734, -1222, 5017, -476, 1506, 11351, 567, 1243, 0, 10601, 22, 14088, 108,
}
var cbAcbgainsLRQ14 = [acbgN * acbgM]int16{
	2812, 2484, 0, 0, -362, 2465, -337, 703, 3033, 1474, 13536, 220, -2630, 9226, 6032, 3499,
	-220, 441, 7661, 4243, 11521, 0, 1430, 779, 4495, 2724, 15535, 343, -779, 1559, 480, 481,
}

type fcbGainsT struct {
	uv [uvGainIdxLen + 1]float32
	v  [fcbgVN]float32
}

var (
	fcbGainsOnce sync.Once
	fcbGainsV    fcbGainsT
)

func fcbGains() *fcbGainsT {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L64-L77
	fcbGainsOnce.Do(func() {
		for ix := 0; ix <= uvGainIdxLen; ix++ {
			fcbGainsV.uv[ix] = float32(math.Pow(10, float64(0.05*(float32(ix)*uvGainStepDB+uvGainMinDB))))
		}
		for ix := 0; ix < fcbgVN; ix++ {
			fcbGainsV.v[ix] = float32(math.Pow(10, float64(0.05*(float32(ix)*vGainStepDB+vGainMinDB))))
		}
	})
	return &fcbGainsV
}

func celpDot(a, b []float32, l int) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L79-L86
	var r float32
	for i := 0; i < l; i++ {
		r += a[i] * b[i]
	}
	return r
}

// lpcInterpol: per-subframe interpolation of the LSF between prevLsf and lsf, then
// NLSF→A. Mutates prevLsf to the last interpolated LSF (carried across frames).
func lpcInterpol(lsf []float32, prevLsf *[SmplOrder]float32, interpol [4]float32, aOut *[SmplSubfrCount][SmplOrder + 1]float32, lsfsOut *[SmplSubfrCount][SmplOrder]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L126-L155
	if prevLsf[SmplOrder-1] == 0.0 {
		copy(prevLsf[:], lsf[:SmplOrder])
	}
	var ilsf [SmplOrder]float32
	prevFactor := float32(-1.0)
	for j := 0; j < SmplSubfrCount; j++ {
		if interpol[j] == prevFactor {
			aOut[j] = aOut[j-1]
		} else {
			if interpol[j] == 1.0 {
				copy(ilsf[:], lsf[:SmplOrder])
			} else {
				for k := 0; k < SmplOrder; k++ {
					ilsf[k] = prevLsf[k]*(1.0-interpol[j]) + lsf[k]*interpol[j]
				}
			}
			copy(aOut[j][:], SmplNLSF2A(ilsf[:]))
		}
		prevFactor = interpol[j]
		lsfsOut[j] = ilsf
	}
	copy(prevLsf[:], ilsf[:])
}

func acbDequant(lowRate bool, acbIdx int32, acbG *[acbgM]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L157-L168
	cb := &cbAcbgainsHRQ14
	if lowRate {
		cb = &cbAcbgainsLRQ14
	}
	const sc = 1.0 / float32(int32(1)<<14)
	for m := 0; m < acbgM; m++ {
		acbG[m] = float32(cb[int(acbIdx)*acbgM+m]) * sc
	}
}

// acbSynthesize: adjust_acbgains (high-boost) then 3-tap symmetric ACB synthesis.
func acbSynthesize(fcbSubfrlen int, acbBasis []float32, acbGIn *[acbgM]float32, highBoost float32, acb []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L170-L193
	acbG := *acbGIn
	if highBoost != 0.0 {
		f0 := acbG[0] + 2.0*acbG[1]
		f1 := acbG[0] - acbG[1]
		absF2new := minF32(absF32(f1)+highBoost, absF32(f0))
		f1 = f1 * (absF2new / (absF32(f1) + 1e-12))
		acbG[0] = (f0 + 2.0*f1) / 3.0
		acbG[1] = (f0 - f1) / 3.0
	}
	for i := 0; i < fcbSubfrlen; i++ {
		acb[i] = acbG[0] * acbBasis[i]
	}
	for i := 0; i < fcbSubfrlen; i++ {
		acb[i] += acbG[1] * acbBasis[fcbSubfrlen+i]
	}
}

func pitchSharp(x []float32, lag, l int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L195-L200
	for i := lag; i < l; i++ {
		x[i] += x[i-lag] * pitchSharpCoef
	}
}

// synLTPBasis: build the ACB basis from the excitation history; mutates state forward.
func synLTPBasis(lags []float32, nLags int, state []float32, stateLen int, acbBasis []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L202-L269
	p := stateLen - nLags*celpLagSubfrLen
	for subfr := 0; subfr < nLags; subfr++ {
		iLag := int(math.Floor(float64(lags[subfr])))
		if float32(iLag) == lags[subfr] {
			il := iLag
			for i := 0; i < celpLagSubfrLen; i++ {
				state[p+i] = state[p+i-il]
			}
			for i := 0; i < celpLagSubfrLen; i++ {
				acbBasis[subfr*celpLagSubfrLen+i] = state[p+i]
			}
			for i := 0; i < celpLagSubfrLen; i++ {
				acbBasis[(nLags+subfr)*celpLagSubfrLen+i] = state[p+i-il-1] + state[p+i-il+1]
			}
		} else {
			il := iLag
			baseFirst := p + (-1 - il - celpLTPInterpolDelay)
			first := celpDot(state[baseFirst:], celpInterpolKernel[:], 2*celpLTPInterpolDelay)
			srcBase := p + (-il - celpLTPInterpolDelay)
			for nn := 0; nn < celpLagSubfrLen; nn++ {
				var ret float32
				for i := 0; i < 8; i++ {
					s0 := state[srcBase+nn+i]
					s1 := state[srcBase+nn+15-i]
					ret += (s0 + s1) * celpInterpolKernel[i]
				}
				state[p+nn] = ret
			}
			baseLast := p + (celpLagSubfrLen - il - celpLTPInterpolDelay)
			last := celpDot(state[baseLast:], celpInterpolKernel[:], 2*celpLTPInterpolDelay)
			for i := 0; i < celpLagSubfrLen; i++ {
				acbBasis[subfr*celpLagSubfrLen+i] = state[p+i]
			}
			b1 := (nLags + subfr) * celpLagSubfrLen
			acbBasis[b1] = first + state[p+1]
			for i := 0; i < celpLagSubfrLen-2; i++ {
				acbBasis[b1+1+i] = state[p+i] + state[p+i+2]
			}
			iLast := celpLagSubfrLen - 1
			acbBasis[b1+iLast] = state[p+iLast-1] + last
		}
		p += celpLagSubfrLen
	}
}

// celpDecode: add the ACB (LTP) contribution into lpcRes (voiced), then push the
// subframe into the ACB state.
func celpDecode(acbState []float32, acbStateLen int, voiced bool, acbGainIdx int32, lags []float32, numLags, subfrlen int, lowRate bool, normalizedBitrate float32, lpcRes []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L271-L306
	if voiced {
		highBoost := decAcbHighBoost[0] + (decAcbHighBoost[1]-decAcbHighBoost[0])*normalizedBitrate
		iLag := int(lags[numLags-1])
		if lowRate {
			pitchSharp(lpcRes, iLag, subfrlen)
		}
		acbBasis := make([]float32, subfrlen*acbgM)
		acb := make([]float32, subfrlen)
		synLTPBasis(lags, numLags, acbState, acbStateLen, acbBasis)
		var acbGain [acbgM]float32
		acbDequant(lowRate, acbGainIdx, &acbGain)
		acbSynthesize(subfrlen, acbBasis, &acbGain, highBoost, acb)
		for i := 0; i < subfrlen; i++ {
			lpcRes[i] += acb[i]
		}
	}
	// Update ACB state: shift left by subfrlen, append this subframe's excitation.
	copy(acbState[0:], acbState[subfrlen:acbStateLen-subfrlen])
	copy(acbState[acbStateLen-2*subfrlen:acbStateLen-subfrlen], lpcRes[:subfrlen])
}

// filtAR16: y[n] = x[n] - sum_i a[16-i]*y[n-16+i]; ybuf holds a 16-sample history
// prefix at ybuf[base-16..base].
func filtAR16(x []float32, a *[SmplOrder + 1]float32, ybuf []float32, base, n int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L308-L319
	for nn := 0; nn < n; nn++ {
		res := x[nn]
		for i := 0; i < SmplOrder; i++ {
			res -= a[SmplOrder-i] * ybuf[base+nn-SmplOrder+i]
		}
		ybuf[base+nn] = res
	}
}

// CelpDecParams holds the per-subframe decoded params the synthesis consumes.
type CelpDecParams struct {
	Voiced       bool
	SfPulses     [SmplSubfrCount]int32
	FcbgIdx      [SmplSubfrCount]int32
	NrgresDbqQ14 [SmplSubfrCount]int32
	AcbgIdx      [SmplSubfrCount]int32
	BlockLags    [2 * SmplSubfrCount]float32 // per-40-block pitch lag (codec units), 0 for unvoiced
	TotalPulses  int32
}

// CelpDecState is the persistent decoder synthesis state (C float domain).
type CelpDecState struct {
	noise       NoiseGenerator
	acbState    []float32
	acbStateLen int
	lpcSynthMem [SmplOrder]float32
	lsfPrev     [SmplOrder]float32
	prevNrgres  float32
	hp          HpPostfilterState
	// traceExcPre captures the per-subframe pre-noise excitation into ExcPre (KAT only).
	traceExcPre bool
	ExcPre      []float32
}

// NewCelpDecState allocates a fresh CELP decoder state.
func NewCelpDecState() *CelpDecState {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L351-L366
	acbStateLen := SmplSubfrLen + 2*celpMaxPitchLag + celpLTPInterpolDelay
	return &CelpDecState{
		acbState:    make([]float32, acbStateLen),
		acbStateLen: acbStateLen,
		hp:          *NewHpPostfilterState(),
	}
}

// SynthFrame synthesizes one 20 ms internal frame (4 subframes) into 320 float
// samples in [-1, 1]. nlsf is the reconstructed order-16 NLSF; pulses are the signed
// FCB pulse magnitudes (320 positions); lowRate is the TOC bit.
func (s *CelpDecState) SynthFrame(nlsf []float32, lsfInterpolIdx int, pulses []int32, params *CelpDecParams, lowRate bool, frameLength16 int32, out []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celpdec.rs#L372-L488
	// Validation: the deterministic pre-noise excitation (FCB×gain + voiced ACB/LTP) is
	// covered bit-tight by TestExcPre (exc_pre_lags.json); the noise and HP-postfilter
	// stages it composes are each KAT-verified in their own modules. The full combined
	// PCM output is validated end-to-end by the decoder module (e2e_vectors.json).
	gains := fcbGains()
	var a [SmplSubfrCount][SmplOrder + 1]float32
	var lsfs [SmplSubfrCount][SmplOrder]float32
	idx := lsfInterpolIdx
	if idx > 1 {
		idx = 1
	}
	lpcInterpol(nlsf, &s.lsfPrev, lsfInterpol4[idx], &a, &lsfs)

	normBr := SmplGetNormalizedBitrate(params.TotalPulses, frameLength16)

	var lpcRes [SmplIntfLen]float32
	gainTab := gains.uv[:]
	if params.Voiced {
		gainTab = gains.v[:]
	}
	for pos := 0; pos < SmplIntfLen; pos++ {
		if pulses[pos] != 0 {
			sf := pos / SmplSubfrLen
			lpcRes[pos] = float32(pulses[pos]) * gainTab[params.FcbgIdx[sf]]
		}
	}

	const lagsPerSubfr = 2
	var ybuf [SmplOrder + SmplIntfLen]float32
	copy(ybuf[:SmplOrder], s.lpcSynthMem[:])
	if s.traceExcPre {
		s.ExcPre = s.ExcPre[:0]
	}
	for sf := 0; sf < SmplSubfrCount; sf++ {
		base := sf * SmplSubfrLen
		sfLags := []float32{params.BlockLags[2*sf], params.BlockLags[2*sf+1]}
		celpDecode(s.acbState, s.acbStateLen, params.Voiced, params.AcbgIdx[sf], sfLags, lagsPerSubfr, SmplSubfrLen, lowRate, normBr, lpcRes[base:base+SmplSubfrLen])

		if s.traceExcPre {
			s.ExcPre = append(s.ExcPre, lpcRes[base:base+SmplSubfrLen]...)
		}

		nrgres := SmplDecodeResnrg(params.NrgresDbqQ14[sf], int32(SmplSubfrLen))
		if !params.Voiced {
			s.prevNrgres = nrgres
		}
		var noise [160]float32
		SmplCelpGenNoise(&s.noise, lpcRes[base:base+SmplSubfrLen], SmplSubfrLen, params.Voiced, params.SfPulses[sf], nrgres, params.FcbgIdx[sf], lsfs[sf][:], normBr, gains.uv[:], noise[:])
		for i := 0; i < SmplSubfrLen; i++ {
			lpcRes[base+i] += noise[i]
		}

		filtAR16(lpcRes[base:base+SmplSubfrLen], &a[sf], ybuf[:], SmplOrder+base, SmplSubfrLen)
	}
	copy(out[:SmplIntfLen], ybuf[SmplOrder:])
	copy(s.lpcSynthMem[:], ybuf[SmplOrder+SmplIntfLen-SmplOrder:])

	// Post-LPC HP (pitch-harmonic) postfilter. The comb lag is the energy-weighted
	// mean of the 8 per-40-block lags (0 → default fixed-corner curve, unvoiced).
	var lag float32
	if params.Voiced {
		var sl, sll float32
		for _, l := range params.BlockLags {
			sl += l
			sll += l * l
		}
		if sl > 0.0 {
			lag = sll / sl
		}
	}
	var hpOut [SmplIntfLen]float32
	SmplHpPostfilter(&s.hp, out[:SmplIntfLen], SmplIntfLen, lag, hpOut[:])
	copy(out[:SmplIntfLen], hpOut[:])
}
