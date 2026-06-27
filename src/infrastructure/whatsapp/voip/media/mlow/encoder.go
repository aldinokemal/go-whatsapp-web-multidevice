package mlow

import (
	"errors"
	"math"

	"github.com/rs/zerolog"
)

// MLow ENCODER (module #16, outbound counterpart of mlow/decoder).
//
// This file holds the voiced/unvoiced classifier (smpl_signal_mode.rs) and the
// entropy coder (EncodeSmplFrame — the exact inverse of the byte-exact decoder).
// The classifier folds five voicing strengths (pitch correlation, VAD, spectral
// tilt, harmonicity, short lag) plus a per-stream hysteresis into a single
// voicing_strength; the encoder codes a frame voiced when that is positive and the
// packet is coded-as-active. The full PCM→wire path (MlowEncoder.Encode) drives the
// analysis front-end (analysis.go: LPC, perc, pitch, CELP, bitrate) → EncodeSmplFrame
// and round-trips a tone through the decoder (TestEncodeRoundTripsATone, corr 0.89).
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L1-L222

// SmplEncodeBufBytes is the range-encoder body capacity (mirrors SMPL_ENCODE_BUF_BYTES).
const SmplEncodeBufBytes = 512

// smpl_vuv_weights (smpl_tables.c): weights on corrs, vad, tilt, harmonicity,
// short lags. The C declares 6 but sums only the first 5.
var smplVuvWeights = [5]float32{1.0, 0.5, 0.5, 0.7, 0.3}

const (
	smplVuvBias      float32 = -0.1038
	smplVuvHyst      float32 = 0.05
	transitionIx             = SmplFLen / 3 // low/high spectral-tilt band split
	harmonicityUndef float32 = -10000.0
	numHarms                 = 4
)

// smplInvSigmoid is the C smpl_inv_sigmoid: -ln(1/x - 1).
func smplInvSigmoid(x float32) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L30-L33
	return -float32(math.Log(float64(1.0/x - 1.0)))
}

// vuvDot is smpl_dot_prod over the first l elements (float32 accumulation, to
// match the reference's f32 rounding).
func vuvDot(a, b []float32, l int) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L35-L42
	var s float32
	for i := 0; i < l; i++ {
		s += a[i] * b[i]
	}
	return s
}

// vuvSum is smpl_sum_vec over the first l elements.
func vuvSum(x []float32, l int) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L44-L51
	var s float32
	for i := 0; i < l && i < len(x); i++ {
		s += x[i]
	}
	return s
}

// VuvMode is the per-stream voicing hysteresis + spectral-tilt background tracker
// (VUV_Mode in the C). The encoder threads one instance across the whole stream;
// the zero value matches the C calloc init.
type VuvMode struct {
	nrgLoBgn    float32
	nrgHiBgn    float32
	voicingPrev float32
	lastLagPrev float32
}

// spectralHarmonicity (smpl_pitch_util.c): harmonic peak/valley energy ratio at
// low frequencies, from the per-bin weighted power spectrum f2w. cache is the C's
// per-call harmonicity memo keyed by harmonic bin; reset clears it.
func spectralHarmonicity(avgLag float32, f2w []float32, cache []float32, reset bool) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L78-L99
	if reset {
		for i := range cache {
			cache[i] = harmonicityUndef
		}
	}
	invF2StepHz := 2.0 * float32(SmplFLen-1) / 16000.0
	harmHz := 16000.0 / avgLag
	harmIx := int32(math.Round(float64(harmHz * 2.0 * invF2StepHz)))
	cacheLen := int32(len(cache))
	if harmIx >= cacheLen {
		// The C asserts this never happens; guard defensively and recompute.
		return recomputeHarmonicity(harmHz, invF2StepHz, f2w)
	}
	if cache[harmIx] > harmonicityUndef {
		return cache[harmIx]
	}
	hs := recomputeHarmonicity(harmHz, invF2StepHz, f2w)
	cache[harmIx] = hs
	return hs
}

func recomputeHarmonicity(harmHz, invF2StepHz float32, f2w []float32) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L103-L147
	harmWidth := harmHz * invF2StepHz
	harmStrength := float32(0.1)
	if harmWidth > 1.97 {
		var peakValleyMags [2*numHarms + 1]float32
		invHarmWidth := 1.0 / harmWidth
		for numHarm := 0; numHarm < len(peakValleyMags); numHarm++ {
			ixStart := 0.5 * float32(numHarm) * harmWidth
			ixEnd := ixStart + harmWidth
			idxStart := int32(math.Ceil(float64(ixStart)))
			idxEnd := int32(math.Floor(float64(ixEnd)))
			weightsLen := int(idxEnd - idxStart + 1)
			if weightsLen < 0 {
				weightsLen = 0
			}
			var weights [20]float32
			for i := 0; i < weightsLen && i < len(weights); i++ {
				tmp := (float32(idxStart) - ixStart + float32(i)) * invHarmWidth
				tmp -= tmp * tmp
				weights[i] = tmp * tmp
			}
			base := int(idxStart)
			if base < 0 {
				base = 0
			}
			if base > len(f2w) {
				base = len(f2w)
			}
			avail := len(f2w) - base
			if avail > weightsLen {
				avail = weightsLen
			}
			peakValleyNrg := vuvDot(f2w[base:], weights[:], avail) / vuvSum(weights[:], weightsLen)
			peakValleyMags[numHarm] = float32(math.Sqrt(float64(peakValleyNrg + 1e-30)))
		}
		var magRatiosLog [numHarms]float32
		var magWeights [numHarms]float32
		magPeakW := [3]float32{1.0, 10.0, 1.0}
		magValleyW := [3]float32{5.0, 2.0, 5.0}
		for numHarm := 0; numHarm < numHarms; numHarm++ {
			magPeak := magPeakW[0]*peakValleyMags[2*numHarm] +
				magPeakW[1]*peakValleyMags[2*numHarm+1] +
				magPeakW[2]*peakValleyMags[2*numHarm+2]
			magValley := magValleyW[0]*peakValleyMags[2*numHarm] +
				magValleyW[1]*peakValleyMags[2*numHarm+1] +
				magValleyW[2]*peakValleyMags[2*numHarm+2]
			magRatiosLog[numHarm] = float32(math.Log(float64(magPeak / magValley)))
			magWeights[numHarm] = float32(math.Sqrt(float64(magPeak + magValley + 1e-30)))
		}
		harmStrength = vuvDot(magWeights[:], magRatiosLog[:], numHarms) / vuvSum(magWeights[:], numHarms)
	}
	return harmStrength
}

// BuildF2w builds the C F2w (F2[i] * (i+3), with F2w[0]=F2w[1]=0).
func BuildF2w(f2 *[SmplFLen]float32) [SmplFLen]float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L150-L156
	var f2w [SmplFLen]float32
	for i := 2; i < SmplFLen; i++ {
		f2w[i] = f2[i] * float32(i+3)
	}
	return f2w
}

// HarmStrengthAt is the harmonicity at avgLag with a fresh cache (the C call
// right after the pitch search). Reused by the pitch estimator so its
// harm_strength matches the value fed to SmplGetSignalMode.
func HarmStrengthAt(avgLag float32, f2w *[SmplFLen]float32) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L160-L163
	var cache [50]float32
	return spectralHarmonicity(avgLag, f2w[:], cache[:], true)
}

// SmplGetSignalMode combines the five voicing strengths + hysteresis into the
// voicing strength; it mutates vuv. lags is the per-lag-subframe pitch lag in
// samples; f2 is the power spectrum F2[0..256].
func SmplGetSignalMode(
	pitchcorr float32,
	lags []float32,
	avgLag float32,
	harmStrength float32,
	f2 *[SmplFLen]float32,
	spActProb float32,
	vuv *VuvMode,
) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_signal_mode.rs#L168-L222
	pc := pitchcorr
	if pc < 0.0 {
		pc = 0.0
	} else if pc > 1.0 {
		pc = 1.0
	}
	corrStrength := smplInvSigmoid(0.1 + 0.75*pc)       // -1.4 .. 1.4
	vadStrength := 0.04 * (1.0 - 1.04/(spActProb+0.04)) // -1 .. 0

	// spectral tilt
	var nrgLo float32
	for i := 2; i < transitionIx; i++ {
		tmp := f2[i] * float32(i+3)
		nrgLo += tmp * float32(transitionIx-i)
	}
	var nrgHi float32
	for i := transitionIx; i < SmplFLen; i++ {
		tmp := f2[i] * float32(i+3)
		nrgHi += tmp * float32(i-transitionIx)
	}
	if vadStrength < -0.1 {
		smthCoef := -0.5 * vadStrength
		vuv.nrgLoBgn += smthCoef * (nrgLo - vuv.nrgLoBgn)
		vuv.nrgHiBgn += smthCoef * (nrgHi - vuv.nrgHiBgn)
	}
	loDiff := nrgLo - vuv.nrgLoBgn
	if loDiff < 0.0 {
		loDiff = 0.0
	}
	hiDiff := nrgHi - vuv.nrgHiBgn
	if hiDiff < 0.0 {
		hiDiff = 0.0
	}
	tiltLin := (loDiff - hiDiff) / (nrgLo + nrgHi + 1e-9)
	tiltStrength := tiltLin * tiltLin * tiltLin // make less binary
	lagStrength := -smplSigmoid(0.25 * (38.0 - avgLag))

	voicingStrength := (smplVuvWeights[0]*corrStrength+
		smplVuvWeights[1]*vadStrength+
		smplVuvWeights[2]*tiltStrength+
		smplVuvWeights[3]*harmStrength+
		smplVuvWeights[4]*lagStrength)/
		vuvSum(smplVuvWeights[:], 5) + smplVuvBias

	// hysteresis
	if vuv.lastLagPrev > 0.0 {
		tmp := float32(math.Log2(float64(lags[0] / vuv.lastLagPrev)))
		if tmp > 0.0 {
			tmp *= 0.5
		}
		vuv.voicingPrev /= 0.4 + tmp*tmp
	}
	voicingStrength += vuv.voicingPrev * smplVuvHyst
	vuv.voicingPrev = float32(math.Tanh(float64(3.0 * voicingStrength)))
	vuv.lastLagPrev = lags[len(lags)-1]

	return voicingStrength
}

// --- entropy encoder (the exact inverse of the byte-exact decoder) ----------

// ErrEncodeUnimplemented marks the parts of the encode path that are not yet built.
var ErrEncodeUnimplemented = errors.New("mlow encode: analysis front-end (pcm→params) not yet implemented")

// SmplRawSym is one uniform raw-symbol write (encode(sym, sym+1, 1<<nbits)).
type SmplRawSym struct {
	Sym   uint32
	Nbits uint32
}

// SmplLsfParams is one internal frame's LSF index set (inverse of DecodeSmplLsf).
type SmplLsfParams struct {
	Stage1 int32
	Grid   int32
	Stage2 [16]int32
	Extra  int32
}

// SmplPulseParams is one internal frame's excitation (inverse of DecodeSmplPulses).
// MagRuns/SignSyms are the raw entropy symbols replayed verbatim (the structured
// counts alone are lossy w.r.t. the exact bitstream).
type SmplPulseParams struct {
	Total    int32
	Subfr    [4]int32
	MagRuns  []int32
	SignSyms []SmplRawSym
}

// SmplGainParams is one unvoiced internal frame's gain block (inverse of DecodeSmplGains).
type SmplGainParams struct {
	GainMain  int32
	GainDelta int32
	NrgRes    [4]int32
}

// SmplPitchParams is one voiced internal frame's pitch block: the LTP gains/filters
// and the estimator's chosen contour (BlocksegIdx) + per-40-block lag indices
// (Laginds) that smpl_encode_lags writes straight to the wire.
type SmplPitchParams struct {
	GainIdx     [4]int32
	FiltIdx     [4]int32
	BlocksegIdx int
	Laginds     [8]int32
}

// SmplInternalParams is one 20 ms internal frame's full parameter set.
type SmplInternalParams struct {
	Lsf      SmplLsfParams
	Pulses   SmplPulseParams
	HasPitch bool
	Pitch    SmplPitchParams
	Gains    SmplGainParams
}

// SmplFrameParams is the analyzed parameter set for one 60 ms MLow frame.
type SmplFrameParams struct {
	TOC      byte
	Config   int
	Internal [3]SmplInternalParams
}

// encodeSmplLsf is the inverse of DecodeSmplLsf: mirror the selector/grid/16-residual/extra
// writes, mutating st identically (so the cross-internal-frame predictor stays in sync).
func encodeSmplLsf(enc *RangeEncoder, t *SmplTables, st *SmplLsfState, config, intf int, lsf *SmplLsfParams) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L100-L151
	sel := 0
	if intf != 0 {
		if st.PrevStage1 != 0 {
			sel = 2
		} else {
			sel = 1
		}
	}
	stage1 := lsf.Stage1
	enc.EncodeCDF(stage1, t.LsfSel[sel])

	m := intf != 0 && stage1 == st.PrevStage1
	if !m {
		st.PrevGainIdx = -1
		st.PrevFiltIdx = -1
		st.PrevLag = -1
		st.PrevFracLag = -1
		st.PrevLagblk = -1
		st.PrevLagidx = -1
	}
	st.PrevStage1 = stage1

	var gridCDF []uint16
	switch {
	case m && stage1 != 0:
		gridCDF = t.LsfGrid.Match1
	case m:
		gridCDF = t.LsfGrid.Match1Alt
	case stage1 != 0:
		gridCDF = t.LsfGrid.Match0Alt
	default:
		gridCDF = t.LsfGrid.Match0
	}
	enc.EncodeCDF(lsf.Grid, gridCDF)
	st.PrevMatch = m
	st.HavePrev = true

	st2 := t.LsfStage2[int(stage1)][config][int(lsf.Grid)]
	for k := 0; k < 16; k++ {
		enc.EncodeCDF(lsf.Stage2[k], st2[k])
	}
	enc.EncodeCDF(lsf.Extra, t.LsfExtra)
}

// encodeSmplPulses is the inverse of DecodeSmplPulses (config=0 NB count, p3=4):
// re-derive the count interval and split symbols from the per-subframe counts, then
// replay the recorded magnitude/sign symbols.
func encodeSmplPulses(enc *RangeEncoder, _ *SmplMem, p2, p3, p4, p6, s1 int32, pp *SmplPulseParams) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L153-L271
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/encode.rs#L153-L271 (seed cc-table rewire: split/runlen from CcTables)
	cc := LoadCcTables()
	idx := p4 + s1
	bByte := int32(Mem8Static(0xe8990 + uint32(p6*3+idx)))
	frameLen4k := bByte * p2 / 320
	subfrLen16 := frameLen4k / p3
	total := pp.Total

	// --- pulse COUNT (NB triangular; config=0) ---
	l := uint32(frameLen4k)
	triT := func(k uint32) uint32 {
		a := (k + 2) * (l + 1)
		b := ((k - 1) * (k + 131070)) >> 1
		return (a - b) & 0xffff
	}
	ft := triT(l)
	if ft == 0 {
		ft = 1
	}
	var fl uint32
	if total > 0 {
		fl = triT(uint32(total - 1))
	}
	fh := triT(uint32(total))
	enc.Encode(fl, fh, ft)
	if total == 0 {
		return
	}

	// --- recursive binary SPLIT ---
	finalSum := pp.Subfr[0] + pp.Subfr[1]
	initSum := total - subfrLen16*2
	if initSum < 0 {
		initSum = 0
	}
	lo := total - 80
	if lo < 0 {
		lo = 0
	}
	if initSum < lo {
		return
	}
	hiBound := total - lo
	if initSum < hiBound {
		cdf := cdfWindow(cc.SplitCmf(total), int(initSum-lo), int((hiBound-initSum)+2))
		enc.EncodeCDF(finalSum-initSum, cdf)
	}
	if finalSum > 0 {
		encodeSplit3537(enc, cc, finalSum, subfrLen16, pp.Subfr[0])
	}
	if finalSum < total {
		encodeSplit3537(enc, cc, total-finalSum, subfrLen16, pp.Subfr[2])
	}

	// --- MAGNITUDE block: replay recorded run-length symbols through the same loop ---
	posPer := p2 / p3
	magIdx := 0
	for subfr := int32(0); subfr < p3; subfr++ {
		cnt := pp.Subfr[subfr]
		if cnt <= 0 {
			continue
		}
		pos := posPer
		c := cnt
		k := int32(0)
		for k < cnt {
			oct := (pos + 7) / 8
			bucket := cc.Runlen(oct)
			start := int(bucket.MaxSamples() - pos)
			m := pp.MagRuns[magIdx]
			magIdx++
			enc.EncodeCDF(m, cdfWindow(bucket.Cmf(c), start, int(pos+1)))
			if m > 0 || k == 0 {
				pos -= m
			}
			c--
			k++
		}
	}

	// --- SIGN block: replay recorded raw sign symbols ---
	for _, rs := range pp.SignSyms {
		enc.EncodeRawSymbol(rs.Sym, rs.Nbits)
	}
}

// encodeSplit3537 is the inverse of smplSplit3537: encode the first-half count s0.
func encodeSplit3537(enc *RangeEncoder, cc *CcTables, count, granularity int32, s0 int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L273-L292
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/encode.rs#L273-L292 (seed cc-table rewire: SplitCmf)
	lo := count
	if granularity < lo {
		lo = granularity
	}
	minSplit := count - granularity
	if minSplit < 0 {
		minSplit = 0
	}
	if lo < minSplit || minSplit == lo {
		return
	}
	cdf := cdfWindow(cc.SplitCmf(count), int(minSplit), int((lo-minSplit)+2))
	enc.EncodeCDF(s0-minSplit, cdf)
}

// encodeSmplGains is the inverse of DecodeSmplGains: encode main/delta gain, then
// per-subframe nrgres with the same gain-derived address shift.
func encodeSmplGains(enc *RangeEncoder, _ *SmplMem, p3 int32, subfrCounts [4]int32, gp *SmplGainParams) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L294-L335
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/encode.rs#L294-L335 (seed cc-table rewire: Group A/E from CcTables)
	cc := LoadCcTables()
	enc.EncodeCDF(gp.GainMain, cc.NrgresGain4())
	enc.EncodeCDF(gp.GainDelta, cc.NrgresShape4())
	cfgSel := int32(2)

	off6 := p3 * gp.GainDelta
	base7 := gp.GainMain*cc.NrgStep(cfgSel) - 0x154000
	var gainQ [4]int32
	take := int(p3)
	if take > 4 {
		take = 4
	}
	for sf := 0; sf < take; sf++ {
		cbv := cc.GainRecon(p3 == 4, int32(sf)+off6)
		gainQ[sf] = base7 + (cbv << 4)
	}

	for sf := 0; sf < take; sf++ {
		cnt := subfrCounts[sf]
		if cnt <= 0 {
			continue
		}
		var bucket int32
		if cnt >= 30 {
			bucket = 3
		} else {
			bucket = (cnt & 0xffff) / 10
		}
		g := (gainQ[sf] + 8192) >> 14
		if g < -85 {
			g = -85
		}
		negPart := (g >> 31) & g
		minOffset := int(-negPart)
		enc.EncodeCDF(gp.NrgRes[sf], cc.FcbgOffset(int(cfgSel), int(bucket), minOffset))
	}
}

// encodeSmplPitch is the inverse of DecodeSmplPitch: encode the LTP gains/filters,
// then the lag contour (blockseg selector + per-block lag indices) via the pitch
// tables, mutating the predictor state identically.
func encodeSmplPitch(enc *RangeEncoder, _ *SmplMem, st *SmplLsfState, p2, p3, p6 int32, subfrCounts [4]int32, pp *SmplPitchParams) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L337-L405
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/encode.rs#L337-L405 (seed cc-table rewire: Group C LTP gains from CcTables)
	cc := LoadCcTables()

	var gainAccum int32
	take := int(p3)
	if take > 4 {
		take = 4
	}
	for sf := 0; sf < take; sf++ {
		cnt := subfrCounts[sf]
		gi := pp.GainIdx[sf]
		if p6 != 0 {
			enc.EncodeCDF(gi, cc.AcbgainRowLr(st.PrevGainIdx))
		} else {
			enc.EncodeCDF(gi, cc.AcbgainRow(st.PrevGainIdx))
		}
		st.PrevGainIdx = gi
		var w0, w2 int32
		if p6 != 0 {
			w0, w2 = cc.AcbgainWeightsLr(gi)
		} else {
			w0, w2 = cc.AcbgainWeights(gi)
		}
		gainAccum += w0 + 2*w2
		if cnt > 0 {
			fi := pp.FiltIdx[sf]
			if st.PrevFiltIdx == -1 {
				enc.EncodeCDF(fi, cc.FcbgainV())
			} else {
				enc.EncodeCDF(fi, cc.FcbgainVDelta(st.PrevFiltIdx))
			}
			st.PrevFiltIdx = fi
		}
	}
	avgGain := gainAccum / p3

	mode := 0
	if avgGain >= 10007 {
		if avgGain < 14085 {
			mode = 1
		} else {
			mode = 2
		}
	}
	tab := LoadPitchTables()
	encodeLagsWire(tab, enc, pp.BlocksegIdx, &pp.Laginds, st.PrevLagblk, st.PrevLagidx, mode)
	nblk, nidx := smplLagsPredictorAfter(tab, pp.BlocksegIdx, &pp.Laginds)
	st.PrevLagblk = nblk
	st.PrevLagidx = nidx
}

// EncodeSmplFrame builds [TOC || range-coded body] from analyzed frame parameters
// (the exact inverse of the decoder's active-frame body decode).
func EncodeSmplFrame(fp *SmplFrameParams, log ...zerolog.Logger) ([]byte, error) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L61-L102
	lg := pickLog(log)
	const p2, p3, p4 = int32(320), int32(4), int32(1)
	p6 := int32(fp.Config)
	lg.Trace().Uint8("toc_byte", fp.TOC).Int("config", fp.Config).Int("internal_frames", 3).Msg("encode frame")
	tbl := LoadSmplTables()
	mem := LoadSmplMem()
	enc := NewRangeEncoder(1 + SmplEncodeBufBytes)
	var st SmplLsfState
	for f := 0; f < 3; f++ {
		ip := &fp.Internal[f]
		lg.Trace().Int("intf", f).Bool("voiced", ip.Lsf.Stage1 == 1).Int32("stage1", ip.Lsf.Stage1).
			Int32("total_pulses", ip.Pulses.Total).Bool("has_pitch", ip.HasPitch).Msg("encode internal frame params")
		encodeSmplLsf(enc, tbl, &st, fp.Config, f, &ip.Lsf)
		encodeSmplPulses(enc, mem, p2, p3, p4, p6, ip.Lsf.Stage1, &ip.Pulses)
		if ip.Lsf.Stage1 == 1 {
			encodeSmplPitch(enc, mem, &st, p2, p3, p6, ip.Pulses.Subfr, &ip.Pitch)
		} else {
			encodeSmplGains(enc, mem, p3, ip.Pulses.Subfr, &ip.Gains)
		}
	}
	enc.Done()
	if enc.Err() != 0 {
		lg.Debug().Int32("err", enc.Err()).Msg("encode frame: range-encoder buffer overflow")
		return nil, errors.New("mlow encode: range-encoder buffer overflow")
	}
	n := enc.ConsumedLen()
	body := enc.Bytes()
	out := make([]byte, 0, 1+n)
	out = append(out, fp.TOC)
	out = append(out, body[:n]...)
	lg.Trace().Int("frame_bytes", len(out)).Int("body_bytes", n).Msg("encode frame: done")
	return out, nil
}

// MlowEncoder is the stateful top-level MLow encoder. The cross-frame analysis
// history (SmplEncoderState, in analysis.go) persists across Encode calls.
type MlowEncoder struct {
	state SmplEncoderState
	log   zerolog.Logger
}

// NewMlowEncoder allocates a fresh encoder.
func NewMlowEncoder(opts ...Option) *MlowEncoder {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L33-L37
	return &MlowEncoder{log: resolveConfig(opts).log}
}

// Reset clears the cross-frame analysis history (call at a stream discontinuity).
func (e *MlowEncoder) Reset() {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L40-L43
	e.state = SmplEncoderState{}
}

// Encode turns one 60 ms frame (exactly 960 samples) into a wire MLow frame:
// sanitize (NaN→0, clamp [-1,1]) → analysis (PCM → SmplFrameParams) → entropy code.
func (e *MlowEncoder) Encode(pcm []float32) ([]byte, error) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L46-L57
	if len(pcm) != opusFrameSamps {
		e.log.Debug().Int("samples", len(pcm)).Int("want", opusFrameSamps).Msg("encode: wrong frame size")
		return nil, errors.New("mlow encode: expected 960 samples (60 ms @16 kHz)")
	}
	e.log.Trace().Int("samples", len(pcm)).Msg("encode frame: sanitizing and analyzing")
	clean := make([]float32, len(pcm))
	for i, s := range pcm {
		switch {
		case math.IsNaN(float64(s)):
			s = 0.0
		case s < -1.0:
			s = -1.0
		case s > 1.0:
			s = 1.0
		}
		clean[i] = s
	}
	fp := smplAnalyzeFrameSt(&e.state, clean)
	return EncodeSmplFrame(&fp, e.log)
}
