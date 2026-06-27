package mlow

import "math"

// MLow perceptual-weighting front-end — faithful port of smpl_perc.rs
// (smpl_perc_wght.c FFT-based perceptual autocorrelation → perceptual LPC
// response, and smpl_bitrate_controller.c per-subframe pulse budget + importance).
// The C pffft (ordered real) is replaced by a self-contained mixed-radix complex
// FFT re-packed into pffft's exact ordered layout, so smth_filt indexes identical
// bins. PERCW_NFFT = 576 = 2^6 * 3^2 (not a power of two), hence mixed radix.
//
// Reuses smplPI (truncated literal), genSinWin/genCosWin, smplSigmoid from the
// package. Validated by perc-model smoke + bitrate-controller KATs and ultimately
// the encoder tone round-trip.

const (
	percwNfft    = 512 + 64 // 576
	percwFsKhz   = 16.0
	percMaskSmth = 0.1158
	percMelFcHz  = 320.0

	winNextWbLen     = 16 * 2 // 32
	winNextWbLongLen = 16 * 4 // 64
	win3ShortLen     = winNextWbLen
	win3LongLen      = winNextWbLongLen
	winPrevPercLen   = 16 * 12 // 192
	percWin110msLen  = 192
	percWin120msLen  = 352

	smplMaxLResp    = 32 + 1  // 33
	smplMaxSfLen    = 16 * 10 // 160
	smplPercRespLen = 16 * 2  // 32

	// SmplPercReg is the perceptual-LPC autocorrelation regularization (smpl_perc_wght.h).
	SmplPercReg float32 = 1e-3

	smplE float32 = 2.7182818284590

	// SmplFrameTypes
	frameBackgroundNoise = 0
	frameUnvoiced        = 1
	frameVoiced          = 2

	smplCelpIdxFec     = 0
	smplCelpIdxMain    = 1
	smplCelpMaxRates   = smplCelpIdxMain + 1 // 2
	smplMaxPulsesPerSf = 40
	smplRateContScale  = 26.0
)

// SmplPercEmphV / SmplPercEmphUV (smpl_tables.c): voiced / unvoiced pre-emphasis.
var (
	SmplPercEmphV  = [2]float32{-0.72, -0.77}
	SmplPercEmphUV = [2]float32{-0.55, -0.6}
)

// [lowRate][BACKGROUND_NOISE/UNVOICED/VOICED]
var smplMaxPulsesPerFrame = [2][3]uint8{{80, 160, 160}, {16, 32, 32}}

// [framelenidx][lowrate][8]
var smplRateControlModelComp5 = [4][2][8]float32{
	{
		{5.166876656946171, -8.981699804753452, 0.07280811614105594, 0.1301196310618402, -0.01597680442864421, 1.7601470147884113, -3.8161195433141755, 0.3038629198331684},
		{-71.71229978402292, 14.197572549553076, -0.9863630205846172, 0.032124893286072924, -0.0003538411576874928, 1.803705259861388e-11, 10.0, 1.2454667523627154},
	},
	{
		{32.5371190670542, -41.270234279452104, 10.490270829170875, -1.102121269442237, 0.03848319274046071, 3.405326741403831, -5.102658181889428, 0.2141935195026695},
		{-177.10486363500775, 43.952329593498376, -3.7049735533247454, 0.14239771116996938, -0.001919963993993193, 7.953695588409639e-6, 5.220317075476664, 0.6435364076926223},
	},
	{
		{-79.2663194911617, 45.00981883522089, -10.063311543498518, 1.2311531056576501, -0.06023559069137118, 0.059204788212259364, 3.033961466462233, 1.0111383197827808},
		{-122.04861900525415, 31.62096398905459, -2.613237037423586, 0.10050433143234094, -0.0013233009240188039, 2.14859438836692e-7, 1.9077791307787761, 0.7059420500333776},
	},
	{
		{-182.64255084224325, 122.90780796179816, -31.308790671748525, 3.7850563849431462, -0.1750480676903051, 0.05399618467364628, 3.009451055091342, 1.1243365512229038},
		{-132.4565456943888, 34.361297004632966, -2.7956546289118887, 0.10428149547078584, -0.001322667891395693, 2.678747426340249e-6, 6.9940208056381925, 0.7551244069345737},
	},
}

// [framelenidx][lowrate]
var smplRateControlThrsComp5 = [4][2]uint16{{7500, 10000}, {4500, 5750}, {4000, 5000}, {4000, 4750}}

// --- leaf vector helpers (smpl_codec_util.c) -------------------------------

func percMulVec(input, win, out []float32, l int) {
	for i := 0; i < l; i++ {
		out[i] = win[i] * input[i]
	}
}

func percScaleVec(x, y []float32, l int, g float32) {
	for i := 0; i < l; i++ {
		y[i] = x[i] * g
	}
}

func percAddScaleVec(x0, x1, y []float32, l int, g float32) {
	for i := 0; i < l; i++ {
		y[i] = x0[i] + g*x1[i]
	}
}

func percAddScaleVecInplace(x, y []float32, l int, g float32) {
	for i := 0; i < l; i++ {
		y[i] += g * x[i]
	}
}

// percFiltMa2 is smpl_filt_ma2: 2nd-order MA (may be non-monic). state is state[0..2].
func percFiltMa2(x []float32, n int, coef []float32, state *[2]float32, y []float32) {
	if coef[0] == 1.0 {
		percAddScaleVec(x[1:], x, y[1:], n-1, coef[1])
	} else {
		percScaleVec(x, y, n, coef[0])
		percAddScaleVecInplace(x, y[1:], n-1, coef[1])
	}
	percAddScaleVecInplace(x, y[2:], n-2, coef[2])
	y[0] = coef[0]*x[0] + coef[1]*state[0] + coef[2]*state[1]
	y[1] += coef[2] * state[0]
}

// percAc2rcDbl is smpl_ac2rc_dbl: autocorrelation → reflection coeffs (Levinson, f64).
func percAc2rcDbl(corr []float64, order int, reg float64, rc []float32) {
	c0 := make([]float64, order+1)
	c1 := make([]float64, order+1)
	copy(c0, corr[:order+1])
	c0[0] *= 1.0 + reg
	copy(c1, c0)
	for i := 0; i < order; i++ {
		rc[i] = 0.0
	}
	for k := 0; k < order; k++ {
		if c0[k+1] > c1[0] {
			rc[k] = -1.0
			break
		}
		if c0[k+1] < -c1[0] {
			rc[k] = 1.0
			break
		}
		if c1[0] == 0.0 {
			break
		}
		rcTmp := -c0[k+1] / c1[0]
		rc[k] = float32(rcTmp)
		for n := 0; n < order-k; n++ {
			ctmp1 := c0[n+k+1]
			ctmp2 := c1[n]
			c0[n+k+1] = ctmp1 + ctmp2*rcTmp
			c1[n] = ctmp2 + ctmp1*rcTmp
		}
	}
}

// percAc2rc is smpl_ac2rc: float wrapper promoting to double before Levinson.
func percAc2rc(corr []float32, order int, reg float32, rc []float32) {
	corrDbl := make([]float64, order+1)
	for i := 0; i < order+1; i++ {
		corrDbl[i] = float64(corr[i])
	}
	percAc2rcDbl(corrDbl, order, float64(reg), rc)
}

// percRc2a is smpl_rc2a: reflection coeffs → LPC polynomial A[0..order].
func percRc2a(rc []float32, order int, a []float32) {
	for v := 1; v <= order; v++ {
		a[v] = 0.0
	}
	a[0] = 1.0
	for k := 0; k < order; k++ {
		rcTmp := rc[k]
		for n := 0; n < (k+1)/2; n++ {
			tmp1 := a[n+1]
			tmp2 := a[k-n]
			a[n+1] = tmp1 + tmp2*rcTmp
			a[k-n] = tmp2 + tmp1*rcTmp
		}
		a[k+1] = rcTmp
	}
}

// --- inverse real FFT (forward + cfft live in fft.go) ----------------------

// rfftBackwardOrdered: inverse real FFT from the ordered REAL layout, unnormalized.
func rfftBackwardOrdered(f []float32, time []float32) {
	n := len(f)
	spec := make([]cpx, n)
	spec[0] = cpx{f[0], 0}
	spec[n/2] = cpx{f[1], 0}
	for i := 1; i < n/2; i++ {
		re := f[2*i]
		im := f[2*i+1]
		spec[i] = cpx{re, im}
		spec[n-i] = cpx{re, -im}
	}
	tout := make([]cpx, n)
	cfft(spec, tout, 1.0)
	for i := 0; i < n; i++ {
		time[i] = tout[i].re
	}
}

// --- perceptual model (smpl_perc_wght.c) -----------------------------------

type percWindows struct {
	percWin110ms []float32
	percWin120ms []float32
	win3Short    []float32
	win3Long     []float32
}

func newPercWindows() percWindows {
	return percWindows{
		percWin110ms: genSinWin(percWin110msLen),
		percWin120ms: genSinWin(percWin120msLen),
		win3Short:    genCosWin(win3ShortLen),
		win3Long:     genCosWin(win3LongLen),
	}
}

// smplWindowPerc is smpl_window for the perc case (use_lpc_win == FALSE).
func smplWindowPerc(win *percWindows, input, out []float32, length int, frameMs int32, useLongWin bool) {
	win1len := percWin120msLen
	win1 := win.percWin120ms
	if frameMs == 10 {
		win1len = percWin110msLen
		win1 = win.percWin110ms
	}
	win3len := win3ShortLen
	win3 := win.win3Short
	if useLongWin {
		win3len = win3LongLen
		win3 = win.win3Long
	}

	percMulVec(input, win1, out, win1len)
	mid := length - win1len - win3LongLen
	copy(out[win1len:win1len+mid], input[win1len:win1len+mid])
	percMulVec(input[length-win3LongLen:], win3, out[length-win3LongLen:], win3len)
	if !useLongWin {
		start := length - win3LongLen + win3ShortLen
		for i := start; i < length; i++ {
			out[i] = 0.0
		}
	}
}

// smthFilt is the bidirectional masking smooth across the power spectrum.
func smthFilt(f []float32, smthcoef []float32) {
	half := percwNfft / 2
	f2smth := f[0]
	for i := 1; i < half; i++ {
		f2new := f[2*i]
		f2smth = f2new + smthcoef[i]*(f2smth-f2new)
		f[2*i] = f2smth
	}
	f[1] = f[1] + smthcoef[half]*(f2smth-f[1])
	f2smth = f[1]
	for i := half - 1; i > 0; i-- {
		f2new := f[2*i]
		f2smth = f2new + smthcoef[i]*(f2smth-f2new)
		f[2*i] = f2smth
	}
	f[0] = f[0] + smthcoef[0]*(f2smth-f[0])
}

// PercModelState carries the buf history (PERCW_NFFT) across SmplPercModel calls.
type PercModelState struct {
	buf      [percwNfft]float32
	smthcoef []float32
	windows  percWindows
}

// NewPercModelState builds the per-bin mel-width smoothing coefficients (smpl_create_perc_model_tables).
func NewPercModelState() *PercModelState {
	fsStep := (percwFsKhz * 1000.0) / float32(percwNfft)
	smthcoef := make([]float32, percwNfft/2+1)
	for i := 0; i < percwNfft/2+1; i++ {
		percWidthPerBin := percMaskSmth * (fsStep*float32(i) + percMelFcHz) / fsStep
		smthcoef[i] = percWidthPerBin / (percWidthPerBin + 1.0)
	}
	return &PercModelState{smthcoef: smthcoef, windows: newPercWindows()}
}

// SmplPercModel: windowed power spectrum → bidirectional masking smooth → inverse →
// 1/NFFT scale. Returns the first lenR autocorrelation lags. buf advances as the C.
func SmplPercModel(state *PercModelState, xsubfr []float32, xsubfrLen int, frameMs int32, isLastSubfr int32, lenR int) []float32 {
	srcOff := xsubfrLen - (winNextWbLongLen - winNextWbLen)
	keep := percwNfft - xsubfrLen
	copy(state.buf[0:keep], state.buf[srcOff:srcOff+keep])
	copy(state.buf[keep:keep+xsubfrLen], xsubfr[:xsubfrLen])

	winlen := winPrevPercLen + int(frameMs)*16 + win3LongLen
	skipSamples := percwNfft - winlen

	bufWin := make([]float32, percwNfft)
	smplWindowPerc(&state.windows, state.buf[skipSamples:], bufWin[skipSamples:], winlen, frameMs, isLastSubfr == 0)

	f := make([]float32, percwNfft)
	rfftForwardOrdered(bufWin, f)
	f[0] = f[0] * f[0]
	f[1] = f[1] * f[1]
	for i := 1; i < percwNfft/2; i++ {
		f[2*i] = f[2*i]*f[2*i] + f[2*i+1]*f[2*i+1]
		f[2*i+1] = 0.0
	}
	smthFilt(f, state.smthcoef)
	rfftBackwardOrdered(f, bufWin)

	r := make([]float32, lenR)
	percScaleVec(bufWin, r, lenR, 1.0/float32(percwNfft))
	return r
}

// SmplPercAc2a: ma2 (b={pe, 1+pe^2, pe}) on R[1..] then Levinson + rc2a → A[0..percRespLen].
func SmplPercAc2a(r []float32, lenR int, percEmph float32, percRespLen int, reg float32) []float32 {
	b := []float32{percEmph, 1.0 + percEmph*percEmph, percEmph}
	state := [2]float32{r[0], r[1]}
	rTmp := make([]float32, smplMaxLResp)
	percFiltMa2(r[1:], percRespLen, b, &state, rTmp)

	rc := make([]float32, smplMaxLResp)
	percAc2rc(rTmp, percRespLen-1, reg, rc)

	a := make([]float32, percRespLen)
	percRc2a(rc, percRespLen-1, a)
	return a
}

// --- bitrate controller (smpl_bitrate_controller.c) ------------------------

func bitrate2pulses(rateKbps float32, coeff *[8]float32) float32 {
	return coeff[0] +
		coeff[1]*rateKbps +
		coeff[2]*rateKbps*rateKbps +
		coeff[3]*float32(math.Pow(float64(rateKbps), 3.0)) +
		coeff[4]*float32(math.Pow(float64(rateKbps), 4.0)) +
		coeff[5]*float32(math.Pow(float64(smplE), float64((rateKbps-coeff[6])*coeff[7])))
}

func bitrate2pulsesHrFec(rateKbps float32, coeff *[8]float32, onePulseRateBps float32) float32 {
	const rateThresKbps float32 = 9.0
	if rateKbps >= rateThresKbps {
		return bitrate2pulses(rateKbps, coeff)
	} else if onePulseRateBps >= rateThresKbps*1000.0 {
		return 1.0
	}
	pulsesThres := bitrate2pulses(rateThresKbps, coeff)
	sc := (rateThresKbps - rateKbps) / (rateThresKbps - onePulseRateBps/1000.0)
	return pulsesThres - sc*(pulsesThres-1.0)
}

// BitrateControllerInputs are the smpl_EncControlStruct fields the controller reads.
type BitrateControllerInputs struct {
	InternalSampleRate       int32
	PayloadSizeMs            int32
	FecBitRate               int32
	MainBitRate              int32
	Complexity               int32
	UseFecRateCompensation   int32
	UseDtx                   int32
	SubFrameImportanceFactor float32
}

// BitrateController state carried across frames.
type BitrateController struct {
	prevVoiced           int32
	rateContWnrgSmth     float32
	rateContBitrateScale [smplCelpMaxRates]float32
	bitrateDeltaSmth     [smplCelpMaxRates]float32
	rateContBitrate      [smplCelpMaxRates]float32
	adjustmentFactor     [smplCelpMaxRates]float32
}

// NewBitrateController is bitrate_controller_init + zeroed state.
func NewBitrateController() *BitrateController {
	bc := &BitrateController{}
	for i := range bc.adjustmentFactor {
		bc.adjustmentFactor[i] = 1.0
	}
	return bc
}

// control is bitrate_controller. Returns (max_pulses_per_subfr, subfr_importance).
func (bc *BitrateController) control(
	enc *BitrateControllerInputs,
	dtxSidFrame, codedAsActiveVoice int32,
	spActProb, nonflatness, voicingStrength float32,
	voiced int32,
	wnrg, wnrgNext float32,
	lowRate, framelen, subfrlen int32,
) ([smplCelpMaxRates]int16, [smplCelpMaxRates]float32) {
	var bweBitrate int32
	if enc.InternalSampleRate > 16000 {
		if lowRate != 0 {
			bweBitrate += 450
		} else {
			bweBitrate += 750
		}
		if enc.PayloadSizeMs == 10 {
			bweBitrate += 450
		}
	}

	bc.rateContWnrgSmth += 0.6 * (wnrg - bc.rateContWnrgSmth)

	framelenIdx := 3
	switch enc.PayloadSizeMs {
	case 10:
		framelenIdx = 0
	case 20:
		framelenIdx = 1
	case 60:
		framelenIdx = 2
	}

	var maxPulsesPerSubfr [smplCelpMaxRates]int16
	var subfrImportance [smplCelpMaxRates]float32

	startR := 0
	if (smplCelpIdxFec+boolToInt(enc.FecBitRate == 0)) != 0 || enc.FecBitRate == enc.MainBitRate {
		startR = 1
	}

	lrIdx := 1
	if lowRate != 0 {
		lrIdx = 0
	}

	for r := startR; r <= smplCelpIdxMain; r++ {
		bitRate := float32(enc.MainBitRate)
		if r == smplCelpIdxFec {
			bitRate = float32(enc.FecBitRate)
		}
		if bitRate > 30000.0 {
			bitRate = 30000.0
		}
		rateKbps := (bitRate - float32(bweBitrate)) / 1000.0
		if lowRate == 0 {
			switch enc.Complexity {
			case 1, 2:
				rateKbps *= 0.9900990
			case 3, 4:
				rateKbps *= 1.0101010
			}
		}

		var pulsesPer20msTargetMax float32
		rateControlThrs := float32(smplRateControlThrsComp5[framelenIdx][lrIdx])
		if (bitRate - float32(bweBitrate)) < rateControlThrs {
			pulsesPer20msTargetMax = 1.0
		} else {
			coeff := &smplRateControlModelComp5[framelenIdx][lrIdx]
			if r == smplCelpIdxFec && lowRate == 0 && enc.UseFecRateCompensation != 0 {
				pulsesPer20msTargetMax = maxF32(bitrate2pulsesHrFec(rateKbps, coeff, rateControlThrs), 1.0)
			} else {
				pulsesPer20msTargetMax = maxF32(bitrate2pulses(rateKbps, coeff), 1.0)
			}
		}

		relPulserate := pulsesPer20msTargetMax / 16.0 * (320.0 / float32(framelen))
		relPulserateLog := float32(math.Log(float64(relPulserate)))
		if bc.rateContBitrate[r] != bitRate {
			bitrateScale := float32(smplRateContScale) * relPulserate * (1.0 + 0.4*relPulserateLog*relPulserateLog)
			bc.rateContBitrateScale[r] = bitrateScale
			bc.rateContBitrate[r] = bitRate
		}

		numsubfrs := framelen / subfrlen
		mpps := 1 + int32(math.Round(float64(pulsesPer20msTargetMax*(1.0+0.5)/float32(numsubfrs))))
		if enc.UseDtx != 0 && dtxSidFrame != 0 {
			mpps = 0
		} else {
			mpps = int32(math.Round(float64(float32(mpps) * (0.5 + 0.5*float32(math.Sqrt(float64(spActProb+1e-12)))))))
			frameType := frameBackgroundNoise
			if codedAsActiveVoice != 0 {
				if voiced == 1 {
					frameType = frameVoiced
				} else {
					frameType = frameUnvoiced
				}
			}
			maxPulses := int32(smplMaxPulsesPerFrame[lowRate][frameType]) * framelen / 320
			if m := maxPulses / numsubfrs; mpps > m {
				mpps = m
			}
		}
		maxPulsesPerSubfr[r] = int16(mpps)

		imp := (wnrg + 0.01*wnrgNext) / (bc.rateContWnrgSmth + 0.02*wnrgNext + 1e-12)
		if voiced != 0 {
			if bitRate <= 9000.0 {
				imp = float32(math.Sqrt(float64(imp + 1e-12)))
			}
		} else {
			imp *= 0.9 + 0.3*smplSigmoid(nonflatness-2.0)
			imp *= 0.8
		}
		if voiced != bc.prevVoiced {
			imp *= 1.1
		}
		imp *= 0.9 + 0.3*1.0/(1.0+25.0*voicingStrength*voicingStrength)

		impFactor := enc.SubFrameImportanceFactor
		if impFactor <= 1.0 {
			imp *= (1.0 - impFactor) + impFactor*float32(math.Sqrt(float64(spActProb+1e-12)))
		} else if impFactor <= 2.0 {
			impFactor -= 1.0
			imp *= (1.0 - impFactor) + impFactor*spActProb
		} else {
			impFactor -= 2.0
			imp *= (1.0 - impFactor) + impFactor*spActProb*spActProb
		}
		imp *= bc.adjustmentFactor[r] * bc.rateContBitrateScale[r]
		subfrImportance[r] = imp
		bc.prevVoiced = voiced
	}

	return maxPulsesPerSubfr, subfrImportance
}

func boolToInt(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
