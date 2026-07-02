package mlow

import (
	"math"
)

// LSFCBCentroids is the number of stage-1 LSF codebook centroids. SmplLPCOrder
// (the 16-tap LPC order) is shared with lpc.go.
const LSFCBCentroids = 16

const lsfQstepCondMult = 0.9
const smplPi = float32(math.Pi)

// LsfQuantResult is one LSF quantization: Qi[0] (=grid), Qi[1..16] (=stage2), and
// the reconstructed quantized NLSF — the same envelope the decoder rebuilds.
type LsfQuantResult struct {
	Qi   [SmplLPCOrder + 1]int32
	QLsf [SmplLPCOrder]float32
}

// st1Tables is the per-codebook (voiced/unvoiced) stage-1 table set, mirroring the
// reference St1Json layout dumped from the C smpl_get_lsf_CBks().
type st1Tables struct {
	Cbhalf   [][]float32   // [16][16]
	CInv     [][]float32   // [16][16]
	BitsCond []float32     // [17]
	Rotcond  [][][]float32 // [2][16][16]
	CbCinv   [][]float32   // [16][16]
	We       [][][]float32 // [16][16][16]
	Bits     []float32     // [16]
	Wie      [][][]float32 // [16][16][16]
}

// st2Tables is one stage-2 table set (per voiced/lowRate/qi1); the per-coeff Qlvls
// and NumBits rows are ragged, so they stay slices.
type st2Tables struct {
	NumQlvls []int32
	Qlvls    [][]float32 // [16][numQlvls[i]]
	NumBits  [][]float32 // [16][numQlvls[i]]
}

// LsfCb holds the loaded LSF codebook tables (the C smpl_get_lsf_CBks() output plus
// the static smpl_lsf_tables.c constants).
type LsfCb struct {
	St1       []st1Tables     // [2]
	St2       [][][]st2Tables // [2][2][17]
	MinQi     [][][][]int32   // [2][2][17][16]
	MaxQi     [][][][]int32   // [2][2][17][16]
	Qstep     [][]float32     // [2][2]
	MeanV     []float32       // [16]
	MeanUV    []float32       // [16]
	RegCond   []float32       // [2]
	MinDistV  []float32       // [17]
	MinDistUV []float32       // [17]
}

// LoadLsfCb returns the LSF quantizer codebook, built from the embedded seed ROM
// (lsf_seed.bin) and shared read-only.
func LoadLsfCb() *LsfCb {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_lsf_quant.rs#L79-L85
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_quant.rs#L117-L119 (seed rewire: build from lsf_seed.bin)
	return loadLsfBuilt().cb
}

// ----- f32 scalar helpers (single-precision throughout: qi[] is decided by f32 comparisons) -----

func cosF32(x float32) float32   { return float32(math.Cos(float64(x))) }
func sinF32(x float32) float32   { return float32(math.Sin(float64(x))) }
func sqrtF32(x float32) float32  { return float32(math.Sqrt(float64(x))) }
func log2F32(x float32) float32  { return float32(math.Log2(float64(x))) }
func roundF32(x float32) float32 { return float32(math.Round(float64(x))) }

func absF32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func smplSign(a float32) int32 {
	if a > 0 {
		return 1
	}
	if a == 0 {
		return 0
	}
	return -1
}

// ----- vector helpers (faithful ports) -----

func subVec(y, z, out []float32) {
	for i := 0; i < SmplLPCOrder; i++ {
		out[i] = y[i] - z[i]
	}
}

func dotProd(a, b []float32) float32 {
	var s float32
	for i := 0; i < SmplLPCOrder; i++ {
		s += a[i] * b[i]
	}
	return s
}

func werr(x, y, w []float32) float32 {
	var s float32
	for k := 0; k < SmplLPCOrder; k++ {
		e := x[k] - y[k]
		s += w[k] * e * e
	}
	return s
}

// matrixMultTransp16: y[i] = sum_j c[j][i]*x[j].
func matrixMultTransp16(c [][]float32, x, y []float32, lenX int) {
	var yt [SmplLPCOrder]float32
	xtmp := x[0]
	for i := 0; i < SmplLPCOrder; i++ {
		yt[i] = c[0][i] * xtmp
	}
	for j := 1; j < lenX; j++ {
		xtmp := x[j]
		for i := 0; i < SmplLPCOrder; i++ {
			yt[i] += c[j][i] * xtmp
		}
	}
	copy(y[:SmplLPCOrder], yt[:])
}

// getMaxiK: top-K indices of the K largest values in x (descending), ties toward the lower index.
func getMaxiK(x []float32, idx []int32, k int) {
	n := len(x)
	used := make([]bool, n)
	for slot := 0; slot < k; slot++ {
		bestI := int32(-1)
		bestV := float32(math.Inf(-1))
		for i := 0; i < n; i++ {
			if used[i] {
				continue
			}
			if x[i] > bestV {
				bestV = x[i]
				bestI = int32(i)
			}
		}
		if bestI < 0 {
			idx[slot] = 0
		} else {
			used[bestI] = true
			idx[slot] = bestI
		}
	}
}

// lsfWeightsSpectral: RD weight = inverse spectral envelope magnitude 1/sqrt(|A(e^jw)|^2 * scale),
// scale = 1/min. a is the monic LPC A[0..16] (A[0]=1).
func lsfWeightsSpectral(a, lsf []float32) [SmplLPCOrder]float32 {
	var lsfw [SmplLPCOrder]float32
	for i := 0; i < SmplLPCOrder; i++ {
		eRe := cosF32(lsf[i])
		eIm := sinF32(lsf[i])
		accRe := float32(1.0)
		accIm := float32(0.0)
		epRe := eRe
		epIm := eIm
		for j := 1; j < SmplLPCOrder; j++ {
			accRe += epRe * a[j]
			accIm -= epIm * a[j]
			nr := epRe*eRe - epIm*eIm
			ni := epRe*eIm + epIm*eRe
			epRe = nr
			epIm = ni
		}
		accRe += epRe * a[SmplLPCOrder]
		accIm -= epIm * a[SmplLPCOrder]
		lsfw[i] = accRe*accRe + accIm*accIm
	}
	minLsfw := lsfw[0]
	for _, v := range lsfw[1:] {
		if v < minLsfw {
			minLsfw = v
		}
	}
	scale := 1.0 / minLsfw
	for i := range lsfw {
		lsfw[i] = 1.0 / sqrtF32(lsfw[i]*scale)
	}
	return lsfw
}

// LsfWeightsLaroia is the Laroia LSF weighting (inverse adjacent-spacing sum), used by the
// conditional path's rotation weighting.
func LsfWeightsLaroia(lsf []float32) [SmplLPCOrder]float32 {
	minDist := float32(1e-3)
	var invDelta [SmplLPCOrder + 1]float32
	invDelta[0] = 1.0 / maxF32(lsf[0], minDist)
	for i := 1; i < SmplLPCOrder; i++ {
		invDelta[i] = 1.0 / maxF32(lsf[i]-lsf[i-1], minDist)
	}
	invDelta[SmplLPCOrder] = 1.0 / maxF32(smplPi-lsf[SmplLPCOrder-1], minDist)
	var lsfw [SmplLPCOrder]float32
	for i := 0; i < SmplLPCOrder; i++ {
		lsfw[i] = invDelta[i] + invDelta[i+1]
	}
	return lsfw
}

// lsfMinDist pushes LSFs apart so consecutive spacings exceed min_dist (SMPL_lsf_min_dist).
func lsfMinDist(lsfs, minDist []float32) {
	n := SmplLPCOrder
	var dlsfs [SmplLPCOrder + 1]float32
	dlsfs[0] = lsfs[0] - minDist[0]
	for i := 1; i < n; i++ {
		dlsfs[i] = (lsfs[i] - lsfs[i-1]) - minDist[i]
	}
	dlsfs[n] = (smplPi - lsfs[n-1]) - minDist[n]
	findMin := func(d []float32) (float32, int) {
		m := d[0]
		mi := 0
		for i := 1; i < n+1; i++ {
			if d[i] < m {
				m = d[i]
				mi = i
			}
		}
		return m, mi
	}
	dm, minIx := findMin(dlsfs[:])
	if dm > 0.0 {
		return
	}
	for k := 0; k < 1000; k++ {
		delta := float32(k)*1.0e-6 - dm
		dlsfs[minIx] += delta
		if minIx == 0 {
			dlsfs[1] -= delta
		} else if minIx == n {
			dlsfs[n-1] -= delta
		} else {
			delta *= 0.5
			dlsfs[minIx-1] -= delta
			dlsfs[minIx+1] -= delta
		}
		ndm, nmi := findMin(dlsfs[:])
		dm = ndm
		minIx = nmi
		if dm >= 0.0 {
			lsfs[0] = dlsfs[0] + minDist[0]
			for i := 1; i < n; i++ {
				lsfs[i] = lsfs[i-1] + (dlsfs[i] + minDist[i])
			}
			return
		}
	}
	// C asserts here; we fall through with the best-effort spacing (do not panic).
}

// condParams is the VQ_temp cond centroid (built from the previous frame's quantized NLSF).
type condParams struct {
	st1Cbhalf [SmplLPCOrder]float32
	st1CbCinv [SmplLPCOrder]float32
	st1We     [][]float32 // [16][16]
	st1Wie    [][]float32 // [16][16]
}

// vqTemp: Mahalanobis shortlist of `surv` stage-1 centroids (plus the cond centroid when present).
func vqTemp(lsf []float32, cbhalf, cbCinv [][]float32, cond *condParams, surv int, idxs []int32) {
	var err [LSFCBCentroids + 1]float32
	var tmp [SmplLPCOrder]float32
	for s := 0; s < LSFCBCentroids; s++ {
		subVec(cbhalf[s], lsf, tmp[:])
		err[s] = -dotProd(tmp[:], cbCinv[s])
	}
	cbCentroids := LSFCBCentroids
	if cond != nil {
		subVec(cond.st1Cbhalf[:], lsf, tmp[:])
		err[LSFCBCentroids] = -dotProd(tmp[:], cond.st1CbCinv[:])
		cbCentroids++
	}
	getMaxiK(err[:cbCentroids], idxs, surv)
}

// lsfQuantCore is the faithful port of smpl_lsf_quant_core.
func lsfQuantCore(cb *LsfCb, a, nlsf []float32, voiced, lowRate int, cond *condParams, rdWAdj float32, surv int) LsfQuantResult {
	st1 := &cb.St1[voiced]
	st2v := cb.St2[voiced][lowRate]
	minQi := cb.MinQi[voiced][lowRate]
	maxQi := cb.MaxQi[voiced][lowRate]
	minDist := cb.MinDistUV
	if voiced == 1 {
		minDist = cb.MinDistV
	}

	var lsf [SmplLPCOrder]float32
	copy(lsf[:], nlsf[:SmplLPCOrder])
	wlsf := lsfWeightsSpectral(a, lsf[:])

	qstep := cb.Qstep[voiced][lowRate]
	qstepCond := qstep * lsfQstepCondMult

	var qim1 [LSFCBCentroids + 1]int32
	vqTemp(lsf[:], st1.Cbhalf, st1.CbCinv, cond, surv, qim1[:])

	rdBest := float32(math.MaxFloat32)
	var outQi [SmplLPCOrder + 1]int32
	var outQlsf [SmplLPCOrder]float32

	for s1 := 0; s1 < surv; s1++ {
		qi1 := int(qim1[s1])
		isCond := qi1 == LSFCBCentroids

		// lsfq1 = 2 * cbhalf[qi1] (or cond centroid).
		var lsfq1 [SmplLPCOrder]float32
		if isCond {
			for i := 0; i < SmplLPCOrder; i++ {
				lsfq1[i] = cond.st1Cbhalf[i] * 2.0
			}
		} else {
			for i := 0; i < SmplLPCOrder; i++ {
				lsfq1[i] = st1.Cbhalf[qi1][i] * 2.0
			}
		}

		// qerr = wie^T * (lsf - lsfq1).
		var qerrIn [SmplLPCOrder]float32
		subVec(lsf[:], lsfq1[:], qerrIn[:])
		var wiePtr [][]float32
		if isCond {
			wiePtr = cond.st1Wie
		} else {
			wiePtr = st1.Wie[qi1]
		}
		var qerr [SmplLPCOrder]float32
		matrixMultTransp16(wiePtr, qerrIn[:], qerr[:], SmplLPCOrder)

		invQstep := 1.0 / qstep
		if isCond {
			invQstep = 1.0 / qstepCond
		}
		for i := range qerr {
			qerr[i] *= invQstep
		}

		var bits float32
		if cond == nil {
			bits = st1.Bits[qi1]
		} else {
			bits = st1.BitsCond[qi1]
		}

		var alt [SmplLPCOrder]int32
		var absQerr [SmplLPCOrder]float32
		var qres [SmplLPCOrder]float32
		var qi2 [SmplLPCOrder]int32
		st2 := &st2v[qi1]
		for i := 0; i < SmplLPCOrder; i++ {
			qi2i := int32(roundF32(qerr[i]))
			mn := minQi[qi1][i]
			mx := maxQi[qi1][i]
			if qi2i > mx {
				qi2i = mx
			}
			if qi2i < mn {
				qi2i = mn
			}
			qerr[i] -= float32(qi2i)
			alt[i] = smplSign(qerr[i])
			if (qi2i == mx && alt[i] > 0) || (qi2i == mn && alt[i] < 0) {
				absQerr[i] = -1.0
			} else {
				absQerr[i] = absF32(qerr[i])
			}
			qi2i -= mn
			qi2u := int(qi2i)
			bits += st2.NumBits[i][qi2u]
			qres[i] = st2.Qlvls[i][qi2u]
			qi2[i] = qi2i
		}

		var iAlt [SmplLPCOrder]int32
		getMaxiK(absQerr[:], iAlt[:], surv)

		var wePtr [][]float32
		if isCond {
			wePtr = cond.st1We
		} else {
			wePtr = st1.We[qi1]
		}
		var lsfq [SmplLPCOrder]float32
		matrixMultTransp16(wePtr, qres[:], lsfq[:], SmplLPCOrder)
		for i := 0; i < SmplLPCOrder; i++ {
			lsfq[i] += lsfq1[i]
		}

		surv2 := surv - s1
		indChgd := int32(-1)
		bitsOrig := bits
		// Beam base is FIXED to the initial lsfq (C memcpy BEFORE the loop); each refinement flips
		// ONE coeff relative to this base, undoing the previous flip.
		lsfqBase := lsfq
		curBits := bits
		for s2 := 0; s2 < surv2; s2++ {
			lsfMinDist(lsfq[:], minDist)
			w := werr(lsf[:], lsfq[:], wlsf[:])
			rd := 0.5*float32(SmplLPCOrder)*log2F32(w)*rdWAdj + curBits
			if rd < rdBest {
				rdBest = rd
				outQi[0] = int32(qi1)
				copy(outQi[1:SmplLPCOrder+1], qi2[:])
				copy(outQlsf[:], lsfq[:])
			}
			if s2 == surv2-1 || absQerr[iAlt[s2]] < 0.25 {
				break
			}
			if s2 > 0 {
				ic := int(indChgd)
				qi2[ic] -= alt[ic]
			}
			indChgd = iAlt[s2]
			ic := int(indChgd)
			qi2Old := qi2[ic]
			qi2[ic] += alt[ic]
			qi2New := qi2[ic]
			qlvlsDiff := st2.Qlvls[ic][qi2New] - st2.Qlvls[ic][qi2Old]
			for i := 0; i < SmplLPCOrder; i++ {
				lsfq[i] = lsfqBase[i] + qlvlsDiff*wePtr[ic][i]
			}
			curBits = bitsOrig + st2.NumBits[ic][qi2New] - st2.NumBits[ic][qi2Old]
		}
	}

	return LsfQuantResult{Qi: outQi, QLsf: outQlsf}
}

// LsfQuant is the non-conditional LSF quantization (smpl_lsf_quant). a is the monic LPC A[0..16].
func LsfQuant(a, nlsf []float32, voiced, lowRate int, rdWAdj float32, surv int) LsfQuantResult {
	cb := LoadLsfCb()
	return lsfQuantCore(cb, a, nlsf, voiced, lowRate, nil, rdWAdj, surv)
}

// LsfQuantCond is the conditional LSF quantization given the previous frame's quantized NLSF
// (smpl_lsf_quant_cond). a is the monic LPC A[0..16].
func LsfQuantCond(a, nlsf, lsfqPrev []float32, voiced, lowRate int, rdWAdj float32, surv int) LsfQuantResult {
	cb := LoadLsfCb()
	st1 := &cb.St1[voiced]
	cbMean := cb.MeanUV
	if voiced == 1 {
		cbMean = cb.MeanV
	}
	reg := cb.RegCond[voiced]
	var lsfqPrevReg [SmplLPCOrder]float32
	var st1Cbhalf [SmplLPCOrder]float32
	for i := 0; i < SmplLPCOrder; i++ {
		lsfqPrevReg[i] = lsfqPrev[i] + reg*(cbMean[i]-lsfqPrev[i])
		st1Cbhalf[i] = 0.5 * lsfqPrevReg[i]
	}
	var st1CbCinv [SmplLPCOrder]float32
	matrixMultTransp16(st1.CInv, lsfqPrevReg[:], st1CbCinv[:], SmplLPCOrder)
	we, wie := rotApplyWght(st1.Rotcond[lowRate], lsfqPrevReg[:])
	cond := &condParams{
		st1Cbhalf: st1Cbhalf,
		st1CbCinv: st1CbCinv,
		st1We:     we,
		st1Wie:    wie,
	}
	return lsfQuantCore(cb, a, nlsf, voiced, lowRate, cond, rdWAdj, surv)
}

// rotApplyWght builds wrot1 (=we) and wrot2 (=wie) for the cond centroid from the rotation matrix
// and the Laroia-weighted previous LSF (smpl_rot_apply_wght).
func rotApplyWght(rot [][]float32, lsf []float32) (we, wie [][]float32) {
	lsfw := LsfWeightsLaroia(lsf)
	for i := range lsfw {
		lsfw[i] = sqrtF32(lsfw[i])
	}
	var lsfwInv [SmplLPCOrder]float32
	for i := 0; i < SmplLPCOrder; i++ {
		lsfwInv[i] = 1.0 / lsfw[i]
	}
	wrot1 := make([][]float32, SmplLPCOrder)
	wrot2 := make([][]float32, SmplLPCOrder)
	for i := 0; i < SmplLPCOrder; i++ {
		wrot1[i] = make([]float32, SmplLPCOrder)
		wrot2[i] = make([]float32, SmplLPCOrder)
	}
	for i := 0; i < SmplLPCOrder; i++ {
		for j := 0; j < SmplLPCOrder; j++ {
			wrot1[i][j] = rot[i][j] * lsfwInv[j]
			wrot2[j][i] = rot[i][j] * lsfw[j]
		}
	}
	return wrot1, wrot2
}
