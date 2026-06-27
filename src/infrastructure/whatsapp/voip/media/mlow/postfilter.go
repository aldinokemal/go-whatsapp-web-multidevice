package mlow

import (
	"math"
	"sync"
)

// Postfilters: the excitation-domain harmonic comb (func 3524), the post-LPC HP
// pitch-harmonic comb, and the per-packet harmonic postfilter. Validated
// end-to-end and via the hp/harm postfilter raw vectors when implemented.

// --- excitation-domain harmonic comb (WASM func 3524) ---

// SmplPostfilterState is the persistent comb-postfilter state (pitch gain, env,
// biquad/de-emphasis/resonator FIR state, smoothed autocorrelation, init/count/LCG).
type SmplPostfilterState struct {
	EnvState float32
}

// SmplCombPostfilter computes the n-sample contribution the caller ADDS into the
// excitation.
func SmplCombPostfilter(st *SmplPostfilterState, input []float32, n int, active bool, gain8 float32, nrgEnv [2]float32, out []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_postfilter.rs#L249-L443
	// TODO
	// agent suggestion: port smpl_comb_postfilter — per-subframe autocorrelation →
	//   resonator, de-emphasis FIR, env-shaped noise (LCG) add; carries biquad state.
	// human input:
	panic("mlow: SmplCombPostfilter not yet implemented (scaffold)")
}

// --- post-LPC HP (pitch-harmonic) comb ---

var loEmph = [2]float32{1.0, -0.995}

const (
	hpPitchMAF             = float32(0.1)
	hpDefMAF               = float32(0.1)
	hpDefFcornerHz         = float32(50.0)
	lagChangeThreshold     = float32(1.25)
	hpPostfTransitionSpeed = float32(2.0)
)

var (
	hpPitchARF = [2]float32{0.608057355, 0.070939485}
	hpPitchARR = [2]float32{-2.187380512, 2.291030664}
	hpDefARF   = [2]float32{0.728508218, 0.476039848}
	hpDefARR   = [2]float32{-4.363803713, 8.441854006}
)

// HpPostfilterState is the post-LPC HP comb state (C HpPst). lagOld < 0 marks a
// fresh/reset filter.
type HpPostfilterState struct {
	stateLoEmph1 float32
	stateLoEmph2 float32
	stateHp      [4]float32 // [ma2 x[-1], x[-2], ar2 y[-1], y[-2]]
	lagOld       float32
	xOld         []float32
	coefMA       [3]float32
	coefAR       [3]float32
}

// NewHpPostfilterState allocates a fresh HP-postfilter state.
func NewHpPostfilterState() *HpPostfilterState {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_harmcomb.rs#L46-L58
	return &HpPostfilterState{lagOld: -1.0, xOld: make([]float32, SmplIntfLen)}
}

func cosApprox(x float32) float32 { return 1.0 - 0.5*x*x }

// SmplPfFir3 is the 3-tap FIR with carried 2-sample input history (smpl_filt_ma2 general).
func SmplPfFir3(input []float32, n int, coef [3]float32, state *[2]float32, out []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_harmcomb.rs#L68-L95
	xm1 := state[0]
	xm2 := state[1]
	for i := 0; i < n; i++ {
		var p1, p2 float32
		if i >= 1 {
			p1 = input[i-1]
		} else {
			p1 = xm1
		}
		if i >= 2 {
			p2 = input[i-2]
		} else if i == 1 {
			p2 = xm1
		} else {
			p2 = xm2
		}
		out[i] = coef[0]*input[i] + coef[1]*p1 + coef[2]*p2
	}
	if n >= 2 {
		state[0] = input[n-1]
		state[1] = input[n-2]
	} else if n == 1 {
		state[1] = xm1
		state[0] = input[0]
	}
}

// pfFiltAR2: y[n] = in[n] - c1*y[n-1] - c2*y[n-2] (monic), 4-wide unrolled to match C rounding.
func pfFiltAR2(input []float32, n int, c1, c2 float32, state *[2]float32, out []float32) {
	ytmp0 := state[1]
	ytmp1 := state[0]
	ar1 := -c1
	ar2 := -c2
	ar1_2 := ar1 * ar1
	ar1_3 := ar1 * ar1_2
	ar1_4 := ar1 * ar1_3
	imp1 := ar1
	imp2 := ar1_2 + ar2
	imp3 := ar1_3 + 2.0*ar1*ar2
	imp4 := ar1_4 + ar2*ar2 + 3.0*ar1_2*ar2
	ymp1 := ar2
	ymp2 := ar2 * imp1
	ymp3 := ar2 * imp2
	ymp4 := ar2 * imp3
	nn := 0
	for nn+3 < n {
		xtmp0 := input[nn]
		xtmp1 := input[nn+1]
		xtmp2 := input[nn+2]
		out[nn+2] = xtmp2 + imp1*xtmp1 + imp2*xtmp0 + imp3*ytmp1 + ymp3*ytmp0
		xtmp3 := input[nn+3]
		out[nn+3] = xtmp3 + imp1*xtmp2 + imp2*xtmp1 + imp3*xtmp0 + imp4*ytmp1 + ymp4*ytmp0
		out[nn] = xtmp0 + imp1*ytmp1 + ymp1*ytmp0
		out[nn+1] = xtmp1 + imp1*xtmp0 + imp2*ytmp1 + ymp2*ytmp0
		ytmp0 = out[nn+2]
		ytmp1 = out[nn+3]
		nn += 4
	}
	for nn < n {
		out[nn] = input[nn] + ar1*ytmp1 + ar2*ytmp0
		ytmp0 = ytmp1
		ytmp1 = out[nn]
		nn++
	}
	state[1] = ytmp0
	state[0] = ytmp1
}

// pfFiltAR1: leaky integrator y[n] = x[n] - c1*y[n-1], 5-wide unrolled to match C rounding.
func pfFiltAR1(input []float32, n int, c1 float32, state *float32, out []float32) {
	ar1 := -c1
	ar1_2 := ar1 * ar1
	ar1_3 := ar1 * ar1_2
	ar1_4 := ar1 * ar1_3
	ar1_5 := ar1 * ar1_4
	ytmp := *state
	nn := 0
	for nn+4 < n {
		xtmp0 := input[nn]
		xtmp1 := input[nn+1]
		xtmp2 := input[nn+2]
		xtmp3 := input[nn+3]
		xtmp4 := input[nn+4]
		out[nn+4] = xtmp4 + ar1*xtmp3 + ar1_2*xtmp2 + ar1_3*xtmp1 + ar1_4*xtmp0 + ar1_5*ytmp
		out[nn] = xtmp0 + ar1*ytmp
		out[nn+1] = xtmp1 + ar1*xtmp0 + ar1_2*ytmp
		out[nn+2] = xtmp2 + ar1*xtmp1 + ar1_2*xtmp0 + ar1_3*ytmp
		out[nn+3] = xtmp3 + ar1*xtmp2 + ar1_2*xtmp1 + ar1_3*xtmp0 + ar1_4*ytmp
		ytmp = out[nn+4]
		nn += 5
	}
	for nn < n {
		ytmp = input[nn] + ytmp*ar1
		out[nn] = ytmp
		nn++
	}
	*state = ytmp
}

// pfFiltMA1: y[n] = x[n] + c1*x[n-1] (companion pre-emphasis).
func pfFiltMA1(input []float32, n int, c1 float32, state *float32, out []float32) {
	prev := *state
	for i := n - 1; i >= 1; i-- {
		out[i] = input[i] + c1*input[i-1]
	}
	if n > 0 {
		out[0] = input[0] + c1*prev
		*state = input[n-1]
	}
}

// SmplGetHpCoefs returns the default fixed-corner ARMA2 biquad (coefMA, coefAR).
func SmplGetHpCoefs(fcornerHz float32) (coefMA, coefAR [3]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_harmcomb.rs#L188-L191
	fc := fcornerHz
	if fc < 5.0 {
		fc = 5.0
	}
	if fc > 1500.0 {
		fc = 1500.0
	}
	return smplCalcHPCoefs(hpDefMAF, hpDefARF, hpDefARR, fc/16000.0)
}

// SmplFiltArma2: MA2 numerator then AR2 denominator, shared 4-wide state.
func SmplFiltArma2(input []float32, n int, coefMA, coefAR [3]float32, state *[4]float32, out []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_harmcomb.rs#L194-L211
	tmp := make([]float32, n)
	maSt := [2]float32{state[0], state[1]}
	SmplPfFir3(input, n, coefMA, &maSt, tmp)
	state[0] = maSt[0]
	state[1] = maSt[1]
	arSt := [2]float32{state[2], state[3]}
	pfFiltAR2(tmp, n, coefAR[1], coefAR[2], &arSt, out)
	state[2] = arSt[0]
	state[3] = arSt[1]
}

// smplCalcHPCoefs builds the unity-DC comb biquad: AR resonance at the pitch angle
// 2*pi*arf*f with radius 1+arr*f, then MA scaled for unity DC gain.
func smplCalcHPCoefs(maf float32, arf, arr [2]float32, f float32) (coefMA, coefAR [3]float32) {
	coefMA = [3]float32{1.0, -2.0 * cosApprox(2.0*smplPiF32*maf*f), 1.0}
	far := arf[0]*f + arf[1]*f*f
	rar := arr[0]*f + arr[1]*f*f
	coefAR = [3]float32{
		1.0,
		-2.0 * cosApprox(2.0*smplPiF32*far) * (1.0 + rar),
		1.0 + (2.0*rar + rar*rar),
	}
	sc := (1.0 - coefAR[1] + coefAR[2]) / (1.0 - coefMA[1] + coefMA[2])
	coefMA[0] *= sc
	coefMA[1] *= sc
	coefMA[2] *= sc
	return coefMA, coefAR
}

// newCoefs: voiced pitch curve when lag>0 (f=1/lag), else the default 50 Hz curve.
func newCoefs(st *HpPostfilterState, lag float32) {
	if lag > 0.0 {
		st.coefMA, st.coefAR = smplCalcHPCoefs(hpPitchMAF, hpPitchARF, hpPitchARR, 1.0/lag)
	} else {
		fc := hpDefFcornerHz // already in [5,1500]
		st.coefMA, st.coefAR = smplCalcHPCoefs(hpDefMAF, hpDefARF, hpDefARR, fc/16000.0)
	}
}

// rampDn is the cos(omega)^2 down-ramp for the lag-change overlap-add.
func rampDn() []float32 {
	rampDnOnce.Do(func() {
		dOmega := smplPiF32 / (2.0 * (float32(SmplIntfLen) + 1.0))
		omega := dOmega
		rampDnTab = make([]float32, SmplIntfLen)
		for i := 0; i < SmplIntfLen; i++ {
			rampDnTab[i] = float32(math.Pow(float64(float32(math.Cos(float64(omega)))), float64(hpPostfTransitionSpeed)))
			omega += dOmega
		}
	})
	return rampDnTab
}

var (
	rampDnOnce sync.Once
	rampDnTab  []float32
)

// SmplHpPostfilter applies the post-LPC HP comb; lag is the frame's average pitch
// lag (sum(l^2)/sum(l)), 0 for unvoiced.
func SmplHpPostfilter(st *HpPostfilterState, xIn []float32, n int, lag float32, out []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_harmcomb.rs#L265-L314
	x := make([]float32, n)
	pfFiltAR1(xIn, n, loEmph[1], &st.stateLoEmph1, x)

	overlap := false
	yOld := make([]float32, n)
	if st.lagOld < 0.0 {
		newCoefs(st, lag)
		st.lagOld = lag
	} else if lag > lagChangeThreshold*st.lagOld || lagChangeThreshold*lag < st.lagOld {
		overlap = true
		SmplFiltArma2(x, n, st.coefMA, st.coefAR, &st.stateHp, yOld)
		newCoefs(st, lag)
		st.lagOld = lag
		xOld := append([]float32(nil), st.xOld...)
		dummy := make([]float32, n)
		SmplFiltArma2(xOld, n, st.coefMA, st.coefAR, &st.stateHp, dummy)
	} else if lag != st.lagOld {
		newCoefs(st, lag)
		st.lagOld = lag
	}
	copy(st.xOld[:n], x[:n])

	yTmp := make([]float32, n)
	SmplFiltArma2(x, n, st.coefMA, st.coefAR, &st.stateHp, yTmp)

	if overlap {
		ramp := rampDn()
		for i := 0; i < n; i++ {
			yTmp[i] += (yOld[i] - yTmp[i]) * ramp[i]
		}
	}

	pfFiltMA1(yTmp, n, loEmph[1], &st.stateLoEmph2, out)
}

// --- per-packet harmonic postfilter (smpl_harm_postfilter.c) ---

const (
	harmMaxFramesPerPacket = 6
	harmMinPitchLag        = 32
	harmMaxPitchLag        = 320
	harmMaxpitchLen        = 320
	harmFBDelay            = 8
	harmLagSubfrLen        = 40
	harmDelay              = 40 // = LAG_SUBFR_LEN
	harmPitchNumSubframes  = 8
	harmFBStrength         = float32(0.4734)
	harmStrength           = float32(0.6438)
	harmCutoffHz           = float32(4000.0)
	harmNHarmCutoff        = float32(6.3)
	harmReductionFac       = float32(0.0579)
	harmLPFiltRes          = 2500
	harmStateCombLen       = harmMaxpitchLen + SmplIntfLen*harmMaxFramesPerPacket + harmDelay
	harmNumLPFilt          = harmLPFiltRes/80 - harmLPFiltRes/harmMaxPitchLag + 1
)

func lagToFiltIx(lag int32) int {
	d := lag + 30
	if d < 80 {
		d = 80
	}
	return int(int32(harmLPFiltRes)/d - int32(harmLPFiltRes)/int32(harmMaxPitchLag))
}

type harmTablesT struct {
	lpFilters [][2*harmFBDelay + 1]float32
}

var (
	harmTablesOnce sync.Once
	harmTablesV    harmTablesT
)

func harmTables() *harmTablesT {
	harmTablesOnce.Do(func() {
		var filtWin [harmFBDelay]float32
		dOmega := (0.5 * smplPiF32) / (float32(harmFBDelay) + 1.0)
		omega := dOmega
		for i := 0; i < harmFBDelay; i++ {
			filtWin[i] = float32(math.Cos(float64(omega))) / (float32(i) + 1.0)
			omega += dOmega
		}
		harmTablesV.lpFilters = make([][2*harmFBDelay + 1]float32, harmNumLPFilt)
		ixPrev := int32(-1)
		for lag := int32(harmMinPitchLag); lag <= harmMaxPitchLag; lag++ {
			ix := int32(lagToFiltIx(lag))
			if ix != ixPrev {
				harmCreateLPFilter(2.0*smplPiF32/float32(lag), &filtWin, &harmTablesV.lpFilters[ix])
				ixPrev = ix
			}
		}
	})
	return &harmTablesV
}

func harmCreateLPFilter(omega0 float32, filtWin *[harmFBDelay]float32, blp *[2*harmFBDelay + 1]float32) {
	omegaC := omega0 * harmNHarmCutoff
	if lim := harmCutoffHz / 16000.0 * smplPiF32; lim < omegaC {
		omegaC = lim
	}
	var sumB float32
	omegaCSum := omegaC
	for i := 0; i < harmFBDelay; i++ {
		b := filtWin[i] * float32(math.Sin(float64(omegaCSum)))
		omegaCSum += omegaC
		blp[harmFBDelay+i+1] = b
		blp[harmFBDelay-i-1] = b
		sumB += 2.0 * b
	}
	blp[harmFBDelay] = omegaC
	sumB += omegaC
	sc := 1.0 / sumB
	for k := range blp {
		blp[k] *= sc
	}
}

// HarmPostfilterState is the per-packet harmonic postfilter state (C HarmPst).
type HarmPostfilterState struct {
	state1        [2 * harmFBDelay]float32
	lpcoefs       [2*harmFBDelay + 1]float32
	stateComb     []float32
	prevLag       int32
	prevDidFilter int32
}

// NewHarmPostfilterState allocates a fresh harmonic-postfilter state.
func NewHarmPostfilterState() *HarmPostfilterState {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_harm_postfilter.rs#L102-L112
	return &HarmPostfilterState{stateComb: make([]float32, harmStateCombLen)}
}

func harmDotProd(a, b []float32, l int) float32 {
	var r float32
	for i := 0; i < l; i++ {
		r += a[i] * b[i]
	}
	return r
}

func harmNrg(x []float32, n int) float32 {
	var r float32
	for i := 0; i < n; i++ {
		r += x[i] * x[i]
	}
	return r
}

// harmFiltMA16Sym: 17-tap symmetric MA reading 16 samples of history from buf[xBase-16..].
func harmFiltMA16Sym(buf []float32, xBase, n int, coef *[17]float32, out []float32) {
	for nn := 0; nn < n; nn++ {
		c := xBase + nn
		res := buf[c-8] * coef[8]
		for i := 0; i < 8; i++ {
			res += coef[i] * (buf[c-i] + buf[c-16+i])
		}
		out[nn] = res
	}
}

// harmPostfilterCore filters one 40-sample lag block (harm_postfilter_core).
func harmPostfilterCore(lpcoefs *[2*harmFBDelay + 1]float32, comb []float32, combX int, futureSamples int32, lag int32, diff []float32, diffBase int, out []float32, outOff, l int, fbStrength float32, prevDidFilter *int32) {
	tables := harmTables()
	lagU := int(lag)
	var xy float32
	if lag > 0 {
		lookforward := int32(l) + lag - futureSamples
		if lookforward > 0 {
			l2 := int(int32(l) - lookforward)
			if l2 < 0 {
				l2 = 0
			}
			for i := 0; i < l2; i++ {
				out[outOff+i] = comb[combX+i-lagU] + comb[combX+i+lagU]
			}
			for i := 0; i < l-l2; i++ {
				out[outOff+l2+i] = comb[combX+l2+i-lagU] + comb[combX+l2+i]
			}
		} else {
			for i := 0; i < l; i++ {
				out[outOff+i] = comb[combX+i-lagU] + comb[combX+i+lagU]
			}
		}
		xy = harmDotProd(comb[combX:], out[outOff:], l)
	}
	if lag > 0 && xy > 0.0 {
		xx := harmNrg(comb[combX:], l)
		yy := 0.25 * harmNrg(out[outOff:], l)
		denom := yy
		if xx > denom {
			denom = xx
		}
		strength := 0.5 * xy / denom
		highLagReduction := 1.0 - harmReductionFac*(float32(lag-harmMinPitchLag)/float32(harmMaxPitchLag-harmMinPitchLag))
		strength = strength * highLagReduction * harmStrength
		for i := 0; i < l; i++ {
			out[outOff+i] *= 0.5 * strength
		}
		for i := 0; i < l; i++ {
			diff[diffBase+i] = out[outOff+i] + (-strength)*comb[combX+i]
		}
		kernel := tables.lpFilters[lagToFiltIx(lag)]
		for k := 0; k < 2*harmFBDelay+1; k++ {
			lpcoefs[k] = kernel[k] * fbStrength
		}
		coef17 := *lpcoefs
		var yh [harmLagSubfrLen]float32
		harmFiltMA16Sym(diff, diffBase, l, &coef17, yh[:])
		for i := 0; i < l; i++ {
			out[outOff+i] = yh[i] + comb[combX-harmFBDelay+i]
		}
		*prevDidFilter = 1
	} else {
		for i := 0; i < harmLagSubfrLen; i++ {
			diff[diffBase+i] = 0.0
		}
		if *prevDidFilter != 0 {
			coef17 := *lpcoefs
			var yh [2 * harmFBDelay]float32
			harmFiltMA16Sym(diff, diffBase, 2*harmFBDelay, &coef17, yh[:])
			for i := 0; i < 2*harmFBDelay; i++ {
				out[outOff+i] = yh[i] + comb[combX-harmFBDelay+i]
			}
			for i := 2 * harmFBDelay; i < l; i++ {
				out[outOff+i] = comb[combX+harmFBDelay+i-2*harmFBDelay]
			}
		} else {
			for i := 0; i < l; i++ {
				out[outOff+i] = comb[combX-harmFBDelay+i]
			}
		}
		*prevDidFilter = 0
	}
}

// SmplHarmPostfilter applies the harmonic postfilter to a full packet IN PLACE. x is
// xLen samples; lags are the per-40-block lags (nLags = packetlen/40);
// normalizedBitrate is the packet average.
func SmplHarmPostfilter(st *HarmPostfilterState, x []float32, xLen int, lags []float32, nLags int, normalizedBitrate float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_harm_postfilter.rs#L242-L299
	const diffPrefix = 2 * harmFBDelay // 16 samples of history prefix
	diff := make([]float32, SmplIntfLen+diffPrefix)

	lag := st.prevLag
	combCur := harmMaxPitchLag + harmDelay // current packet starts here
	copy(st.stateComb[combCur:combCur+xLen], x[:xLen])

	fbStrength := 1.0 - harmFBStrength*normalizedBitrate
	offset1 := 0
	lagCtr := 0
	for lagCtr < nLags {
		offset2 := 0
		copy(diff[diffPrefix-16:diffPrefix], st.state1[:])
		lagCtrEnd := lagCtr + harmPitchNumSubframes
		if lagCtrEnd > nLags {
			lagCtrEnd = nLags
		}
		for lagCtr < lagCtrEnd {
			combX := harmMaxPitchLag + offset1
			futureSamples := int32(harmDelay) + int32(xLen) - int32(offset1)
			harmPostfilterCore(&st.lpcoefs, st.stateComb, combX, futureSamples, lag, diff, diffPrefix+offset2, x, offset1, harmLagSubfrLen, fbStrength, &st.prevDidFilter)
			offset1 += harmLagSubfrLen
			offset2 += harmLagSubfrLen
			lag = int32(math.Round(float64(lags[lagCtr])))
			lagCtr++
		}
		copy(st.state1[:], diff[diffPrefix+offset2-16:diffPrefix+offset2])
	}

	st.prevLag = lag
	copy(st.stateComb[0:combCur], st.stateComb[xLen:xLen+combCur])
}
