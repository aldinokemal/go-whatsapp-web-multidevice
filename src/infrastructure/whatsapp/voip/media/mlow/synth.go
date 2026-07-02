package mlow

import (
	"math"

	"github.com/rs/zerolog"
)

// Low-band synthesis: NLSF reconstruction, NLSF→LPC, gain linearization, LTP/ACB
// excitation prediction, and the per-internal-frame synthesis that turns decoded
// parameters into PCM. Validated end-to-end via the decoder module.

const (
	SmplOrder      = 16
	SmplSubfrLen   = 80  // 5 ms @ 16 kHz
	SmplIntfLen    = 320 // 20 ms internal frame
	SmplSubfrCount = 4
	SmplLtpHist    = 728
)

const (
	smplPiF32            = float32(3.1415927410125)
	smplNLSFWeightWMax   = float32(999.9999)
	smplNLSFWeightEps    = float32(0.0009999999)
	smplStabilizeMaxLoop = 1000
	smplStabilizeEps     = float32(9.5367431640625e-07)
)

const (
	gLTP             = float32(0.949999988079071)
	smplFracStateLen = 728
	ltpHistLen       = SmplLtpHist + SmplIntfLen + 64
)

// smplFIR16 is the 16-tap symmetric fractional-delay interpolation FIR (WASM mem
// 0xe8780, func 3523/3507).
var smplFIR16 = [16]float32{
	-0.000006392598606907995,
	0.00011064113641623408,
	-0.0009153038263320923,
	0.0048477197997272015,
	-0.018698347732424736,
	0.05759090930223465,
	-0.15997476875782013,
	0.617045521736145,
	0.6170454621315002,
	-0.15997475385665894,
	0.05759090557694435,
	-0.018698347732424736,
	0.0048477197997272015,
	-0.0009153038263320923,
	0.00011064114369219169,
	-0.0000063925981521606445,
}

// --- NLSF reconstruction / synthesis tables ---

// SmplSynthTables is the runtime synthesis table set (the smpl_synth_tables dump).
type SmplSynthTables struct {
	Valtables      [][][][][]float32 // [stage1][config][grid][coeff][sym]
	Centroids      [][][]float32     // [stage1][grid][16]
	Matrices       [][][][]float32   // [stage1][grid][row][col]
	MinSpacing     [][]float32       // [stage1][17]
	Grid16W        [][]float32
	Grid16Alpha    []float32
	Grid16Matrices [][][]float32 // [sig][config][256]
}

// LoadSmplSynthTables returns the runtime synthesis tables, built from the embedded
// seed ROM (lsf_seed.bin) and shared read-only.
func LoadSmplSynthTables() *SmplSynthTables {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L97-L104
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_synth.rs#L71-L73 (seed rewire: build from lsf_seed.bin)
	return loadLsfBuilt().synth
}

// smplNLSFLaroiaWeights: inverse-gap weights w[k] = invgap[k] + invgap[k+1] (silk_NLSF_VQ_weights_laroia).
func smplNLSFLaroiaWeights(nlsf, out []float32) {
	var inv [SmplOrder + 1]float32
	clamp := func(gap float32) float32 {
		if gap > smplNLSFWeightEps {
			return 1.0 / gap
		}
		return smplNLSFWeightWMax
	}
	inv[0] = clamp(nlsf[0])
	prev := nlsf[0]
	for k := 1; k < SmplOrder; k++ {
		inv[k] = clamp(nlsf[k] - prev)
		prev = nlsf[k]
	}
	inv[SmplOrder] = clamp(smplPiF32 - nlsf[SmplOrder-1])
	for k := 0; k < SmplOrder; k++ {
		out[k] = inv[k] + inv[k+1]
	}
}

// smplNLSFDecorr: out[r] = sum_c mat[c*16 + r] * vec[c] (column-major decorrelation matrix).
func smplNLSFDecorr(mat, vec, out []float32) {
	var scr [SmplOrder]float32
	v0 := vec[0]
	for r := 0; r < SmplOrder; r++ {
		scr[r] = v0 * mat[r]
	}
	for c := 1; c < SmplOrder; c++ {
		v := vec[c]
		base := c * SmplOrder
		for r := 0; r < SmplOrder; r++ {
			scr[r] += mat[base+r] * v
		}
	}
	copy(out[:SmplOrder], scr[:])
}

// smplStabilizeNLSF enforces minimum spacing + ordering in the margin domain (silk_NLSF_stabilize).
func smplStabilizeNLSF(nlsf, minSpacing []float32) {
	const L = SmplOrder
	var marg [L + 1]float32
	marg[0] = nlsf[0] - minSpacing[0]
	for i := 1; i < L; i++ {
		marg[i] = nlsf[i] - nlsf[i-1] - minSpacing[i]
	}
	marg[L] = smplPiF32 - nlsf[L-1] - minSpacing[L]
	argmin := func() (float32, int) {
		m := marg[0]
		idx := 0
		for i := 1; i < L+1; i++ {
			if marg[i] < m {
				m = marg[i]
				idx = i
			}
		}
		return m, idx
	}
	min, sel := argmin()
	loopN := 0
	for min < 0.0 {
		d := float32(loopN)*smplStabilizeEps - min
		if sel == 0 {
			marg[0] += d
			marg[1] -= d
		} else if sel == L {
			marg[L] += d
			marg[L-1] -= d
		} else {
			marg[sel] += d
			half := d * 0.5
			marg[sel-1] -= half
			marg[sel+1] -= half
		}
		m, s := argmin()
		min = m
		sel = s
		if min < 0.0 {
			loopN++
			if loopN == smplStabilizeMaxLoop {
				break
			}
		}
	}
	nlsf[0] = minSpacing[0] + marg[0]
	run := nlsf[0]
	for i := 1; i < L; i++ {
		run = run + marg[i] + minSpacing[i]
		nlsf[i] = run
	}
}

// SmplReconstructNLSF rebuilds the quantized NLSF from the stage indices and the
// previous frame's NLSF (the envelope the decoder synthesizes from).
func SmplReconstructNLSF(t *SmplSynthTables, stage1, config, grid int, stage2 *[16]int32, prevNLSF []float32) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L176-L234
	val := t.Valtables[stage1][config][grid]
	var resid [SmplOrder]float32
	for k := 0; k < SmplOrder; k++ {
		sym := stage2[k]
		if sym >= 0 && int(sym) < len(val[k]) {
			resid[k] = val[k][sym]
		}
	}

	out := make([]float32, SmplOrder)
	if grid == 16 {
		// grid==16: interpolate base between prevNLSF and the inverted grid16 base table.
		var base [SmplOrder]float32
		baseTbl := t.Grid16W[1-stage1]
		alpha := t.Grid16Alpha[stage1]
		for k := 0; k < SmplOrder; k++ {
			var pv float32
			if k < len(prevNLSF) {
				pv = prevNLSF[k]
			}
			base[k] = pv + alpha*(baseTbl[k]-pv)
		}
		var w [SmplOrder]float32
		smplNLSFLaroiaWeights(base[:], w[:])
		for i := range w {
			w[i] = float32(math.Sqrt(float64(w[i])))
		}
		var decorr [SmplOrder]float32
		smplNLSFDecorr(t.Grid16Matrices[stage1][config], resid[:], decorr[:])
		for k := 0; k < SmplOrder; k++ {
			out[k] = base[k] + decorr[k]/w[k]
		}
		smplStabilizeNLSF(out, t.MinSpacing[stage1])
		return out
	}

	// matrix case (grid < 16): NLSF[r] = 2*centroid[r] + sum_c mat[c][r]*resid[c].
	cent := t.Centroids[stage1][grid]
	mat := t.Matrices[stage1][grid]
	for r := 0; r < SmplOrder; r++ {
		acc := 2.0 * cent[r]
		for c := 0; c < SmplOrder; c++ {
			acc += mat[c][r] * resid[c]
		}
		out[r] = acc
	}
	smplStabilizeNLSF(out, t.MinSpacing[stage1])
	return out
}

// SmplNLSF2A converts NLSF to the monic LPC coefficient vector A[0..16] (a[0]=1).
func SmplNLSF2A(nlsf []float32) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L293-L311
	// Exercised end-to-end by TestEncodeRoundTripsATone (the encoder shadow-synth
	// path reconstructs a tone at correlation 0.89). Correlation-bounded, not
	// bit-exact — there is no isolated vector for this WASM-domain alt synth path.
	order := len(nlsf)
	half := order / 2
	cosv := make([]float64, order)
	for i, x := range nlsf {
		cosv[i] = math.Cos(float64(x))
	}
	p := make([]float64, half+1)
	q := make([]float64, half+1)
	smplNLSFPoly(p, cosv, half, 0)
	smplNLSFPoly(q, cosv, half, 1)

	a := make([]float32, order+1)
	a[0] = 1.0
	for k := 0; k < half; k++ {
		pt := p[k+1] + p[k]
		qt := q[k+1] - q[k]
		a[k+1] = float32(0.5 * (pt + qt))
		a[order-k] = float32(0.5 * (pt - qt))
	}
	return a
}

func smplNLSFPoly(out, cosv []float64, half, parity int) {
	out[0] = 1.0
	out[1] = -2.0 * cosv[parity]
	for k := 1; k < half; k++ {
		c := -2.0 * cosv[2*k+parity]
		out[k+1] = 2.0*out[k-1] + c*out[k]
		for n := k; n > 1; n-- {
			out[n] += out[n-2] + c*out[n-1]
		}
		out[1] += c
	}
}

// smplLPCSynthesis: out[n] = ex[n] - sum_{j=1..16} a[j]*out[n-j]; state holds the
// previous order outputs, carried across subframes/frames, updated in place.
func smplLPCSynthesis(ex, a, out, state []float32) {
	order := SmplOrder
	for n := 0; n < len(ex); n++ {
		acc := float64(ex[n])
		for j := 1; j <= order; j++ {
			var prev float64
			if n >= j {
				prev = float64(out[n-j])
			} else {
				prev = float64(state[order+n-j])
			}
			acc -= float64(a[j]) * prev
		}
		out[n] = float32(acc)
	}
	if len(out) >= order {
		copy(state[:order], out[len(out)-order:])
	}
}

// SmplGainLin maps the quantized log-gain to a linear gain (fast pow2 bit-cast).
func SmplGainLin(gainQ int32) float64 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L350-L362
	// Exercised end-to-end by TestEncodeRoundTripsATone (the encoder shadow-synth
	// path reconstructs a tone at correlation 0.89). Correlation-bounded, not
	// bit-exact — there is no isolated vector for this WASM-domain alt synth path.
	y := float32(gainQ)*6.103515625e-05*0.10000000149011612*27749388.0 + 1064866816.0
	var i int32
	if y < 2147483648.0 && y > -2147483648.0 {
		i = int32(y)
	} else {
		i = -2147483648
	}
	f := math.Float32frombits(uint32(i)) - 3.1622775509276835e-09
	if f < 0.0 {
		f = 0.0
	}
	return float64(f)
}

func smplFloorF32(x float32) float32 {
	i := int32(x)
	if float32(i) > x {
		i--
	}
	return float32(i)
}

// SmplLTPFracGain maps the normalized LTP gain to the fractional gain.
func SmplLTPFracGain(normGain float64) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L482-L484
	// Exercised end-to-end by TestEncodeRoundTripsATone (the encoder shadow-synth
	// path reconstructs a tone at correlation 0.89). Correlation-bounded, not
	// bit-exact — there is no isolated vector for this WASM-domain alt synth path.
	return float32(normGain)*-0.16999998688697815 + 0.3499999940395355
}

// smplFir8: 8-tap symmetric FIR16 application, in-place over sig (in==out overlap;
// f32 accumulation order matches the WASM).
func smplFir8(sig []float32, inBase, outBase, cnt int32) {
	for jj := int32(0); jj < cnt; jj++ {
		var acc float32
		for i := int32(0); i < 8; i++ {
			acc += (sig[inBase+jj+i] + sig[inBase+jj+15-i]) * smplFIR16[i]
		}
		sig[outBase+jj] = acc
	}
}

// smplFracLTP: fractional LTP + interpolation (func 3523). Reads sig backward from
// sigEnd, writes two regions per subframe into out (len 2*numSubfr*40); mutates sig.
func smplFracLTP(lag []float32, numSubfr int32, sig []float32, sigEnd, stateLen int32, out []float32) {
	lb := sigEnd - (40*numSubfr - stateLen)
	for sf := int32(0); sf < numSubfr; sf++ {
		fl := smplFloorF32(lag[sf])
		intLag := int32(fl)
		if float32(intLag) == lag[sf] {
			for k := int32(0); k < 40; k++ {
				sig[lb+k] = sig[lb+k-intLag]
			}
			for k := int32(0); k < 40; k++ {
				out[sf*40+k] = sig[lb+k]
				out[(numSubfr+sf)*40+k] = sig[lb+k-intLag-1] + sig[lb+k-intLag+1]
			}
		} else {
			b := (numSubfr + sf) * 40
			for k := int32(0); k < 40; k++ {
				out[b+k] = sig[lb-intLag-1+k] + sig[lb-intLag+1+k]
			}
			var l10 float32
			for j := int32(0); j < 16; j++ {
				l10 += sig[lb-9-intLag+j] * smplFIR16[j]
			}
			smplFir8(sig, lb-intLag-8, lb, 40)
			var l11 float32
			for j := int32(0); j < 16; j++ {
				l11 += sig[lb+32-intLag+j] * smplFIR16[j]
			}
			for k := int32(0); k < 40; k++ {
				out[sf*40+k] = sig[lb+k]
			}
			out[b] = l10 + sig[lb+1]
			for k := int32(0); k < 38; k++ {
				out[b+1+k] = sig[lb+k] + sig[lb+2+k]
			}
			out[b+39] = l11 + sig[lb+38]
		}
		lb += 40
	}
}

// smplExcGainApply: per-subframe LTP gain-apply (func 3522).
func smplExcGainApply(subLen int, input []float32, st *SmplExcGainState, out []float32, gain float32) {
	if gain != 0.0 {
		s5 := st.S1
		s6 := (s5 + s5) + st.S0
		d := st.S0 - s5
		absD := absF32(d)
		absS6 := absF32(s6)
		mn := absD + gain
		if absS6 < mn {
			mn = absS6
		}
		t := d * mn / (absD + 1e-12)
		st.S1 = (s6 - t) / 3.0
		st.S0 = (2.0*t + s6) / 3.0
	}
	if subLen == 0 {
		return
	}
	s0 := st.S0
	for n := 0; n < subLen; n++ {
		out[n] = s0 * input[n]
	}
	s1 := st.S1
	for n := 0; n < subLen; n++ {
		out[n] += s1 * input[subLen+n]
	}
}

// --- low-band synthesis (WASM func 3597 core) ---

// SmplExcGainState is the 2-tap excitation-gain smoother state.
type SmplExcGainState struct {
	S0 float32
	S1 float32
}

// SmplPitchSynth carries the per-internal-frame pitch synthesis inputs.
type SmplPitchSynth struct {
	Voiced   bool
	LagSubfr [4]float64
	NormGain float64
}

// SmplFrameSynth is the cross-internal-frame low-band synthesis state: LPC state and
// the LTP/excitation history plus the gain smoother. (The reference also carries
// Region-1 and HP postfilter state for paths gated off by SMPL_TAIL_REGION1 /
// SMPL_HP_POSTFILTER — those gated blocks are not ported here; they would need the
// postfilter module's state types.)
type SmplFrameSynth struct {
	lpcState [SmplOrder]float32
	ltpHist  []float32
	gst      SmplExcGainState
}

// NewSmplFrameSynth allocates a zeroed low-band synthesis state.
func NewSmplFrameSynth() *SmplFrameSynth {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L528-L538
	return &SmplFrameSynth{ltpHist: make([]float32, ltpHistLen)}
}

// SmplLTPSubframePred runs the fractional LTP prediction for one 80-sample subframe,
// writing predOut from the history at the fractional lag (func 3523 + func 3522).
func SmplLTPSubframePred(hist []float32, histPos int32, lagF, gainFrac float32, gst *SmplExcGainState, predOut []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L487-L506
	// Exercised end-to-end by TestEncodeRoundTripsATone (the encoder shadow-synth
	// path reconstructs a tone at correlation 0.89). Correlation-bounded, not
	// bit-exact — there is no isolated vector for this WASM-domain alt synth path.
	var fracOut [2 * 2 * 40]float32
	lags := []float32{lagF, lagF}
	smplFracLTP(lags, 2, hist, histPos-648, smplFracStateLen, fracOut[:])
	smplExcGainApply(SmplSubfrLen, fracOut[:], gst, predOut, gainFrac)
}

// SynthInternalFrame synthesizes one internal (20 ms) frame, returning the PCM
// signal and the reconstructed nlsf (which becomes the next frame's prevNLSF).
func SynthInternalFrame(
	t *SmplSynthTables,
	st *SmplFrameSynth,
	stage1, config, grid int,
	stage2 *[16]int32,
	prevNLSF []float32,
	pulses []int32,
	gainQ *[4]int32,
	pitch *SmplPitchSynth,
	log ...zerolog.Logger,
) (signal []float32, nlsf []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L543-L662
	lg := pickLog(log)
	lg.Trace().Int("stage1", stage1).Int("grid", grid).Int("pulses_len", len(pulses)).Int("prev_nlsf_len", len(prevNLSF)).Msg("synth internal frame")
	// Exercised end-to-end by TestEncodeRoundTripsATone (the encoder shadow-synth
	// path reconstructs a tone at correlation 0.89). Correlation-bounded, not
	// bit-exact — there is no isolated vector for this WASM-domain alt synth path.
	// The reference's Region-1 excitation comb and post-LPC HP postfilter are gated off
	// (SMPL_TAIL_REGION1 / SMPL_HP_POSTFILTER == false) and need the postfilter
	// module; those gated blocks are omitted here, matching the vector-capture config.
	nlsf = SmplReconstructNLSF(t, stage1, config, grid, stage2, prevNLSF)
	a := SmplNLSF2A(nlsf)

	subGain := func(sf int) float64 {
		gq := int32(0)
		if sf < len(gainQ) {
			gq = gainQ[sf]
		}
		return SmplGainLin(gq) * float64(SmplSubfrLen)
	}

	ex := make([]float32, SmplIntfLen)
	for n := 0; n < SmplIntfLen; n++ {
		ex[n] = float32(float64(pulses[n]) * subGain(n/SmplSubfrLen))
	}
	hist := st.ltpHist

	if pitch.Voiced {
		gainFrac := SmplLTPFracGain(pitch.NormGain)
		predOut := make([]float32, SmplSubfrLen)
		st.gst = SmplExcGainState{}
		for sf := 0; sf < SmplSubfrCount; sf++ {
			lagF := float32(pitch.LagSubfr[sf])
			intLag := int32(lagF)
			if intLag <= 0 {
				from := sf * SmplSubfrLen
				to := (sf + 1) * SmplSubfrLen
				copy(hist[SmplLtpHist+from:SmplLtpHist+to], ex[from:to])
				continue
			}
			exBase := sf * SmplSubfrLen
			histPos := int32(SmplLtpHist + exBase)
			if intLag > 0 && int(intLag) < SmplSubfrLen {
				for n := int(intLag); n < SmplSubfrLen; n++ {
					ex[exBase+n] += gLTP * ex[exBase+n-int(intLag)]
				}
			}
			SmplLTPSubframePred(hist, histPos, lagF, gainFrac, &st.gst, predOut)
			for n := 0; n < SmplSubfrLen; n++ {
				ex[exBase+n] += predOut[n]
			}
			copy(hist[int(histPos):int(histPos)+SmplSubfrLen], ex[exBase:exBase+SmplSubfrLen])
		}
	} else {
		copy(hist[SmplLtpHist:SmplLtpHist+SmplIntfLen], ex)
	}

	out := make([]float32, SmplIntfLen)
	smplLPCSynthesis(ex, a, out, st.lpcState[:])

	// roll the LTP history forward by one internal frame; clear the forward margin.
	copy(hist[0:], hist[SmplIntfLen:SmplLtpHist+SmplIntfLen])
	for i := SmplLtpHist + SmplIntfLen; i < ltpHistLen; i++ {
		hist[i] = 0.0
	}
	return out, nlsf
}

// (The C-float CELP synthesis — CelpDecParams / CelpDecState / SynthFrame — lives in
// celpdec.go.)

// --- unvoiced residual-energy quantizer (smpl_quant_nrg_res.c) ---

// NrgResQuant is the quantized residual-energy result; DbqQ14 is what the decoder
// reads as gainQ.
type NrgResQuant struct {
	FrameQi int32
	ShapeQi int32
	DbqQ14  [4]int32
}

const (
	smplResNrgBias      = float32(3.1622776e-9)
	smplResNrgMinDB     = float32(-85.0)
	smplResNrgMaxDB     = float32(0.0)
	smplNrgStepDBQ14_4  = int32(16686)
	smplResNrgShapeCBN4 = 98
)

// nrgresShapeCB4Q10 is nrgres_shape_CB_4_Q10 (98 vectors x 4 subframes), verbatim.
var nrgresShapeCB4Q10 = [smplResNrgShapeCBN4 * 4]int16{
	-2515, -2238, 2632, 2121, 790, 3973, -2872, -1891, -533, 2847, 1453, -3767, -6174, -402, 2668, 3908,
	-1623, -1458, 153, 2928, -1254, 3197, -476, -1467, 1803, -1086, 270, -987, 1952, -66, -1257, -629,
	161, 19, -85, -96, 4833, 3147, -105, -7875, -1320, 1377, -1156, 1099, 3398, -2247, 1485, -2637,
	-3031, 2756, 1841, -1566, -1487, 2202, -2668, 1954, 5518, -5344, 522, -696, 8400, -3123, -6235, 958,
	5152, -2444, -2811, 102, 2513, -82, 1181, -3612, -561, -197, -1074, 1832, -294, -1250, -1839, 3383,
	5126, 522, -782, -4866, -7760, -5178, -1840, 14779, -1119, 6007, -1489, -3399, -4567, -2543, 1855, 5255,
	53, -1626, 67, 1506, -12256, -7706, -1982, 21943, 3549, -969, -1096, -1484, -10824, 2981, 2204, 5639,
	-229, 1106, 945, -1821, -9237, 10157, 1616, -2537, 4916, -199, -2177, -2540, 6673, 984, -3355, -4302,
	-7130, -4677, 8925, 2882, 445, 2762, -348, -2859, -196, -1859, 1761, 294, 2725, -2093, -966, 334,
	-3908, -308, 3675, 541, 735, 890, -2516, 891, 504, 1631, -1157, -977, -17817, 2119, 7104, 8594,
	-2056, 1897, -198, 356, 292, -4544, -287, 4538, -1455, -304, 603, 1156, -18259, -12643, 15247, 15655,
	4177, 1778, -1815, -4140, 1425, 576, -294, -1707, -1301, 5132, 2838, -6669, -4727, -3148, -905, 8781,
	-650, 152, -4654, 5152, 13746, 2320, -6259, -9807, -1356, 396, 3789, -2829, 2337, 1947, -29, -4256,
	6033, 820, -5730, -1123, -1795, 1091, 1080, -377, 2208, -1921, -3314, 3027, 9688, 5218, -3754, -11152,
	3814, -3941, -6183, 6310, -1017, -2391, 4393, -984, 10944, -1182, -5011, -4751, -4640, 7201, -218, -2343,
	-1278, 4720, -4212, 770, 2777, 1333, -5944, 1833, -16066, 8107, 5165, 2795, 2530, -5020, 6073, -3582,
	-2111, -7534, 4575, 5070, -8702, -3762, 4050, 8414, 1335, -997, -1567, 1229, 9348, 1534, -3959, -6922,
	2440, 1153, -2175, -1418, -2715, -4538, -4478, 11730, 569, -885, 2032, -1716, 3529, -91, -3218, -219,
	2157, -4121, 191, 1772, -2123, -1968, -1355, 5446, 1475, -354, 3651, -4772, 1654, -3521, 2726, -859,
	2393, 6820, -2958, -6255, -3861, 1365, 1177, 1319, 7614, -1638, -2789, -3187, -3628, -2635, 6902, -639,
	1925, 2295, -1451, -2769, -3683, 4517, -981, 147, -1260, -529, 2339, -550, 3013, 639, -1050, -2602,
	3651, 1959, -3218, -2391, 6267, 3124, -2926, -6464, -8180, 3900, 4191, 89, -3372, -611, 1042, 2941,
	-2510, 856, -925, 2579, -11667, -8436, 10605, 9498, 6427, -2733, 1887, -5581, 1581, -1722, -328, 469,
	2011, 1989, -3606, -394, -1014, 2197, -1200, 17, 1544, -2555, 765, 247, 1188, -183, 1966, -2972,
	-6057, 3480, -2284, 4860, -25659, 8466, 8891, 8303,
}

// QuantNrgRes4 quantizes the 4-subframe residual-energy vector (smpl_quant_nrg_res, num_subfr==4).
func QuantNrgRes4(nrgres *[4]float32) NrgResQuant {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_nrgres.rs#L61-L101
	// Exercised by TestEncodeRoundTripsATone (the encoder unvoiced candidate quantizes
	// the per-subframe residual energy through this). Correlation-bounded e2e.
	var nrgresDB [4]float32
	var frameDB float32
	for i := 0; i < 4; i++ {
		v := 10.0 * float32(math.Log10(float64(nrgres[i]+smplResNrgBias)))
		if v > smplResNrgMaxDB {
			v = smplResNrgMaxDB
		}
		nrgresDB[i] = v
		frameDB += v
	}
	frameDB /= 4.0
	scQ14 := float32(1.0) / float32(int32(1)<<14)
	frameQi := int32(math.Round(float64((frameDB - smplResNrgMinDB) / (scQ14 * float32(smplNrgStepDBQ14_4)))))
	frameDbqQ14 := frameQi * smplNrgStepDBQ14_4
	frameDbqQ14 += int32(smplResNrgMinDB) * (1 << 14)
	for i := 0; i < 4; i++ {
		nrgresDB[i] -= float32(frameDbqQ14) * scQ14
	}
	scQ10 := float32(1.0) / float32(int32(1)<<10)
	bestRD := float32(1e30)
	qi := 0
	for n := 0; n < smplResNrgShapeCBN4; n++ {
		var rd float32
		for i := 0; i < 4; i++ {
			d := nrgresDB[i] - float32(nrgresShapeCB4Q10[n*4+i])*scQ10
			rd += d * d
		}
		if rd < bestRD {
			qi = n
			bestRD = rd
		}
	}
	var dbqQ14 [4]int32
	for i := 0; i < 4; i++ {
		dbqQ14[i] = frameDbqQ14 + int32(nrgresShapeCB4Q10[qi*4+i])*16
	}
	return NrgResQuant{FrameQi: frameQi, ShapeQi: int32(qi), DbqQ14: dbqQ14}
}
