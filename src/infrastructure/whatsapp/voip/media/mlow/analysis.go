package mlow

import "math"

// MLow encoder analysis — faithful port of analysis.rs (datasheets/mlow-encoder.md):
// PCM → SmplFrameParams. Per internal frame: LPC front-end (window → FFT-autocorr →
// A/NLSF) → bit-exact LSF quantizer → perceptual model + multi-stage pitch estimator
// + voicing classifier → CELP excitation encode → candidate selection (voiced LTP /
// unvoiced nrgres / silent), committed to a shadow synth for warm history, advancing
// the entropy predictor mirror. Validated end-to-end by the tone round-trip.

const (
	smplLpcHistLen           = 144 // C lpc_buf_mem
	smplLpcPre               = 96
	smplLsfSurv              = 6 // lsf_surv at complexity 8
	smplWinnextWbLen         = 32
	smplLsfRdwAdj    float32 = 1.1952286

	smplMainBitRate = 20000
	smplComplexity  = 8

	smplCelpLowRate                = false
	smplCelpPercRespLen            = 32
	smplCelpFcbSubfrlen            = 80
	smplCelpSubfrPerPacket         = 12
	smplPercRLen                   = smplCelpPercRespLen + 1 // 33
	smplFcbTotSurv20msMax          = 100
	smplEncHpFcornerHz     float32 = 35.0

	smplPercEmphPitch     float32 = -0.82
	smplPitchPercRespLen          = 17
	smplPitchLagMax               = 320
	smplPitchLookaheadLen         = 7
	smplVoicedNormGain    float64 = 1.0
)

// SmplEncoderState is the cross-frame analysis history (only the LPC-analysis input
// history + the persistent sub-models persist; the decoder rebuilds synth per frame).
type SmplEncoderState struct {
	hist        []float64
	hpMA, hpAR  [3]float32
	hpSet       bool
	hpState     [4]float32
	celp        *CelpEncoder
	perc        *PercModelState
	percPrev    []float32
	bitrate     *BitrateController
	lpcHist     []float32
	prevLsfq    []float32
	prevVoiced  bool
	vad         *SmplVadState
	vuv         VuvMode
	hpPitchHist []float32
	ltpBuf      []float32
	pitchEst    PitchEstState
}

func unvoicedPitch() SmplPitchSynth { return SmplPitchSynth{} }

type candidate struct {
	ip       SmplInternalParams
	stage1   int32
	grid     int32
	qsym     [16]int32
	pulseVec []int32
	gainQ    [4]int32
	pitch    SmplPitchSynth
	silent   bool
}

// celpFrameCtx is the borrowed CELP/perceptual state for one internal frame.
type celpFrameCtx struct {
	celp               *CelpEncoder
	perc               *PercModelState
	percPrev           *[]float32
	bitrate            *BitrateController
	hpN                []float32
	intf               int
	spActProb          float32
	codedAsActiveVoice bool
	f2                 [SmplFLen]float32
	voicingStrength    float32
	vuv                *VuvMode
	hpPitchHist        []float32
	ltpBuf             *[]float32
	pitchEst           *PitchEstState
	percCorrs          [][]float32
	blockLags          [SmplSubfrCount][2]float32
}

type frontEndLsf struct {
	a          [SmplLPCOrder + 1]float32
	nlsf       [SmplLPCOrder]float32
	prevLsfq   []float32
	prevVoiced bool
	intf       int
}

// smplAnalyzeFrameSt turns one 60 ms PCM frame (960 f32 @16 kHz, ~[-1,1]) into params.
func smplAnalyzeFrameSt(es *SmplEncoderState, pcm []float32) SmplFrameParams {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L857-L1046
	need := SmplIntfLen * 3
	if len(pcm) < need {
		o := make([]float32, need)
		copy(o, pcm)
		pcm = o
	}
	synthT := LoadSmplSynthTables()

	pcmI16 := make([]int16, need)
	for i := 0; i < need; i++ {
		v := math.Round(float64(pcm[i] * 32768.0))
		if v > 32767 {
			v = 32767
		}
		if v < -32768 {
			v = -32768
		}
		pcmI16[i] = int16(v)
	}
	if es.vad == nil {
		es.vad = NewSmplVadState()
	}
	vad := es.vad.ProcessPacket(pcmI16, SmplIntfLen)
	spActProb := vad.VadResults
	codedAsActiveVoice := vad.CodedAsActiveVoice

	if !es.hpSet {
		es.hpMA, es.hpAR = SmplGetHpCoefs(smplEncHpFcornerHz)
		es.hpSet = true
	}
	pcmIn := append([]float32(nil), pcm[:need]...)
	hp := make([]float32, need)
	SmplFiltArma2(pcmIn, need, es.hpMA, es.hpAR, &es.hpState, hp)

	x := make([]float64, SmplOrder+need)
	if len(es.hist) >= SmplOrder {
		copy(x[:SmplOrder], es.hist[len(es.hist)-SmplOrder:])
	}
	for i := 0; i < need; i++ {
		x[SmplOrder+i] = float64(hp[i]) * 32768.0
	}

	shadow := NewSmplFrameSynth()
	var prevNlsf []float32
	var lstate SmplLsfState

	if es.celp == nil {
		es.celp = NewCelpEncoder(smplCelpLowRate, smplCelpPercRespLen, smplCelpFcbSubfrlen, smplCelpSubfrPerPacket)
	}
	if es.perc == nil {
		es.perc = NewPercModelState()
	}
	if es.bitrate == nil {
		es.bitrate = NewBitrateController()
	}
	if len(es.percPrev) != smplPercRLen {
		es.percPrev = make([]float32, smplPercRLen)
	}

	resLead := SmplOrder + smplWinnextWbLen
	xn := make([]float32, resLead+need)
	if len(es.hist) >= resLead {
		for i := 0; i < resLead; i++ {
			xn[i] = float32(es.hist[len(es.hist)-resLead+i] / 32768.0)
		}
	}
	copy(xn[resLead:resLead+need], hp[:need])

	hpFull := make([]float32, smplLpcHistLen+need+smplWinnextWbLen)
	if len(es.lpcHist) == smplLpcHistLen {
		copy(hpFull[:smplLpcHistLen], es.lpcHist)
	}
	copy(hpFull[smplLpcHistLen:smplLpcHistLen+need], hp[:need])

	hpPitchHist := make([]float32, smplPitchLagMax)
	if len(es.hpPitchHist) == smplPitchLagMax {
		copy(hpPitchHist, es.hpPitchHist)
	}
	es.hpPitchHist = append([]float32(nil), hp[need-smplPitchLagMax:need]...)

	if len(es.ltpBuf) != MaxLTPBufLen {
		es.ltpBuf = make([]float32, MaxLTPBufLen)
	}

	prevLsfq := append([]float32(nil), es.prevLsfq...)
	prevVoiced := es.prevVoiced

	var internal [3]SmplInternalParams
	for f := 0; f < 3; f++ {
		base := SmplOrder + f*SmplIntfLen
		win := x[base-SmplOrder : base+SmplIntfLen]
		nbase := resLead + f*SmplIntfLen
		winN := xn[nbase-resLead : nbase+SmplIntfLen]

		lpcStart := smplLpcHistLen - smplLpcPre + f*SmplIntfLen
		var lpcbuf [SmplLPCBufLen]float32
		copy(lpcbuf[:], hpFull[lpcStart:lpcStart+SmplLPCBufLen])
		windowed := smplWindowLPC20(&lpcbuf, f < 2)
		a, f2 := smplLPCAnalyzeWithF2(&windowed)
		nlsf := smplA2NLSF16(a[:])

		cs := celpFrameCtx{
			celp:               es.celp,
			perc:               es.perc,
			percPrev:           &es.percPrev,
			bitrate:            es.bitrate,
			hpN:                hp,
			intf:               f,
			spActProb:          spActProb[f],
			codedAsActiveVoice: codedAsActiveVoice,
			f2:                 f2,
			vuv:                &es.vuv,
			hpPitchHist:        hpPitchHist,
			ltpBuf:             &es.ltpBuf,
			pitchEst:           &es.pitchEst,
		}
		var feA [SmplLPCOrder + 1]float32
		copy(feA[:], a[:])
		var feNlsf [SmplLPCOrder]float32
		copy(feNlsf[:], nlsf[:])
		fe := frontEndLsf{a: feA, nlsf: feNlsf, prevLsfq: prevLsfq, prevVoiced: prevVoiced, intf: f}

		ip, nlsfOut, voicedOut := smplAnalyzeInternal(synthT, shadow, &lstate, f, win, winN, prevNlsf, &fe, &cs)
		prevNlsf = nlsfOut
		prevLsfq = nlsfOut
		prevVoiced = voicedOut
		internal[f] = ip
		if f == 2 {
			es.pitchEst.ResetCond()
		}
	}

	es.hist = append([]float64(nil), x[len(x)-(SmplOrder+smplWinnextWbLen):]...)
	es.lpcHist = append([]float32(nil), hp[need-smplLpcHistLen:need]...)
	es.prevLsfq = prevLsfq
	es.prevVoiced = prevVoiced
	return SmplFrameParams{TOC: 0x50, Config: 0, Internal: internal}
}

// quantize runs the bit-exact LSF quantizer + the C cond-coding condition.
func (fe *frontEndLsf) quantize(synthT *SmplSynthTables, voiced int, prevNlsf []float32) (int32, [16]int32, []float32, [17]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1065-L1105
	cond := (fe.prevVoiced == (voiced != 0)) && fe.intf > 0
	var res LsfQuantResult
	if cond && len(fe.prevLsfq) == SmplLPCOrder {
		res = LsfQuantCond(fe.a[:], fe.nlsf[:], fe.prevLsfq, voiced, 0, smplLsfRdwAdj, smplLsfSurv)
	} else {
		res = LsfQuant(fe.a[:], fe.nlsf[:], voiced, 0, smplLsfRdwAdj, smplLsfSurv)
	}
	grid := res.Qi[0]
	var stage2 [16]int32
	copy(stage2[:], res.Qi[1:1+SmplLPCOrder])
	committed := SmplReconstructNLSF(synthT, voiced, 0, int(grid), &stage2, prevNlsf)
	aVq := SmplNLSF2A(committed)
	var predcoef [17]float32
	for i := 0; i < 17 && i < len(aVq); i++ {
		predcoef[i] = aVq[i]
	}
	predcoef[0] = 1.0
	return grid, stage2, committed, predcoef
}

func commitCandidate(synthT *SmplSynthTables, st *SmplFrameSynth, cand *candidate, prevNlsf []float32) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1108-L1151
	if cand.silent {
		nlsf := SmplReconstructNLSF(synthT, 0, 0, int(cand.ip.Lsf.Grid), &cand.ip.Lsf.Stage2, prevNlsf)
		pulseVec := make([]int32, SmplIntfLen)
		var st2 [16]int32 = cand.ip.Lsf.Stage2
		SynthInternalFrame(synthT, st, 0, 0, int(cand.ip.Lsf.Grid), &st2, prevNlsf, pulseVec, &cand.gainQ, &cand.pitch)
		return nlsf
	}
	var qsym [16]int32 = cand.qsym
	_, nlsf := SynthInternalFrame(synthT, st, int(cand.stage1), 0, int(cand.grid), &qsym, prevNlsf, cand.pulseVec, &cand.gainQ, &cand.pitch)
	return nlsf
}

func smplUnvoicedCandidate(synthT *SmplSynthTables, _ *SmplFrameSynth, win []float64, winN []float32, prevNlsf []float32, fe *frontEndLsf, cs *celpFrameCtx) candidate {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1153-L1273
	frame := win[SmplOrder:]
	r0 := smplAutocorr(frame, 0)[0]
	if r0 <= 0.0 {
		var flat [SmplSubfrCount][17]float32
		for sf := range flat {
			flat[sf][0] = 1.0
		}
		percCorrs := cs.percCorrs
		runCelpSubframes(cs, &flat, make([]float32, SmplIntfLen), &[SmplSubfrCount][2]float32{}, percCorrs, SmplPercEmphUV, 0)
		return smplSilentInternal(synthT)
	}

	bgrid, bsym, brec, _ := fe.quantize(synthT, 0, prevNlsf)
	predcoefs, resLpc, interpolIdx := smplLsfInterpolSearch(brec, fe.prevLsfq, winN)

	percCorrs := cs.percCorrs
	celpOut := runCelpSubframes(cs, &predcoefs, resLpc, &[SmplSubfrCount][2]float32{}, percCorrs, SmplPercEmphUV, 0)

	pulseVec := make([]int32, SmplIntfLen)
	var fcbgIdx [4]int32
	const main = 1
	for sf := 0; sf < SmplSubfrCount; sf++ {
		out := &celpOut[sf]
		for _, v := range out.Pulses[main] {
			sign := int32(1) + 2*(int32(v)>>15)
			pos := int32(v)*sign - 1
			if pos >= 0 && pos < int32(SmplSubfrLen) {
				pulseVec[sf*SmplSubfrLen+int(pos)] += sign
			}
		}
		fcbgIdx[sf] = int32(out.GainIdx[main])
	}

	var nrgres [4]float32
	for sf := 0; sf < 4; sf++ {
		res := resLpc[sf*SmplSubfrLen : (sf+1)*SmplSubfrLen]
		var e float32
		for _, v := range res {
			e += v * v
		}
		nrgres[sf] = e / float32(SmplSubfrLen)
	}
	nq := QuantNrgRes4(&nrgres)
	gm := nq.FrameQi
	gd := nq.ShapeQi
	gainQ := nq.DbqQ14

	pp := smplBuildPulseParams(pulseVec)
	gains := SmplGainParams{GainMain: gm, GainDelta: gd, NrgRes: [4]int32{-1, -1, -1, -1}}
	for sf := 0; sf < 4; sf++ {
		if pp.Subfr[sf] > 0 {
			gains.NrgRes[sf] = fcbgIdx[sf]
		} else {
			gains.NrgRes[sf] = -1
		}
	}

	return candidate{
		ip: SmplInternalParams{
			Lsf:    SmplLsfParams{Stage1: 0, Grid: bgrid, Stage2: bsym, Extra: interpolIdx},
			Pulses: pp,
			Gains:  gains,
		},
		stage1:   0,
		grid:     bgrid,
		qsym:     bsym,
		pulseVec: pulseVec,
		gainQ:    gainQ,
		pitch:    unvoicedPitch(),
	}
}

func runCelpSubframes(cs *celpFrameCtx, predcoefs *[SmplSubfrCount][17]float32, resLpc []float32, blockLags *[SmplSubfrCount][2]float32, percCorrs [][]float32, emph [2]float32, voiced int32) []CelpSubframeOut {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1279-L1359
	percWght := percCorrsToWght(percCorrs, emph, smplCelpPercRespLen)
	outs := make([]CelpSubframeOut, 0, SmplSubfrCount)

	wnrgs := make([]float32, SmplSubfrCount)
	for sf := 0; sf < SmplSubfrCount; sf++ {
		res := resLpc[sf*SmplSubfrLen : (sf+1)*SmplSubfrLen]
		const scale = 32768.0
		var s float32
		for _, v := range res {
			s += (v * scale) * (v * scale)
		}
		wnrgs[sf] = s
	}

	enc := BitrateControllerInputs{
		InternalSampleRate: 16000, PayloadSizeMs: 60, FecBitRate: 0, MainBitRate: smplMainBitRate,
		Complexity: smplComplexity, UseFecRateCompensation: 0, UseDtx: 0, SubFrameImportanceFactor: 1.0,
	}

	for sf := 0; sf < SmplSubfrCount; sf++ {
		wnrg := wnrgs[sf]
		wnrgNext := wnrgs[sf]
		if sf+1 < SmplSubfrCount {
			wnrgNext = wnrgs[sf+1]
		}
		var nonflatness float32 = 2.0
		if voiced != 0 {
			nonflatness = 0.0
		}
		maxPulses, importance := cs.bitrate.control(&enc, 0, boolToInt(cs.codedAsActiveVoice), cs.spActProb, nonflatness, cs.voicingStrength, voiced, wnrg, wnrgNext, 0, 320, 80)
		numsurv := make([]int16, smplMaxPulsesPerSf)
		for i := range numsurv {
			numsurv[i] = 1
		}
		totSurv := int32(1000 * (smplFcbTotSurv20msMax * smplCelpFcbSubfrlen) / (20 * 16000))
		smplDistributeFcbSurv(numsurv, int32(maxPulses[1]), totSurv)

		lags := []float32{blockLags[sf][0], blockLags[sf][1], blockLags[sf][1]}
		res := resLpc[sf*SmplSubfrLen : (sf+1)*SmplSubfrLen]
		pc := predcoefs[sf]
		out := cs.celp.EncodeSubframe(res, &pc, percWght[sf], lags, importance, maxPulses, numsurv)
		outs = append(outs, out)
	}
	return outs
}

// computePercCorrs computes the per-subframe perceptual autocorrelation (advances
// perc state EXACTLY ONCE per internal frame).
func computePercCorrs(cs *celpFrameCtx) [SmplSubfrCount][]float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1367-L1397
	const frameMs = 20
	const shorter = 32
	var corrs [SmplSubfrCount][]float32
	for sf := 1; sf < SmplSubfrCount; sf += 2 {
		start := cs.intf*SmplIntfLen + (sf-1)*SmplSubfrLen
		xlen := 2*SmplSubfrLen + shorter
		xsubfr := make([]float32, xlen)
		for i := 0; i < xlen; i++ {
			idx := start + i
			if idx < len(cs.hpN) {
				xsubfr[i] = cs.hpN[idx]
			}
		}
		isLast := int32(0)
		if cs.intf == 2 && sf == SmplSubfrCount-1 {
			isLast = 1
		}
		r := SmplPercModel(cs.perc, xsubfr, xlen, frameMs, isLast, smplPercRLen)
		even := make([]float32, smplPercRLen)
		for i := 0; i < smplPercRLen; i++ {
			var prev float32
			if i < len(*cs.percPrev) {
				prev = (*cs.percPrev)[i]
			}
			even[i] = 0.5 * (r[i] + prev)
		}
		corrs[sf-1] = even
		*cs.percPrev = append([]float32(nil), r...)
		corrs[sf] = r
	}
	return corrs
}

func percCorrsToWght(corrs [][]float32, emph [2]float32, respLen int) [][]float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1401-L1414
	idx := 0
	if smplCelpLowRate {
		idx = 1
	}
	out := make([][]float32, len(corrs))
	for i, c := range corrs {
		out[i] = SmplPercAc2a(c, smplPercRLen, emph[idx], respLen, SmplPercReg)
	}
	return out
}

func smplLsfInterpolSearch(brec, prevLsfq []float32, winN []float32) ([SmplSubfrCount][17]float32, []float32, int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1420-L1447
	residualFor := func(idx int) ([SmplSubfrCount][17]float32, []float32, float32) {
		pc4, _ := smplLPCInterpolIdx(brec, prevLsfq, idx, SmplNLSF2A)
		var predcoefs [SmplSubfrCount][17]float32
		for sf := 0; sf < SmplSubfrCount; sf++ {
			predcoefs[sf] = pc4[sf]
		}
		res := make([]float32, SmplIntfLen)
		var sumRms float32
		for sf := 0; sf < SmplSubfrCount; sf++ {
			r := smplAnalysisResidualSubfr(&predcoefs[sf], winN, sf)
			var nrg float32
			for _, v := range r {
				nrg += v * v
			}
			sumRms += float32(math.Sqrt(float64(nrg + 1e-30)))
			copy(res[sf*SmplSubfrLen:(sf+1)*SmplSubfrLen], r[:])
		}
		return predcoefs, res, sumRms
	}
	pc0, res0, rms0 := residualFor(0)
	pc1, res1, rms1 := residualFor(1)
	if rms1 < rms0*0.998 {
		return pc1, res1, 1
	}
	return pc0, res0, 0
}

func smplAnalysisResidualSubfr(aSyn *[17]float32, winN []float32, sf int) [SmplSubfrLen]float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1451-L1466
	var res [SmplSubfrLen]float32
	for n := 0; n < SmplSubfrLen; n++ {
		idx := SmplOrder + sf*SmplSubfrLen + n
		acc := winN[idx]
		for j := 1; j <= SmplOrder; j++ {
			acc += aSyn[j] * winN[idx-j]
		}
		res[n] = acc
	}
	return res
}

func smplSilentInternal(synthT *SmplSynthTables) candidate {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1468-L1500
	var sym [16]int32
	for k := 0; k < 16; k++ {
		sym[k] = int32(len(synthT.Valtables[0][0][0][k]) / 2)
	}
	gm, gd, _ := smplRateControlGains(0.0)
	return candidate{
		ip: SmplInternalParams{
			Lsf:   SmplLsfParams{Stage1: 0, Grid: 0, Stage2: sym, Extra: 0},
			Gains: SmplGainParams{GainMain: gm, GainDelta: gd, NrgRes: [4]int32{-1, -1, -1, -1}},
		},
		stage1:   0,
		grid:     0,
		qsym:     sym,
		pulseVec: make([]int32, SmplIntfLen),
		pitch:    unvoicedPitch(),
		silent:   true,
	}
}

func smplAutocorr(x []float64, order int) []float64 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1502-L1513
	n := len(x)
	r := make([]float64, order+1)
	for lag := 0; lag <= order; lag++ {
		var s float64
		for i := lag; i < n; i++ {
			s += x[i] * x[i-lag]
		}
		r[lag] = s
	}
	return r
}

func smplBuildPulseParams(pulse []int32) SmplPulseParams {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1515-L1580
	const p3 = 4
	posPer := SmplIntfLen / p3
	var pp SmplPulseParams
	for sf := 0; sf < p3; sf++ {
		var s int32
		for n := sf * posPer; n < (sf+1)*posPer; n++ {
			a := pulse[n]
			if a < 0 {
				a = -a
			}
			s += a
		}
		pp.Subfr[sf] = s
	}
	pp.Total = pp.Subfr[0] + pp.Subfr[1] + pp.Subfr[2] + pp.Subfr[3]

	var magRuns []int32
	var signs []int32
	for sf := 0; sf < p3; sf++ {
		if pp.Subfr[sf] <= 0 {
			continue
		}
		basePos := posPer * sf
		runPos := int32(basePos)
		first := true
		for n := basePos; n < basePos+posPer; n++ {
			if pulse[n] == 0 {
				continue
			}
			magv := pulse[n]
			mag := magv
			if mag < 0 {
				mag = -mag
			}
			var m int32
			if first {
				m = int32(n) - int32(basePos)
			} else {
				m = int32(n) - runPos
			}
			magRuns = append(magRuns, m)
			runPos = int32(n)
			if mag > 1 {
				for k := int32(0); k < mag-1; k++ {
					magRuns = append(magRuns, 0)
				}
			}
			if magv < 0 {
				signs = append(signs, -1)
			} else {
				signs = append(signs, 1)
			}
			first = false
		}
	}
	pp.MagRuns = magRuns

	numPos := len(signs)
	var signSyms []SmplRawSym
	p := 0
	for p < numPos {
		nbits := numPos - p
		if nbits > 15 {
			nbits = 15
		}
		var sym uint32
		for q := 0; q < nbits; q++ {
			var bit uint32
			if signs[p+q] > 0 {
				bit = 1
			}
			sym |= bit << uint(nbits-1-q)
		}
		signSyms = append(signSyms, SmplRawSym{Sym: sym, Nbits: uint32(nbits)})
		p += nbits
	}
	pp.SignSyms = signSyms
	return pp
}

func smplRateControlGains(targetLinear float64) (int32, int32, int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1583-L1605
	mem := LoadSmplMem()
	cfgSel := uint32(2)
	cb1 := int32(mem.I16(0xf35e0 + cfgSel*2))
	gainTabAddr := uint32(0xf35f0)
	bestD := math.Inf(1)
	var bgm, bgd, bgq int32
	for gm := int32(0); gm < 84; gm++ {
		base7 := gm*cb1 - 0x154000
		for gd := int32(0); gd < 98; gd++ {
			cbv := int32(mem.I16(gainTabAddr + uint32(4*gd)*2))
			gq := base7 + (cbv << 4)
			d := math.Abs(SmplGainLin(gq) - targetLinear)
			if d < bestD {
				bestD = d
				bgm, bgd, bgq = gm, gd, gq
			}
		}
	}
	return bgm, bgd, bgq
}

// buildLtpBuf rolls the persistent perceptually-weighted speech buffer and writes
// this internal frame's weighted speech + lookahead into its tail.
func buildLtpBuf(cs *celpFrameCtx, percCorrs [][]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1625-L1690
	respPitch := percCorrsToWght(percCorrs, [2]float32{smplPercEmphPitch, smplPercEmphPitch}, smplPitchPercRespLen)
	maxLen := MaxLTPBufLen
	look := smplPitchLookaheadLen
	framelen := SmplIntfLen
	ltp := *cs.ltpBuf
	keep := maxLen - framelen - look
	copy(ltp[0:keep], ltp[framelen:framelen+keep])

	frameStart := cs.intf*SmplIntfLen - smplWinnextWbLen
	hist := smplPitchLagMax
	sample := func(rel int) float32 {
		idx := frameStart + rel
		if idx >= 0 {
			if idx < len(cs.hpN) {
				return cs.hpN[idx]
			}
			return 0.0
		}
		if len(cs.hpPitchHist) == hist {
			k := idx + hist
			if k >= 0 {
				return cs.hpPitchHist[k]
			}
		}
		return 0.0
	}
	wOrigin := maxLen - SmplSubfrCount*SmplSubfrLen - look
	for i := 0; i < SmplSubfrCount; i++ {
		coef := respPitch[i]
		for n := 0; n < SmplSubfrLen; n++ {
			pos := i*SmplSubfrLen + n
			res := sample(pos)
			for j := 1; j < smplPitchPercRespLen; j++ {
				res += coef[j] * sample(pos-j)
			}
			ltp[wOrigin+i*SmplSubfrLen+n] = res
		}
	}
	coef := respPitch[SmplSubfrCount-1]
	for n := 0; n < look; n++ {
		pos := framelen + n
		res := sample(pos)
		for j := 1; j < smplPitchPercRespLen; j++ {
			res += coef[j] * sample(pos-j)
		}
		ltp[maxLen-look+n] = res
	}
}

func smplAnalyzeInternal(synthT *SmplSynthTables, st *SmplFrameSynth, lstate *SmplLsfState, intf int, win []float64, winN []float32, prevNlsf []float32, fe *frontEndLsf, cs *celpFrameCtx) (SmplInternalParams, []float32, bool) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1697-L1771
	corrs := computePercCorrs(cs)
	cs.percCorrs = corrs[:]
	buildLtpBuf(cs, append([][]float32(nil), cs.percCorrs...))
	f2 := cs.f2
	ltpBuf := append([]float32(nil), (*cs.ltpBuf)...)
	pr := SmplPitch(cs.pitchEst, ltpBuf, &f2, cs.codedAsActiveVoice)
	lags8 := pr.Lags
	lagSamples := pr.Lags[0]
	vstr := SmplGetSignalMode(pr.Pitchcorr, lags8[:], pr.AvgLag, pr.HarmStrength, &f2, cs.spActProb, cs.vuv)
	cs.voicingStrength = vstr
	isVoicedDecision := vstr > 0.0 && cs.codedAsActiveVoice
	if isVoicedDecision {
		lstate.PrevLagSamples = lagSamples
	} else {
		lstate.PrevLagSamples = 0.0
	}
	if !isVoicedDecision {
		cs.pitchEst.ResetCond()
		lags8 = [8]float32{}
	}

	voicedLstate := *lstate
	SmplAdvanceLsfState(&voicedLstate, intf, 1)
	var vd *voicedDecision
	if isVoicedDecision {
		vd = smplVoicedDecisionForLag(pr.BlocksegIdx, &pr.Laginds, cs, &lags8)
	}

	var chosen candidate
	var chosenLstate *SmplLsfState
	var isVoiced bool
	if vd != nil {
		chosen = smplVoicedCandidate(synthT, win, prevNlsf, fe, cs, vd)
		chosenLstate = &voicedLstate
		isVoiced = true
	} else {
		chosen = smplUnvoicedCandidate(synthT, st, win, winN, prevNlsf, fe, cs)
		isVoiced = false
	}
	committedNlsf := commitCandidate(synthT, st, &chosen, prevNlsf)
	if chosen.stage1 == 1 {
		*lstate = *chosenLstate
		smplReplayPitchState(lstate, 4, chosen.ip.Pulses.Subfr, &chosen.ip.Pitch)
	} else {
		SmplAdvanceLsfState(lstate, intf, chosen.stage1)
	}
	return chosen.ip, committedNlsf, isVoiced
}

func smplReplayPitchState(st *SmplLsfState, p3 int32, subfrCounts [4]int32, pp *SmplPitchParams) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1776-L1794
	take := int(p3)
	if take > 4 {
		take = 4
	}
	for sf := 0; sf < take; sf++ {
		st.PrevGainIdx = pp.GainIdx[sf]
		if subfrCounts[sf] > 0 {
			st.PrevFiltIdx = pp.FiltIdx[sf]
		}
	}
	tab := LoadPitchTables()
	nblk, nidx := smplLagsPredictorAfter(tab, pp.BlocksegIdx, &pp.Laginds)
	st.PrevLagblk = nblk
	st.PrevLagidx = nidx
}

type voicedDecision struct {
	pp    SmplPitchParams
	pitch SmplPitchSynth
}

func smplVoicedDecisionForLag(blocksegIdx int, laginds *[8]int32, cs *celpFrameCtx, lags8 *[8]float32) *voicedDecision {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1808-L1838
	var blockLags8 [8]float32
	for b := 0; b < 8; b++ {
		v := float32(laginds[b])*0.5 + 32.0
		if v > 320.0 {
			v = 320.0
		}
		blockLags8[b] = v
	}
	*lags8 = blockLags8
	for sf := 0; sf < SmplSubfrCount; sf++ {
		cs.blockLags[sf] = [2]float32{blockLags8[2*sf], blockLags8[2*sf+1]}
	}
	var meanLag float32
	for _, v := range blockLags8 {
		meanLag += v
	}
	meanLag /= 8.0

	pp := SmplPitchParams{GainIdx: [4]int32{5, 5, 5, 5}, BlocksegIdx: blocksegIdx, Laginds: *laginds}
	pitch := SmplPitchSynth{Voiced: true, LagSubfr: [4]float64{float64(meanLag), float64(meanLag), float64(meanLag), float64(meanLag)}, NormGain: smplVoicedNormGain}
	return &voicedDecision{pp: pp, pitch: pitch}
}

func smplVoicedCandidate(synthT *SmplSynthTables, win []float64, prevNlsf []float32, fe *frontEndLsf, cs *celpFrameCtx, vd *voicedDecision) candidate {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/analysis.rs#L1846-L1930
	winN := make([]float32, len(win))
	for i, v := range win {
		winN[i] = float32(v / 32768.0)
	}
	gainQ := [4]int32{}

	bgrid, bsym, brec, _ := fe.quantize(synthT, 1, prevNlsf)
	pc4, _ := smplLPCInterpol(brec, fe.prevLsfq, SmplNLSF2A)
	var predcoefs [SmplSubfrCount][17]float32
	for sf := 0; sf < SmplSubfrCount; sf++ {
		predcoefs[sf] = pc4[sf]
	}
	resLpc := make([]float32, SmplIntfLen)
	for sf := 0; sf < SmplSubfrCount; sf++ {
		r := smplAnalysisResidualSubfr(&predcoefs[sf], winN, sf)
		copy(resLpc[sf*SmplSubfrLen:(sf+1)*SmplSubfrLen], r[:])
	}

	blockLags := cs.blockLags
	percCorrs := cs.percCorrs
	celpOut := runCelpSubframes(cs, &predcoefs, resLpc, &blockLags, percCorrs, SmplPercEmphV, 1)

	const main = 1
	pulseVec := make([]int32, SmplIntfLen)
	var acbg, fcbg [4]int32
	for sf := 0; sf < SmplSubfrCount; sf++ {
		out := &celpOut[sf]
		for _, v := range out.Pulses[main] {
			sign := int32(1) + 2*(int32(v)>>15)
			pos := int32(v)*sign - 1
			if pos >= 0 && pos < int32(SmplSubfrLen) {
				pulseVec[sf*SmplSubfrLen+int(pos)] += sign
			}
		}
		ai := int32(out.AcbIdx[main])
		if ai < 0 {
			ai = 0
		}
		if ai > 15 {
			ai = 15
		}
		acbg[sf] = ai
		fi := int32(out.GainIdx[main])
		if fi < 0 {
			fi = 0
		}
		fcbg[sf] = fi
	}
	ppPulses := smplBuildPulseParams(pulseVec)
	subfr := ppPulses.Subfr
	pp := vd.pp
	pp.GainIdx = acbg
	for sf := 0; sf < 4; sf++ {
		if subfr[sf] > 0 {
			pp.FiltIdx[sf] = fcbg[sf]
		} else {
			pp.FiltIdx[sf] = -1
		}
	}

	return candidate{
		ip: SmplInternalParams{
			Lsf:      SmplLsfParams{Stage1: 1, Grid: bgrid, Stage2: bsym, Extra: 0},
			Pulses:   ppPulses,
			HasPitch: true,
			Pitch:    pp,
		},
		stage1:   1,
		grid:     bgrid,
		qsym:     bsym,
		pulseVec: pulseVec,
		gainQ:    gainQ,
		pitch:    vd.pitch,
	}
}
