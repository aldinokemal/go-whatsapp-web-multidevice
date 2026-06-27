package mlow

import (
	"math"
	"sync"
)

// MLow encoder-side CELP excitation — faithful port of smpl_celp.rs (Meta's
// smpl_celp_enc.c). Per subframe: build the perceptually-weighted impulse response,
// run the ACB/LTP search (voiced) and the FCB pulse search (greedy or delayed-
// decision beam), quantize the gains, and return the chosen pulses + indices + the
// reconstructed LPC excitation. The encode-side counterpart of mlow/synth's CELP.
//
// Datasheet: datasheets/mlow-celp.md. Reuses cbAcbgains{HR,LR}Q14, acbgN/acbgM,
// SmplLPCOrder, and the perc constants. No isolated byte-exact vector; validated by
// the encode_*_runs smoke tests and the end-to-end encoder tone round-trip.

// celpMaxPitchLag, fcbgVN, uvGainIdxLen, acbgN/acbgM, celpInterpolKernel,
// cbAcbgains{HR,LR}Q14 are defined in celpdec.go and reused here.
const (
	celpLtpInterpolDelay = 8
	celpLagSubfrlen      = 40
	celpMaxpitchLen      = 320

	celpGAcbRdMu float32 = 0.014999999664723873
	fcbgVDeltaN          = 67

	vGainMinDb   float32 = -100.0
	vGainMaxDb   float32 = 0.0
	vGainStepDb  float32 = 3.0
	uvGainMinDb  float32 = -90.0
	uvGainMaxDb  float32 = 0.0
	uvGainStepDb float32 = 1.0

	rateAcbScale        float32 = 0.9
	pitchSharpeningCoef float32 = 0.9881
	fcbSrvMax                   = 4
	celpMaxNumsurv              = 8
	nGainSteps                  = 2
)

var celpAcbgainsDcmfLR = [(acbgN + 1) * acbgN]uint8{
	103, 70, 48, 3, 122, 135, 47, 192, 2, 255, 99, 96, 186, 194, 4, 28,
	161, 90, 76, 3, 181, 60, 37, 219, 2, 132, 81, 146, 255, 43, 3, 36,
	114, 222, 55, 6, 203, 34, 42, 154, 6, 255, 33, 209, 225, 78, 6, 45,
	198, 161, 110, 8, 239, 26, 35, 162, 4, 117, 42, 214, 255, 33, 6, 72,
	55, 255, 124, 55, 124, 55, 55, 55, 55, 78, 55, 215, 111, 55, 55, 167,
	154, 136, 77, 4, 220, 33, 38, 166, 2, 144, 50, 196, 255, 43, 4, 41,
	56, 21, 19, 3, 48, 255, 38, 220, 2, 225, 107, 31, 122, 227, 2, 11,
	63, 38, 23, 4, 77, 85, 58, 190, 4, 255, 53, 53, 145, 138, 4, 14,
	95, 47, 33, 2, 110, 146, 53, 255, 2, 219, 79, 73, 198, 122, 2, 15,
	84, 255, 84, 84, 147, 84, 84, 84, 84, 120, 84, 120, 84, 84, 84, 84,
	73, 58, 25, 1, 95, 99, 52, 175, 1, 255, 48, 69, 151, 184, 1, 15,
	105, 32, 43, 2, 84, 225, 34, 255, 2, 156, 129, 49, 189, 124, 3, 19,
	152, 230, 89, 6, 253, 28, 40, 153, 2, 195, 31, 255, 249, 58, 5, 61,
	138, 84, 54, 3, 173, 96, 45, 247, 2, 176, 83, 128, 255, 69, 2, 26,
	22, 17, 8, 1, 23, 106, 26, 88, 1, 182, 37, 18, 50, 255, 1, 6,
	218, 174, 228, 65, 186, 65, 65, 92, 65, 65, 65, 255, 174, 65, 65, 174,
	117, 255, 101, 16, 180, 20, 33, 94, 10, 131, 20, 222, 143, 38, 15, 105,
}

var celpAcbgainsDcmfHR = [(acbgN + 1) * acbgN]uint8{
	254, 105, 212, 26, 110, 255, 202, 93, 152, 121, 110, 43, 150, 20, 81, 176,
	255, 28, 100, 5, 26, 184, 61, 29, 36, 26, 28, 9, 61, 4, 27, 116,
	121, 255, 161, 39, 195, 215, 191, 75, 186, 178, 119, 82, 68, 41, 43, 56,
	188, 65, 243, 15, 74, 255, 205, 79, 123, 84, 95, 26, 139, 13, 67, 154,
	81, 219, 173, 70, 219, 165, 234, 102, 231, 255, 191, 119, 87, 60, 62, 59,
	106, 255, 182, 49, 242, 196, 233, 95, 247, 228, 152, 96, 81, 45, 54, 61,
	236, 55, 178, 10, 56, 255, 131, 54, 85, 58, 59, 18, 93, 9, 43, 133,
	123, 95, 224, 24, 113, 202, 255, 105, 186, 134, 135, 38, 141, 18, 82, 111,
	126, 97, 204, 34, 126, 186, 255, 141, 210, 147, 149, 46, 165, 22, 113, 122,
	96, 156, 185, 42, 188, 178, 255, 116, 248, 199, 157, 66, 109, 29, 69, 75,
	102, 207, 194, 57, 224, 193, 255, 107, 253, 242, 180, 95, 97, 44, 60, 64,
	105, 119, 202, 39, 140, 189, 255, 110, 207, 173, 165, 54, 119, 24, 75, 85,
	74, 255, 142, 59, 214, 150, 182, 76, 194, 215, 138, 122, 61, 56, 41, 45,
	200, 53, 255, 17, 66, 238, 222, 109, 129, 78, 101, 21, 227, 11, 110, 243,
	74, 255, 128, 50, 187, 149, 154, 63, 165, 184, 115, 101, 52, 47, 37, 34,
	159, 66, 232, 26, 86, 196, 255, 146, 171, 113, 134, 31, 245, 16, 145, 190,
	255, 29, 182, 7, 33, 235, 115, 55, 59, 37, 47, 11, 139, 6, 60, 234,
}

var celpFcbgVDcmf = [fcbgVN]uint8{
	107, 12, 17, 25, 31, 41, 52, 65, 83, 103, 122, 146, 169, 191, 210, 227,
	240, 249, 255, 253, 246, 229, 200, 161, 120, 82, 51, 29, 14, 6, 2, 2,
	2, 2,
}

var celpFcbgVDeltaDcmf = [fcbgVDeltaN]uint8{
	1, 1, 1, 1, 1, 1, 1, 1, 2, 3, 4, 6, 8, 10, 12, 12,
	12, 13, 14, 14, 14, 13, 12, 11, 10, 9, 8, 9, 15, 33, 65, 119,
	196, 255, 220, 144, 90, 57, 36, 23, 17, 14, 12, 12, 12, 13, 12, 12,
	12, 12, 12, 11, 11, 10, 9, 7, 6, 4, 3, 2, 1, 1, 1, 1,
	1, 1, 1,
}

// --- leaf math helpers ------------------------------------------------------

func celpDotProd(a, b []float32, l int) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L202-L208
	var r float32
	for i := 0; i < l; i++ {
		r += a[i] * b[i]
	}
	return r
}

func celpNrg(x []float32, n int) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L211-L217
	var s float32
	for k := 0; k < n; k++ {
		s += x[k] * x[k]
	}
	return s
}

func celpReverse(x []float32, l int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L220-L224
	for i := 0; i < l/2; i++ {
		x[i], x[l-i-1] = x[l-i-1], x[i]
	}
}

func celpSubVec(y, z, x []float32, l int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L243-L247
	for i := 0; i < l; i++ {
		x[i] = y[i] - z[i]
	}
}

func celpAddVecInplace(y, x []float32, l int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L251-L255
	for i := 0; i < l; i++ {
		x[i] += y[i]
	}
}

func celpScaleVecInplace(x []float32, l int, g float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L266-L270
	for i := 0; i < l; i++ {
		x[i] *= g
	}
}

func celpScaleVec(x, y []float32, l int, g float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L273-L277
	for i := 0; i < l; i++ {
		y[i] = x[i] * g
	}
}

func celpAddScaleVecInplace(x, y []float32, l int, g float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L281-L285
	for i := 0; i < l; i++ {
		y[i] += g * x[i]
	}
}

func celpAddScaleVec(x0, x1, y []float32, l int, g float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L289-L293
	for i := 0; i < l; i++ {
		y[i] = x0[i] + g*x1[i]
	}
}

func celpMulVecInplace(x, y []float32, l int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L296-L300
	for i := 0; i < l; i++ {
		y[i] *= x[i]
	}
}

func celpQ(num, den []float32, l int, q []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L303-L307
	for i := 0; i < l; i++ {
		q[i] = (num[i] * num[i]) / den[i]
	}
}

// celpMultSymtoepl2: symmetric Toeplitz multiply. c carries the trailing zero at
// 2*lResp-1; x must be readable up to n+lResp (zero padded).
func celpMultSymtoepl2(c []float32, lResp int, x, y []float32, n int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L312-L334
	length := lResp
	nn := 0
	for nn < lResp-1 {
		y[nn] = celpDotProd(c[lResp-1-nn:], x[0:], length)
		length++
		nn++
	}
	length = 2 * lResp
	for nn < n-lResp {
		y[nn] = celpDotProd(c[0:], x[nn+1-lResp:], length)
		nn++
	}
	for nn < n {
		length--
		y[nn] = celpDotProd(c[0:], x[nn+1-lResp:], length)
		nn++
	}
}

// celpFiltAr16: 16th-order AR filter; the 16-sample state sits in y[yBase-16 .. yBase].
func celpFiltAr16(x []float32, n int, coef []float32, yBase int, y []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L338-L347
	for nn := 0; nn < n; nn++ {
		res := x[nn]
		for i := 0; i < 16; i++ {
			res -= coef[16-i] * y[yBase+nn-16+i]
		}
		y[yBase+nn] = res
	}
}

// celpFiltMa: MA filter; (coefLen-1) history samples sit before x[xBase]. x != y.
func celpFiltMa(x []float32, xBase, n int, coef []float32, coefLen int, y []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L350-L370
	var i int
	if coef[0] == 1.0 {
		for k := 0; k < n; k++ {
			y[k] = x[xBase+k] + coef[1]*x[xBase+k-1]
		}
		i = 2
	} else {
		for k := 0; k < n; k++ {
			y[k] = coef[0] * x[xBase+k]
		}
		i = 1
	}
	for i < coefLen {
		for k := 0; k < n; k++ {
			y[k] += coef[i] * x[xBase+k-i]
		}
		i++
	}
}

// celpFiltMa9: 9th-order MA; the 9-sample history sits before x[xBase].
func celpFiltMa9(x []float32, xBase, n int, coef []float32, _ int, y []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L373-L388
	for nn := 0; nn < n; nn++ {
		var res float32
		for i := 0; i < 10; i++ {
			res += coef[i] * x[xBase+nn-i]
		}
		y[nn] = res
	}
}

// celpDcmfToCmf: INTEGER, bit-exact dcmf→cmf (truncating-int normalize).
func celpDcmfToCmf(dcmf []uint8, dcmfLen int, cmf []uint16) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L403-L419
	var sum int32
	for n := 0; n < dcmfLen; n++ {
		tmp := int32(dcmf[n]) + 1
		tmp *= tmp
		if tmp > 65535 {
			tmp = 65535
		}
		cmf[n+1] = uint16(tmp)
		sum += tmp
	}
	cmf[0] = 0
	for n := 1; n < dcmfLen+1; n++ {
		cmf[n] = cmf[n-1] + uint16((int32(cmf[n])*(32767-int32(dcmfLen)))/sum) + 1
	}
}

func celpCmfToBits(cmf []uint16, cmfLen int, bits []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L421-L425
	for i := 0; i < cmfLen-1; i++ {
		bits[i] = -float32(math.Log2(float64(float32(cmf[i+1]-cmf[i]) / float32(cmf[cmfLen-1]))))
	}
}

func celpGetMaxi(x []float32, xLen int) int {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L430-L440
	i := 0
	mx := x[0]
	for n := 1; n < xLen; n++ {
		if x[n] > mx {
			mx = x[n]
			i = n
		}
	}
	return i
}

func celpGetMaxiK(x []float32, idx []int32, xLen, k int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L445-L462
	taken := make([]bool, xLen)
	for kk := 0; kk < k; kk++ {
		var best float32 = -math.MaxFloat32
		bi := 0
		found := false
		for n := 0; n < xLen; n++ {
			if !taken[n] && (!found || x[n] > best) {
				best = x[n]
				bi = n
				found = true
			}
		}
		taken[bi] = true
		idx[kk] = int32(bi)
	}
}

// --- CELP tables (smpl_create_celp_tables) ---------------------------------

type celpTables struct {
	acbgInvProbLR     [(acbgN + 1) * acbgN]float32
	acbgInvProbHR     [(acbgN + 1) * acbgN]float32
	fcbgainsV         [fcbgVN]float32
	fcbgainsUV        [uvGainIdxLen + 1]float32
	fcbgVInvProb      [fcbgVN]float32
	fcbgVDeltaInvProb [fcbgVDeltaN]float32
}

var (
	celpTablesOnce sync.Once
	celpTablesInst *celpTables
)

func getCelpTables() *celpTables {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L483-L485
	celpTablesOnce.Do(func() { celpTablesInst = buildCelpTables() })
	return celpTablesInst
}

func buildCelpTables() *celpTables {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L487-L566
	t := &celpTables{}
	acbCmfLR := make([]uint16, (acbgN+1)*(acbgN+1))
	acbCmfHR := make([]uint16, (acbgN+1)*(acbgN+1))
	for i := 0; i < acbgN+1; i++ {
		celpDcmfToCmf(celpAcbgainsDcmfLR[i*acbgN:], acbgN, acbCmfLR[i*(acbgN+1):])
		celpDcmfToCmf(celpAcbgainsDcmfHR[i*acbgN:], acbgN, acbCmfHR[i*(acbgN+1):])
	}
	for i := 0; i < acbgN+1; i++ {
		celpCmfToBits(acbCmfLR[i*(acbgN+1):], acbgN+1, t.acbgInvProbLR[i*acbgN:])
		celpCmfToBits(acbCmfHR[i*(acbgN+1):], acbgN+1, t.acbgInvProbHR[i*acbgN:])
		for j := 0; j < acbgN; j++ {
			t.acbgInvProbLR[i*acbgN+j] = float32(math.Pow(2.0, float64(t.acbgInvProbLR[i*acbgN+j]*celpGAcbRdMu)))
			t.acbgInvProbHR[i*acbgN+j] = float32(math.Pow(2.0, float64(t.acbgInvProbHR[i*acbgN+j]*celpGAcbRdMu)))
		}
	}
	fcbgVCmf := make([]uint16, fcbgVN+1)
	fcbgVDeltaCmf := make([]uint16, fcbgVDeltaN+1)
	celpDcmfToCmf(celpFcbgVDcmf[:], fcbgVN, fcbgVCmf)
	celpDcmfToCmf(celpFcbgVDeltaDcmf[:], fcbgVDeltaN, fcbgVDeltaCmf)
	celpCmfToBits(fcbgVCmf, fcbgVN+1, t.fcbgVInvProb[:])
	for i := 0; i < fcbgVN; i++ {
		t.fcbgVInvProb[i] = float32(math.Pow(2.0, float64(t.fcbgVInvProb[i]*celpGAcbRdMu)))
	}
	celpCmfToBits(fcbgVDeltaCmf, fcbgVDeltaN+1, t.fcbgVDeltaInvProb[:])
	for i := 0; i < fcbgVDeltaN; i++ {
		t.fcbgVDeltaInvProb[i] = float32(math.Pow(2.0, float64(t.fcbgVDeltaInvProb[i]*celpGAcbRdMu)))
	}
	for ix := 0; ix < fcbgVN; ix++ {
		db := float32(ix)*vGainStepDb + vGainMinDb
		t.fcbgainsV[ix] = float32(math.Pow(10.0, float64(0.05*db)))
	}
	for ix := 0; ix <= uvGainIdxLen; ix++ {
		db := float32(ix)*uvGainStepDb + uvGainMinDb
		t.fcbgainsUV[ix] = float32(math.Pow(10.0, float64(0.05*db)))
	}
	return t
}

// --- LTP / ACB synthesis ----------------------------------------------------

func celpAcbDequant(lowRate bool, acbIdx int32, acbG *[acbgM]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L573-L583
	cb := &cbAcbgainsHRQ14
	if lowRate {
		cb = &cbAcbgainsLRQ14
	}
	scQ14 := 1.0 / float32(int32(1)<<14)
	for m := 0; m < acbgM; m++ {
		acbG[m] = float32(cb[int(acbIdx)*acbgM+m]) * scQ14
	}
}

func celpAcbSynthesize(fcbSubfrlen int, acbBasis []float32, acbG *[acbgM]float32, acb []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L587-L595
	celpScaleVec(acbBasis, acb, fcbSubfrlen, acbG[0])
	celpAddScaleVecInplace(acbBasis[fcbSubfrlen:], acb, fcbSubfrlen, acbG[1])
}

func celpPitchSharp(x []float32, lag, l int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L598-L602
	for i := lag; i < l; i++ {
		x[i] += x[i-lag] * pitchSharpeningCoef
	}
}

// celpSynLtpBasis builds the LTP basis per 40-sample sub-block and extends state in place.
func celpSynLtpBasis(lags []float32, nLags int, state []float32, stateLen int, acbBasis []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L607-L678
	p := stateLen - nLags*celpLagSubfrlen
	for subfr := 0; subfr < nLags; subfr++ {
		iLag := int32(math.Floor(float64(lags[subfr])))
		if float32(iLag) == lags[subfr] {
			il := int(iLag)
			for i := 0; i < celpLagSubfrlen; i++ {
				state[p+i] = state[(p+i)-il]
			}
			for i := 0; i < celpLagSubfrlen; i++ {
				acbBasis[subfr*celpLagSubfrlen+i] = state[p+i]
			}
			for i := 0; i < celpLagSubfrlen; i++ {
				a := state[(p+i)-il-1]
				b := state[(p+i)-il+1]
				acbBasis[(nLags+subfr)*celpLagSubfrlen+i] = a + b
			}
		} else {
			il := int(iLag)
			baseFirst := p + (-1 - il - celpLtpInterpolDelay)
			first := celpDotProd(state[baseFirst:], celpInterpolKernel[:], 2*celpLtpInterpolDelay)
			srcBase := p + (-il - celpLtpInterpolDelay)
			for nn := 0; nn < celpLagSubfrlen; nn++ {
				var ret float32
				for i := 0; i < 8; i++ {
					s0 := state[srcBase+nn+i]
					s1 := state[srcBase+nn+15-i]
					ret += (s0 + s1) * celpInterpolKernel[i]
				}
				state[p+nn] = ret
			}
			baseLast := p + (celpLagSubfrlen - 1 - il - celpLtpInterpolDelay)
			last := celpDotProd(state[baseLast:], celpInterpolKernel[:], 2*celpLtpInterpolDelay)
			for i := 0; i < celpLagSubfrlen; i++ {
				acbBasis[subfr*celpLagSubfrlen+i] = state[p+i]
			}
			b1 := (nLags + subfr) * celpLagSubfrlen
			acbBasis[b1] = first + state[p+1]
			for i := 0; i < celpLagSubfrlen-2; i++ {
				acbBasis[b1+1+i] = state[p+i] + state[p+i+2]
			}
			iLast := celpLagSubfrlen - 1
			acbBasis[b1+iLast] = state[p+iLast-1] + last
		}
		p += celpLagSubfrlen
	}
}

// --- FCB search helpers -----------------------------------------------------

func celpCalcDAbsAndSign(d []float32, l int, dAbs, dSign []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L726-L736
	for i := 0; i < l; i++ {
		if d[i] > 0.0 {
			dAbs[i] = d[i]
			dSign[i] = 1.0
		} else {
			dAbs[i] = -d[i]
			dSign[i] = -1.0
		}
	}
}

func celpCheckIfBetter(wnrg float32, nrgThr *float32, wnrgPerPulse float32) bool {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L738-L746
	*nrgThr += wnrgPerPulse
	if wnrg > *nrgThr {
		*nrgThr = wnrg
		return true
	}
	return false
}

func celpPhiColOffset(col int32) int32 { return int32(smplMaxSfLen) - col }

func celpNonZeroRange(col int32, percRespLen, fcbSubfrlen int) (int, int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L756-L760
	lo := col - int32(percRespLen) + 1
	if lo < 0 {
		lo = 0
	}
	hi := col + int32(percRespLen)
	if hi > int32(fcbSubfrlen) {
		hi = int32(fcbSubfrlen)
	}
	return int(lo), int(hi)
}

// CelpSubframeOut is the per-subframe encode result.
type CelpSubframeOut struct {
	Pulses  [smplCelpMaxRates][]int16
	NPulses [smplCelpMaxRates]int16
	AcbIdx  [smplCelpMaxRates]int16
	GainIdx [smplCelpMaxRates]int16
	ExcLpc  []float32
}

type acbgParams struct {
	werrIn      float32
	phiAcb      [acbgM * acbgM]float32
	dAcbLpc     [acbgM]float32
	acbBasisPhi []float32
}

type fcb struct {
	wnrg        float32
	nPulses     int32
	posNew      int32
	signNew     float32
	sgntr       uint64
	fcbStateIdx int
}

type fcbState struct {
	pulsePositions []int32
	pulseSigns     []float32
	num            []float32
	den            []float32
}

func newFcbState() fcbState {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L715-L724
	return fcbState{
		pulsePositions: make([]int32, smplMaxSfLen),
		pulseSigns:     make([]float32, smplMaxSfLen),
		num:            make([]float32, smplMaxSfLen),
		den:            make([]float32, smplMaxSfLen),
	}
}

func (s *fcbState) cloneFrom(o *fcbState) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L707-L724
	copy(s.pulsePositions, o.pulsePositions)
	copy(s.pulseSigns, o.pulseSigns)
	copy(s.num, o.num)
	copy(s.den, o.den)
}

// CelpEncoder is the persistent encode-side CELP state.
type CelpEncoder struct {
	stateWghtBuf   []float32
	stateErrLpcSyn [SmplLPCOrder]float32
	hanningWin     []float32
	sgntrs         []uint64
	acbState       []float32
	acbStateLen    int
	prevAcbIdx     [smplCelpMaxRates]int32
	prevFcbIdx     [smplCelpMaxRates]int32
	subfrCnt       int32
	subfrPerPacket int32
	fcbSubfrlen    int
	percRespLen    int
	lowRate        bool
	ignoreZir      bool
	fcbgain        float32
	useMa9         bool

	impLpcBuf []float32
	phi       []float32
	phiFlip   []float32
}

// NewCelpEncoder builds the encoder (mirrors CelpEncoder::new).
func NewCelpEncoder(lowRate bool, percRespLen, fcbSubfrlen, subfrPerPacket int) *CelpEncoder {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L816-L871
	_ = getCelpTables()
	acbStateLen := fcbSubfrlen + celpMaxpitchLen + celpLtpInterpolDelay

	sgntrs := make([]uint64, smplMaxSfLen)
	var s uint64 = 0x9E3779B97F4A7C15
	for i := range sgntrs {
		s = s*6364136223846793005 + 1442695040888963407
		sgntrs[i] = s
	}

	hanningWin := make([]float32, percRespLen)
	scale := 1.0 / float32(2*smplPercRespLen+1)
	for i := 0; i < percRespLen; i++ {
		hanningWin[i] = float32(math.Sin(float64(smplPI * float32(percRespLen+i+1) * scale)))
	}

	e := &CelpEncoder{
		stateWghtBuf:   make([]float32, smplMaxSfLen+SmplLPCOrder),
		hanningWin:     hanningWin,
		sgntrs:         sgntrs,
		acbState:       make([]float32, celpMaxPitchLag+smplMaxSfLen+celpLtpInterpolDelay),
		acbStateLen:    acbStateLen,
		prevAcbIdx:     [smplCelpMaxRates]int32{-1, -1},
		prevFcbIdx:     [smplCelpMaxRates]int32{-1, -1},
		subfrPerPacket: int32(subfrPerPacket),
		fcbSubfrlen:    fcbSubfrlen,
		percRespLen:    percRespLen,
		lowRate:        lowRate,
		useMa9:         percRespLen == 10,
		impLpcBuf:      make([]float32, smplMaxSfLen+SmplLPCOrder),
		phi:            make([]float32, smplMaxSfLen),
		phiFlip:        make([]float32, 2*smplMaxSfLen),
	}
	return e
}

func (e *CelpEncoder) percFiltMa(x []float32, xBase, n int, coef []float32, coefLen int, y []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L873-L888
	if e.useMa9 {
		celpFiltMa9(x, xBase, n, coef, coefLen, y)
	} else {
		celpFiltMa(x, xBase, n, coef, coefLen, y)
	}
}

// --- greedy FCB search (smpl_fcb_search) ------------------------------------

func (e *CelpEncoder) smplFcbSearch(d []float32, wnrgPerPulse *[smplCelpMaxRates]float32, fcbPulsesMax *[smplCelpMaxRates]int16,
	pulses *[smplCelpMaxRates][smplMaxPulsesPerSf]int16, nPulses *[smplCelpMaxRates]int16, wnrg, gainFromSearch, fcbWnrg *[smplCelpMaxRates]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L946-L1064
	fcbSubfrlen := e.fcbSubfrlen
	percRespLen := e.percRespLen
	*nPulses = [smplCelpMaxRates]int16{}

	var positions [smplMaxPulsesPerSf]int32
	dAbs := make([]float32, smplMaxSfLen)
	dSign := make([]float32, smplMaxSfLen)
	num := make([]float32, smplMaxSfLen)
	den := make([]float32, smplMaxSfLen)
	phi0 := e.phi[0]
	celpCalcDAbsAndSign(d, fcbSubfrlen, dAbs, dSign)

	for i := 0; i < fcbSubfrlen; i++ {
		den[i] = phi0 + 1e-16
	}
	copy(num[:fcbSubfrlen], dAbs[:fcbSubfrlen])
	positions[0] = int32(celpGetMaxi(num, fcbSubfrlen))
	var nrgThr [smplCelpMaxRates]float32
	p0 := int(positions[0])
	ratio := num[p0] / den[p0]
	wnrg0 := num[p0] * ratio
	if celpCheckIfBetter(wnrg0, &nrgThr[smplCelpIdxMain], wnrgPerPulse[smplCelpIdxMain]) {
		nPulses[smplCelpIdxMain] = 1
		wnrg[smplCelpIdxMain] = wnrg0
		wnrg[smplCelpIdxFec] = wnrg0
		gainFromSearch[smplCelpIdxMain] = ratio
		gainFromSearch[smplCelpIdxFec] = ratio
		fcbWnrg[smplCelpIdxMain] = den[p0]
		fcbWnrg[smplCelpIdxFec] = den[p0]
		if fcbPulsesMax[smplCelpIdxFec] > 0 {
			nPulses[smplCelpIdxFec] = nPulses[smplCelpIdxMain]
			wnrg[smplCelpIdxFec] = wnrg[smplCelpIdxMain]
			gainFromSearch[smplCelpIdxFec] = gainFromSearch[smplCelpIdxMain]
			fcbWnrg[smplCelpIdxFec] = fcbWnrg[smplCelpIdxMain]
		}
	}

	for pulseNr := 1; pulseNr < int(fcbPulsesMax[smplCelpIdxMain]); pulseNr++ {
		position := positions[pulseNr-1]
		sgn := dSign[position]
		for i := 0; i < fcbSubfrlen; i++ {
			num[i] += dAbs[position]
		}
		nz0, nz1 := celpNonZeroRange(position, percRespLen, fcbSubfrlen)
		colOff := celpPhiColOffset(position)
		var dDen float32
		for i := 0; i < pulseNr-1; i++ {
			pi := int(positions[i])
			dDen += e.phiFlip[int(colOff)+pi] * dSign[pi]
		}
		dDen *= 2.0 * sgn
		dDen += e.phiFlip[int(colOff+position)]
		for i := 0; i < fcbSubfrlen; i++ {
			den[i] += dDen
		}
		for i := nz0; i < nz1; i++ {
			den[i] += 2.0 * sgn * dSign[i] * e.phiFlip[int(colOff)+i]
		}
		q := make([]float32, smplMaxSfLen)
		celpQ(num, den, fcbSubfrlen, q)
		positions[pulseNr] = int32(celpGetMaxi(q, fcbSubfrlen))
		pp := int(positions[pulseNr])
		if celpCheckIfBetter(q[pp], &nrgThr[smplCelpIdxMain], wnrgPerPulse[smplCelpIdxMain]) {
			nPulses[smplCelpIdxMain] = int16(pulseNr + 1)
			wnrg[smplCelpIdxMain] = q[pp]
			gainFromSearch[smplCelpIdxMain] = num[pp] / den[pp]
			fcbWnrg[smplCelpIdxMain] = den[pp]
		}
		if int(fcbPulsesMax[smplCelpIdxFec]) >= pulseNr &&
			celpCheckIfBetter(q[pp], &nrgThr[smplCelpIdxFec], wnrgPerPulse[smplCelpIdxFec]) {
			nPulses[smplCelpIdxFec] = int16(pulseNr + 1)
			wnrg[smplCelpIdxFec] = q[pp]
			gainFromSearch[smplCelpIdxFec] = num[pp] / den[pp]
			fcbWnrg[smplCelpIdxFec] = den[pp]
		}
	}

	for r := smplCelpIdxFec; r <= smplCelpIdxMain; r++ {
		if nrgThr[r] > 0.0 {
			for i := 0; i < int(nPulses[r]); i++ {
				position := positions[i]
				if dSign[position] > 0.0 {
					pulses[r][i] = 1 + int16(position)
				} else {
					pulses[r][i] = -(1 + int16(position))
				}
			}
		} else {
			wnrg[r] = 0.0
			gainFromSearch[r] = 0.0
			fcbWnrg[r] = 0.0
			nPulses[r] = 0
		}
	}
}

// --- delayed-decision beam FCB search ---------------------------------------

type fcbSearchScratch struct {
	fcbStates         [2][]fcbState
	readIdx           int
	writeIdx          int
	fcbs              []fcb
	fcbsSize          int
	fcbCandidates     []fcb
	fcbCandidatesSize int
	uniqueSgntr       []uint64
	uniqueSgntrSize   int
}

func newFcbSearchScratch() *fcbSearchScratch {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L908-L926
	mk := func() []fcbState {
		v := make([]fcbState, celpMaxNumsurv)
		for i := range v {
			v[i] = newFcbState()
		}
		return v
	}
	return &fcbSearchScratch{
		fcbStates:     [2][]fcbState{mk(), mk()},
		readIdx:       0,
		writeIdx:      1,
		fcbs:          make([]fcb, celpMaxNumsurv),
		fcbCandidates: make([]fcb, celpMaxNumsurv*celpMaxNumsurv),
		uniqueSgntr:   make([]uint64, celpMaxNumsurv*celpMaxNumsurv),
	}
}

func (sc *fcbSearchScratch) swapRw() { sc.readIdx, sc.writeIdx = sc.writeIdx, sc.readIdx }

func (sc *fcbSearchScratch) isUnique(sgntr uint64) bool {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L928-L930
	for i := 0; i < sc.uniqueSgntrSize; i++ {
		if sc.uniqueSgntr[i] == sgntr {
			return false
		}
	}
	return true
}

func (e *CelpEncoder) addPulse(sc *fcbSearchScratch, fcbIdxIn int, dAbs, dSign []float32, numsurv, idx int, lag int32, pitchSharp float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1071-L1242
	fcbSubfrlen := e.fcbSubfrlen
	percRespLen := e.percRespLen

	fcbPosNew := sc.fcbs[fcbIdxIn].posNew
	fcbSignNew := sc.fcbs[fcbIdxIn].signNew
	fcbNPulses := sc.fcbs[fcbIdxIn].nPulses
	fcbStateIdx := sc.fcbs[fcbIdxIn].fcbStateIdx
	fcbSgntrBase := sc.fcbs[fcbIdxIn].sgntr

	ri := sc.readIdx
	wi := sc.writeIdx

	add := dAbs[fcbPosNew]
	for i := 0; i < fcbSubfrlen; i++ {
		sc.fcbStates[wi][idx].num[i] = sc.fcbStates[ri][fcbStateIdx].num[i] + add
	}
	for i := 0; i < fcbSubfrlen; i++ {
		sc.fcbStates[wi][idx].den[i] = sc.fcbStates[ri][fcbStateIdx].den[i]
	}

	if pitchSharp == 0.0 {
		nz0, nz1 := celpNonZeroRange(fcbPosNew, percRespLen, fcbSubfrlen)
		colOff := celpPhiColOffset(fcbPosNew)
		var dDen float32
		for i := 0; i < int(fcbNPulses); i++ {
			pos := sc.fcbStates[ri][fcbStateIdx].pulsePositions[i]
			sgn := sc.fcbStates[ri][fcbStateIdx].pulseSigns[i]
			dDen += e.phiFlip[int(colOff+pos)] * sgn
		}
		dDen *= 2.0 * fcbSignNew
		dDen += e.phiFlip[int(colOff+fcbPosNew)]
		for i := 0; i < fcbSubfrlen; i++ {
			sc.fcbStates[wi][idx].den[i] += dDen
		}
		for i := nz0; i < nz1; i++ {
			sc.fcbStates[wi][idx].den[i] += 2.0 * fcbSignNew * dSign[i] * e.phiFlip[int(colOff)+i]
		}
	} else {
		var g1 float32
		var dDen float32
		g1 = 1.0
		for pos := fcbPosNew; pos < int32(fcbSubfrlen); pos += lag {
			colOff := celpPhiColOffset(pos)
			for i := 0; i < int(fcbNPulses); i++ {
				g2 := g1
				pulsePos := sc.fcbStates[ri][fcbStateIdx].pulsePositions[i]
				pulseSgn := sc.fcbStates[ri][fcbStateIdx].pulseSigns[i]
				for posq := pulsePos; posq < int32(fcbSubfrlen); posq += lag {
					dDen += g2 * e.phiFlip[int(colOff+posq)] * pulseSgn
					g2 *= pitchSharp
				}
			}
			g1 *= pitchSharp
		}
		dDen *= 2.0 * fcbSignNew
		g1 = 1.0
		for pos1 := fcbPosNew; pos1 < int32(fcbSubfrlen); pos1 += lag {
			colOff := celpPhiColOffset(pos1)
			g2 := g1
			for pos2 := fcbPosNew; pos2 < int32(fcbSubfrlen); pos2 += lag {
				dDen += g2 * e.phiFlip[int(colOff+pos2)]
				g2 *= pitchSharp
			}
			g1 *= pitchSharp
		}
		for i := 0; i < fcbSubfrlen; i++ {
			sc.fcbStates[wi][idx].den[i] += dDen
		}
		ddDen := make([]float32, smplMaxSfLen)
		g1 = 1.0
		for pos := fcbPosNew; pos < int32(fcbSubfrlen); pos += lag {
			nz0, nz1 := celpNonZeroRange(pos, percRespLen, fcbSubfrlen)
			colOff := celpPhiColOffset(pos)
			g2 := g1
			for k := int32(0); k < int32(fcbSubfrlen); k += lag {
				startI := int32(nz0) - k
				if startI < 0 {
					startI = 0
				}
				endI := int32(fcbSubfrlen) - k
				if int32(nz1)-k < endI {
					endI = int32(nz1) - k
				}
				for i := startI; i < endI; i++ {
					ddDen[i] += g2 * e.phiFlip[int(colOff+i+k)]
				}
				g2 *= pitchSharp
			}
			g1 *= pitchSharp
		}
		for i := 0; i < fcbSubfrlen; i++ {
			sc.fcbStates[wi][idx].den[i] += 2.0 * fcbSignNew * dSign[i] * ddDen[i]
		}
	}

	for i := 0; i < int(fcbNPulses); i++ {
		sc.fcbStates[wi][idx].pulsePositions[i] = sc.fcbStates[ri][fcbStateIdx].pulsePositions[i]
		sc.fcbStates[wi][idx].pulseSigns[i] = sc.fcbStates[ri][fcbStateIdx].pulseSigns[i]
	}
	sc.fcbStates[wi][idx].pulsePositions[fcbNPulses] = fcbPosNew
	sc.fcbStates[wi][idx].pulseSigns[fcbNPulses] = fcbSignNew

	newNPulses := fcbNPulses + 1
	q := make([]float32, smplMaxSfLen)
	celpQ(sc.fcbStates[wi][idx].num, sc.fcbStates[wi][idx].den, fcbSubfrlen, q)
	var sortIx [celpMaxNumsurv]int32
	celpGetMaxiK(q, sortIx[:], fcbSubfrlen, numsurv)
	for i := 0; i < numsurv; i++ {
		pos := int(sortIx[i])
		sgntr := fcbSgntrBase + e.sgntrs[pos]
		if sc.isUnique(sgntr) {
			sc.fcbCandidates[sc.fcbCandidatesSize] = fcb{wnrg: q[pos], nPulses: newNPulses, posNew: int32(pos), signNew: dSign[pos], sgntr: sgntr, fcbStateIdx: idx}
			sc.fcbCandidatesSize++
			sc.uniqueSgntr[sc.uniqueSgntrSize] = sgntr
			sc.uniqueSgntrSize++
		}
	}
}

func (e *CelpEncoder) smplFcbSearchDeldec(d []float32, pitchSharp float32, lag int32, wnrgPerPulse *[smplCelpMaxRates]float32, fcbPulsesMax *[smplCelpMaxRates]int16, surv []int16,
	pulses *[smplCelpMaxRates][smplMaxPulsesPerSf]int16, nPulses *[smplCelpMaxRates]int16, wnrg, gainFromSearch, fcbWnrg *[smplCelpMaxRates]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1247-L1494
	fcbSubfrlen := e.fcbSubfrlen
	sc := newFcbSearchScratch()

	dNew := make([]float32, smplMaxSfLen)
	dAbs := make([]float32, smplMaxSfLen)
	dSign := make([]float32, smplMaxSfLen)
	phi0 := e.phi[0]

	if pitchSharp != 0.0 && lag > 0 && lag < int32(fcbSubfrlen) {
		copy(dNew[:fcbSubfrlen], d[:fcbSubfrlen])
		for j := 0; j < fcbSubfrlen; j++ {
			g := pitchSharp
			for i := lag + int32(j); i < int32(fcbSubfrlen); i += lag {
				dNew[j] += g * d[i]
				g *= pitchSharp
			}
		}
		celpCalcDAbsAndSign(dNew, fcbSubfrlen, dAbs, dSign)
	} else {
		celpCalcDAbsAndSign(d, fcbSubfrlen, dAbs, dSign)
		pitchSharp = 0.0
	}

	sc.readIdx = 0
	sc.writeIdx = 1
	var bestFcb [smplCelpMaxRates]fcb
	bestFcbState := [smplCelpMaxRates]fcbState{newFcbState(), newFcbState()}
	var nrgThr [smplCelpMaxRates]float32

	{
		wi := sc.writeIdx
		copy(sc.fcbStates[wi][0].num[:fcbSubfrlen], dAbs[:fcbSubfrlen])
		if pitchSharp == 0.0 {
			for i := 0; i < fcbSubfrlen; i++ {
				sc.fcbStates[wi][0].den[i] = phi0 + 1e-16
			}
		} else {
			offset := int32(fcbSubfrlen) - 1
			for i := int32(fcbSubfrlen) - 1; i >= 0; i -= lag {
				res := float32(1e-16)
				g1 := float32(1.0)
				for j := i; j < int32(fcbSubfrlen); j += lag {
					colOff := celpPhiColOffset(j)
					g2 := float32(1.0)
					for k := i; k < int32(fcbSubfrlen); k += lag {
						res += g1 * g2 * e.phiFlip[int(colOff+k)]
						g2 *= pitchSharp
					}
					g1 *= pitchSharp
				}
				length := lag
				if offset+1 < length {
					length = offset + 1
				}
				for jj := int32(0); jj < length; jj++ {
					sc.fcbStates[wi][0].den[offset-jj] = res
				}
				offset -= length
			}
		}
	}

	sc.swapRw()
	q := make([]float32, smplMaxSfLen)
	{
		ri := sc.readIdx
		if pitchSharp == 0.0 {
			copy(q[:fcbSubfrlen], sc.fcbStates[ri][0].num[:fcbSubfrlen])
		} else {
			celpQ(sc.fcbStates[ri][0].num, sc.fcbStates[ri][0].den, fcbSubfrlen, q)
		}
	}

	var sortIx [celpMaxNumsurv]int32
	celpGetMaxiK(q, sortIx[:], fcbSubfrlen, int(surv[0]))
	sc.fcbsSize = 0
	{
		ri := sc.readIdx
		for i := 0; i < int(surv[0]); i++ {
			pos := int(sortIx[i])
			sc.fcbs[sc.fcbsSize] = fcb{
				sgntr:   e.sgntrs[pos],
				posNew:  int32(pos),
				signNew: dSign[pos],
				wnrg:    (sc.fcbStates[ri][0].num[pos] * sc.fcbStates[ri][0].num[pos]) / sc.fcbStates[ri][0].den[pos],
			}
			sc.fcbsSize++
		}
	}

	e.checkIfBetterDeldec(sc, false, 0, &bestFcb[smplCelpIdxMain], &bestFcbState[smplCelpIdxMain], &nrgThr[smplCelpIdxMain], wnrgPerPulse[smplCelpIdxMain])
	if fcbPulsesMax[smplCelpIdxFec] > 0 {
		e.checkIfBetterDeldec(sc, false, 0, &bestFcb[smplCelpIdxFec], &bestFcbState[smplCelpIdxFec], &nrgThr[smplCelpIdxFec], wnrgPerPulse[smplCelpIdxFec])
	}

	if fcbPulsesMax[smplCelpIdxMain] > 1 {
		for pulseNr := 2; pulseNr < int(fcbPulsesMax[smplCelpIdxMain]); pulseNr++ {
			sc.fcbCandidatesSize = 0
			sc.uniqueSgntrSize = 0
			fcbsSize := sc.fcbsSize
			for i := 0; i < fcbsSize; i++ {
				e.addPulse(sc, i, dAbs, dSign, int(surv[pulseNr-1]), i, lag, pitchSharp)
			}
			sc.swapRw()
			candSize := sc.fcbCandidatesSize
			for i := 0; i < candSize; i++ {
				q[i] = sc.fcbCandidates[i].wnrg
			}
			celpGetMaxiK(q, sortIx[:], candSize, int(surv[pulseNr-1]))
			sc.fcbsSize = 0
			for i := 0; i < int(surv[pulseNr-1]); i++ {
				sc.fcbs[sc.fcbsSize] = sc.fcbCandidates[sortIx[i]]
				sc.fcbsSize++
			}
			e.checkIfBetterDeldec(sc, false, 0, &bestFcb[smplCelpIdxMain], &bestFcbState[smplCelpIdxMain], &nrgThr[smplCelpIdxMain], wnrgPerPulse[smplCelpIdxMain])
			if int(fcbPulsesMax[smplCelpIdxFec]) >= pulseNr {
				e.checkIfBetterDeldec(sc, false, 0, &bestFcb[smplCelpIdxFec], &bestFcbState[smplCelpIdxFec], &nrgThr[smplCelpIdxFec], wnrgPerPulse[smplCelpIdxFec])
			}
		}
		sc.fcbCandidatesSize = 0
		sc.uniqueSgntrSize = 0
		fcbsSize := sc.fcbsSize
		for i := 0; i < fcbsSize; i++ {
			e.addPulse(sc, i, dAbs, dSign, 1, i, lag, pitchSharp)
		}
		sc.swapRw()
		bestIdx := 0
		maxWnrg := sc.fcbCandidates[0].wnrg
		for i := 1; i < sc.fcbCandidatesSize; i++ {
			if sc.fcbCandidates[i].wnrg > maxWnrg {
				maxWnrg = sc.fcbCandidates[i].wnrg
				bestIdx = i
			}
		}
		e.checkIfBetterDeldec(sc, true, bestIdx, &bestFcb[smplCelpIdxMain], &bestFcbState[smplCelpIdxMain], &nrgThr[smplCelpIdxMain], wnrgPerPulse[smplCelpIdxMain])
	}

	for r := smplCelpIdxFec; r <= smplCelpIdxMain; r++ {
		for i := 0; i < int(bestFcb[r].nPulses); i++ {
			if bestFcbState[r].pulseSigns[i] > 0.0 {
				pulses[r][i] = 1 + int16(bestFcbState[r].pulsePositions[i])
			} else {
				pulses[r][i] = -(1 + int16(bestFcbState[r].pulsePositions[i]))
			}
		}
		if bestFcb[r].signNew > 0.0 {
			pulses[r][bestFcb[r].nPulses] = 1 + int16(bestFcb[r].posNew)
		} else {
			pulses[r][bestFcb[r].nPulses] = -(1 + int16(bestFcb[r].posNew))
		}
		if bestFcb[r].wnrg > 0.0 {
			wnrg[r] = bestFcb[r].wnrg
			pn := int(bestFcb[r].posNew)
			gainFromSearch[r] = bestFcbState[r].num[pn] / bestFcbState[r].den[pn]
			fcbWnrg[r] = bestFcbState[r].den[pn]
			nPulses[r] = int16(bestFcb[r].nPulses) + 1
		} else {
			wnrg[r] = 0.0
			gainFromSearch[r] = 0.0
			fcbWnrg[r] = 0.0
			nPulses[r] = 0
		}
	}
}

// checkIfBetterDeldec: fromCand selects sc.fcbCandidates[idx] vs sc.fcbs[idx].
func (e *CelpEncoder) checkIfBetterDeldec(sc *fcbSearchScratch, fromCand bool, idx int, bestFcb *fcb, bestFcbState *fcbState, nrgThr *float32, wnrgPerPulse float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1497-L1532
	*nrgThr += wnrgPerPulse
	var f *fcb
	if fromCand {
		f = &sc.fcbCandidates[idx]
	} else {
		f = &sc.fcbs[idx]
	}
	if f.wnrg > *nrgThr {
		*nrgThr = f.wnrg
		*bestFcb = *f
		bestFcbState.cloneFrom(&sc.fcbStates[sc.readIdx][f.fcbStateIdx])
	}
}

// --- gain quant -------------------------------------------------------------

func celpWnrg2(c, x []float32) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1540-L1542
	return x[0]*(c[0]*x[0]+c[1]*x[1]) + x[1]*(c[2]*x[0]+c[3]*x[1])
}

func celpWnrg3(c, x []float32) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1545-L1549
	return x[0]*(c[0]*x[0]+c[1]*x[1]+c[2]*x[2]) +
		x[1]*(c[3]*x[0]+c[4]*x[1]+c[5]*x[2]) +
		x[2]*(c[6]*x[0]+c[7]*x[1]+c[8]*x[2])
}

func celpQuantGainUv(gainFromSearch float32) int16 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1552-L1556
	gainDb := 20.0 * float32(math.Log10(float64(gainFromSearch+1.0e-16)))
	if gainDb < uvGainMinDb {
		gainDb = uvGainMinDb
	}
	if gainDb > uvGainMaxDb {
		gainDb = uvGainMaxDb
	}
	return int16(math.Round(float64((gainDb - uvGainMinDb) / uvGainStepDb)))
}

func celpFcbSynthesize(fcbSubfrlen int, pulses []int16, nPulses int, fcb []float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1558-L1568
	for i := 0; i < fcbSubfrlen; i++ {
		fcb[i] = 0.0
	}
	for n := 0; n < nPulses; n++ {
		sign := int32(1) + 2*(int32(pulses[n])>>15)
		pos := int32(pulses[n])*sign - 1
		fcb[pos] += float32(sign)
	}
}

func (e *CelpEncoder) calcAcbGain(lResp int, acbBasis, dLpc []float32, acbg *acbgParams, dLtp []float32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1572-L1646
	fcbSubfrlen := e.fcbSubfrlen
	for m := 0; m < acbgM; m++ {
		cOff := smplMaxSfLen - lResp + 1
		tmp := make([]float32, fcbSubfrlen)
		celpMultSymtoepl2(e.phiFlip[cOff:], lResp, acbBasis[m*fcbSubfrlen:], tmp, fcbSubfrlen)
		copy(acbg.acbBasisPhi[m*fcbSubfrlen:m*fcbSubfrlen+fcbSubfrlen], tmp)
		for i := 0; i < acbgM; i++ {
			acbg.phiAcb[m*acbgM+i] = celpDotProd(acbBasis[i*fcbSubfrlen:], acbg.acbBasisPhi[m*fcbSubfrlen:], fcbSubfrlen)
		}
		acbg.dAcbLpc[m] = celpDotProd(acbBasis[m*fcbSubfrlen:], dLpc, fcbSubfrlen)
	}

	bestRd := float32(1e30)
	bestAcbgIdx := int32(0)
	transitionIdx := int32(0)
	if e.prevAcbIdx[smplCelpIdxMain] != -1 {
		transitionIdx = e.prevAcbIdx[smplCelpIdxMain] + 1
	}
	invProbFull := e.acbgInvProb()
	invProb := invProbFull[int(transitionIdx)*acbgN:]
	cb := &cbAcbgainsHRQ14
	if e.lowRate {
		cb = &cbAcbgainsLRQ14
	}
	scQ14 := 1.0 / float32(int32(1)<<14)
	var acbGains [acbgM]float32
	for n := 0; n < acbgN; n++ {
		for m := 0; m < acbgM; m++ {
			acbGains[m] = float32(cb[n*acbgM+m]) * scQ14
		}
		werrOut := acbg.werrIn + celpWnrg2(acbg.phiAcb[:], acbGains[:]) -
			2.0*(acbg.dAcbLpc[0]*acbGains[0]+acbg.dAcbLpc[1]*acbGains[1])
		rd := werrOut * invProb[n]
		if rd < bestRd {
			bestRd = rd
			bestAcbgIdx = int32(n)
		}
	}

	g0 := -float32(cb[int(bestAcbgIdx)*acbgM]) * scQ14
	celpAddScaleVec(dLpc, acbg.acbBasisPhi, dLtp, fcbSubfrlen, g0)
	g1 := -float32(cb[int(bestAcbgIdx)*acbgM+1]) * scQ14
	celpAddScaleVecInplace(acbg.acbBasisPhi[fcbSubfrlen:], dLtp, fcbSubfrlen, g1)
	return bestAcbgIdx
}

func (e *CelpEncoder) acbgInvProb() []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1613-L1618
	if e.lowRate {
		return getCelpTables().acbgInvProbLR[:]
	}
	return getCelpTables().acbgInvProbHR[:]
}

func (e *CelpEncoder) calcGainsV(fcbWnrg, gainFromSearch float32, excFcb, dLpc []float32, acbg *acbgParams, rateIdx int, acbIdx, fcbIdx *[smplCelpMaxRates]int16) float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1649-L1761
	tbl := getCelpTables()
	fcbSubfrlen := e.fcbSubfrlen

	fcbgain := gainFromSearch
	if fcbgain < 0.0 {
		fcbgain = 0.0
	}
	gainDb := 20.0 * float32(math.Log10(float64(fcbgain+1.0e-16)))
	if gainDb < vGainMinDb {
		gainDb = vGainMinDb
	}
	if gainDb > vGainMaxDb {
		gainDb = vGainMaxDb
	}
	maxGainIdx := int32(math.Round(float64((vGainMaxDb - vGainMinDb) / vGainStepDb)))

	bestAcbgIdx := int32(0)
	bestFcbgIdx := int32(0)

	var acbFcb [acbgM]float32
	for i := 0; i < acbgM; i++ {
		acbFcb[i] = celpDotProd(acbg.acbBasisPhi[i*fcbSubfrlen:], excFcb, fcbSubfrlen)
	}
	var phiAll [(acbgM + 1) * (acbgM + 1)]float32
	stride := acbgM + 1
	for i := 0; i < acbgM; i++ {
		for j := 0; j < acbgM; j++ {
			phiAll[i*stride+j] = acbg.phiAcb[i*acbgM+j]
		}
	}
	for i := 0; i < acbgM; i++ {
		phiAll[i*stride+acbgM] = acbFcb[i]
		phiAll[acbgM*stride+i] = acbFcb[i]
	}
	phiAll[acbgM*stride+acbgM] = fcbWnrg

	var dall [acbgM + 1]float32
	copy(dall[:acbgM], acbg.dAcbLpc[:])
	dall[acbgM] = celpDotProd(dLpc, excFcb, fcbSubfrlen)

	var gainIdxs [nGainSteps]int32
	var fcbgains [nGainSteps]float32
	var fcbgInvProb [nGainSteps]float32
	firstGainIdx := int32(math.Floor(float64((gainDb-vGainMinDb)/vGainStepDb))) - (nGainSteps-1)/2
	if firstGainIdx < 0 {
		firstGainIdx = 0
	}
	if firstGainIdx > maxGainIdx-1 {
		firstGainIdx = maxGainIdx - 1
	}
	offset := int32(math.Floor(float64((vGainMinDb - vGainMaxDb) / vGainStepDb)))
	for i := 0; i < nGainSteps; i++ {
		gainIdxs[i] = firstGainIdx + int32(i)
		fcbgains[i] = tbl.fcbgainsV[gainIdxs[i]]
		if e.prevFcbIdx[rateIdx] == -1 {
			fcbgInvProb[i] = tbl.fcbgVInvProb[gainIdxs[i]]
		} else {
			delta := e.prevFcbIdx[rateIdx] - gainIdxs[i]
			cmfIdx := delta - offset
			fcbgInvProb[i] = tbl.fcbgVDeltaInvProb[cmfIdx]
		}
	}

	bestRd := float32(1e30)
	transitionIdx := int32(0)
	if e.prevAcbIdx[rateIdx] != -1 {
		transitionIdx = e.prevAcbIdx[rateIdx] + 1
	}
	cb := &cbAcbgainsHRQ14
	if e.lowRate {
		cb = &cbAcbgainsLRQ14
	}
	invProb := e.acbgInvProb()[int(transitionIdx)*acbgN:]
	scQ14 := 1.0 / float32(int32(1)<<14)
	for n := 0; n < acbgN; n++ {
		var gains [acbgM + 1]float32
		for m := 0; m < acbgM; m++ {
			gains[m] = float32(cb[n*acbgM+m]) * scQ14
		}
		for i := 0; i < nGainSteps; i++ {
			gains[acbgM] = fcbgains[i]
			werrOut := acbg.werrIn + celpWnrg3(phiAll[:], gains[:]) -
				2.0*(dall[0]*gains[0]+dall[1]*gains[1]+dall[2]*gains[2])
			rd := werrOut * fcbgInvProb[i] * invProb[n]
			if rd < bestRd {
				bestRd = rd
				bestAcbgIdx = int32(n)
				bestFcbgIdx = gainIdxs[i]
			}
		}
	}
	acbIdx[rateIdx] = int16(bestAcbgIdx)
	fcbIdx[rateIdx] = int16(bestFcbgIdx)
	if fcbIdx[rateIdx] < 0 {
		fcbIdx[rateIdx] = 0
	}
	if fcbIdx[rateIdx] > int16(maxGainIdx) {
		fcbIdx[rateIdx] = int16(maxGainIdx)
	}
	return tbl.fcbgainsV[fcbIdx[rateIdx]]
}

// EncodeSubframe is the main per-subframe CELP encoder (smpl_celp_encoder).
func (e *CelpEncoder) EncodeSubframe(resLpc []float32, predcoef *[17]float32, percWghtResp, lags []float32, subfrImportance [smplCelpMaxRates]float32, fcbPulsesMax [smplCelpMaxRates]int16, surv []int16) CelpSubframeOut {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L1769-L2148
	lResp := e.percRespLen
	fcbSubfrlen := e.fcbSubfrlen
	voiced := lags[1] > 0.0

	celpFiltAr16(percWghtResp, lResp, predcoef[:], SmplLPCOrder, e.impLpcBuf)
	celpMulVecInplace(e.hanningWin, e.impLpcBuf[SmplLPCOrder:], lResp)

	impLpcRev := make([]float32, 2*smplMaxLResp-1)
	revBase := smplMaxLResp - 1
	{
		imp := e.impLpcBuf[SmplLPCOrder:]
		for i := 0; i < lResp; i++ {
			impLpcRev[revBase+i] = imp[lResp-i-1]
		}
	}
	{
		imp := append([]float32(nil), e.impLpcBuf[SmplLPCOrder:SmplLPCOrder+lResp]...)
		phi := make([]float32, smplMaxSfLen)
		e.percFiltMa(impLpcRev, revBase, lResp, imp, lResp, phi)
		celpReverse(phi, lResp)
		for i := lResp; i < fcbSubfrlen; i++ {
			phi[i] = 0.0
		}
		copy(e.phi, phi)
	}
	for i := range e.phiFlip {
		e.phiFlip[i] = 0.0
	}
	e.phiFlip[smplMaxSfLen] = e.phi[0]
	for i := 0; i < lResp+1; i++ {
		e.phiFlip[smplMaxSfLen-i] = e.phi[i]
		e.phiFlip[smplMaxSfLen+i] = e.phi[i]
	}

	resLpcPad := make([]float32, fcbSubfrlen+lResp+1)
	copy(resLpcPad[:fcbSubfrlen], resLpc[:fcbSubfrlen])
	dLpc := make([]float32, smplMaxSfLen)
	{
		cOff := smplMaxSfLen - lResp + 1
		celpMultSymtoepl2(e.phiFlip[cOff:], lResp, resLpcPad, dLpc, fcbSubfrlen)
	}

	acbg := acbgParams{acbBasisPhi: make([]float32, acbgM*fcbSubfrlen)}
	zirLpc := make([]float32, smplMaxSfLen)

	if !e.ignoreZir {
		zirTmp := make([]float32, smplMaxSfLen+smplMaxLResp-1)
		zt := smplMaxLResp - 1
		htZir := make([]float32, 2*smplMaxLResp-1)
		ht := smplMaxLResp - 1

		stateLen := SmplLPCOrder
		if lResp-1 > stateLen {
			stateLen = lResp - 1
		}
		for i := 0; i < stateLen; i++ {
			zirTmp[zt-stateLen+i] = e.stateWghtBuf[SmplLPCOrder+(fcbSubfrlen-stateLen)+i]
		}
		for nn := 0; nn < lResp; nn++ {
			res := zirTmp[zt+nn]
			for i := 0; i < 16; i++ {
				res -= predcoef[16-i] * zirTmp[zt+nn-16+i]
			}
			zirTmp[zt+nn] = res
		}
		e.percFiltMa(zirTmp, zt, lResp, percWghtResp, lResp, zirLpc)
		for i := 0; i < lResp; i++ {
			zirTmp[zt+i] = zirLpc[lResp-i-1]
		}
		for i := 0; i < lResp-1; i++ {
			zirTmp[zt-(lResp-1)+i] = 0.0
		}
		{
			imp := append([]float32(nil), e.impLpcBuf[SmplLPCOrder:SmplLPCOrder+lResp]...)
			e.percFiltMa(zirTmp, zt, lResp, imp, lResp, htZir[ht:])
		}
		celpReverse(htZir[ht:], lResp)

		if voiced {
			acbg.werrIn = celpDotProd(dLpc, resLpc, fcbSubfrlen) +
				2.0*celpDotProd(htZir[ht:], resLpc, lResp) + celpNrg(zirLpc, lResp)
		}
		for i := 0; i < lResp; i++ {
			dLpc[i] += htZir[ht+i]
		}
	} else {
		for i := 0; i < lResp; i++ {
			zirLpc[i] = 0.0
		}
		if voiced {
			acbg.werrIn = celpDotProd(dLpc, resLpc, fcbSubfrlen)
		}
	}

	acbBasis := make([]float32, smplMaxSfLen*acbgM)
	acb := make([]float32, smplMaxSfLen)
	dLtp := make([]float32, smplMaxSfLen)
	acbIdx := [smplCelpMaxRates]int16{-1, -1}

	if voiced {
		celpSynLtpBasis(lags, fcbSubfrlen/celpLagSubfrlen, e.acbState, e.acbStateLen, acbBasis)
		idx := e.calcAcbGain(lResp, acbBasis, dLpc, &acbg, dLtp)
		acbIdx[smplCelpIdxMain] = int16(idx)
		var acbGain [acbgM]float32
		celpAcbDequant(e.lowRate, int32(acbIdx[smplCelpIdxMain]), &acbGain)
		celpAcbSynthesize(fcbSubfrlen, acbBasis, &acbGain, acb)
		acbIdx[smplCelpIdxFec] = acbIdx[smplCelpIdxMain]
	}

	wtgtTmp := make([]float32, smplMaxSfLen+2*smplMaxLResp-1)
	wt := smplMaxLResp - 1
	wtgt := make([]float32, smplMaxSfLen+smplMaxLResp)
	copy(wtgtTmp[wt:wt+fcbSubfrlen], resLpc[:fcbSubfrlen])
	if voiced {
		for i := 0; i < fcbSubfrlen; i++ {
			wtgtTmp[wt+i] += -rateAcbScale * acb[i]
		}
	}
	{
		imp := append([]float32(nil), e.impLpcBuf[SmplLPCOrder:SmplLPCOrder+lResp]...)
		e.percFiltMa(wtgtTmp, wt, fcbSubfrlen+lResp, imp, lResp, wtgt)
	}
	for i := 0; i < lResp; i++ {
		wtgt[i] += zirLpc[i]
	}
	nrgWtgt := celpNrg(wtgt, fcbSubfrlen+lResp)
	var wnrgPerPulse [smplCelpMaxRates]float32
	for r := 0; r < smplCelpMaxRates; r++ {
		wnrgPerPulse[r] = nrgWtgt / (subfrImportance[r] + 1.0e-3)
	}
	iLag := int32(lags[(fcbSubfrlen/celpLagSubfrlen)-1])

	var nPulses [smplCelpMaxRates]int16
	var gainFromSearch [smplCelpMaxRates]float32
	var fcbWnrg [smplCelpMaxRates]float32
	var wnrg [smplCelpMaxRates]float32
	var pulses [smplCelpMaxRates][smplMaxPulsesPerSf]int16

	if fcbPulsesMax[smplCelpIdxMain] > 0 {
		target := dLpc
		if voiced {
			target = dLtp
		}
		useGreedy := fcbPulsesMax[smplCelpIdxMain]-1 > 0 &&
			surv[fcbPulsesMax[smplCelpIdxMain]-2] == 1 && !e.lowRate
		if useGreedy {
			e.smplFcbSearch(target, &wnrgPerPulse, &fcbPulsesMax, &pulses, &nPulses, &wnrg, &gainFromSearch, &fcbWnrg)
		} else {
			ps := float32(0.0)
			if e.lowRate {
				ps = pitchSharpeningCoef
			}
			e.smplFcbSearchDeldec(target, ps, iLag, &wnrgPerPulse, &fcbPulsesMax, surv, &pulses, &nPulses, &wnrg, &gainFromSearch, &fcbWnrg)
		}
	}

	gainIdx := [smplCelpMaxRates]int16{-1, -1}
	var fcbgain float32
	excFcb := make([]float32, smplMaxSfLen)
	tbl := getCelpTables()
	for r := 0; r < smplCelpMaxRates; r++ {
		excFcbRaw := make([]float32, smplMaxSfLen)
		celpFcbSynthesize(fcbSubfrlen, pulses[r][:], int(nPulses[r]), excFcbRaw)
		copy(excFcb[:fcbSubfrlen], excFcbRaw[:fcbSubfrlen])
		if nPulses[r] > 0 {
			if voiced {
				if e.lowRate {
					celpPitchSharp(excFcb, int(iLag), fcbSubfrlen)
				}
				fcbgain = e.calcGainsV(fcbWnrg[r], gainFromSearch[r], excFcb, dLpc, &acbg, r, &acbIdx, &gainIdx)
			} else {
				gainIdx[r] = celpQuantGainUv(gainFromSearch[r])
				fcbgain = tbl.fcbgainsUV[gainIdx[r]]
			}
			celpScaleVecInplace(excFcb, fcbSubfrlen, fcbgain)
		}
	}

	excLpc := make([]float32, fcbSubfrlen)
	copy(excLpc, excFcb[:fcbSubfrlen])
	if voiced {
		var acbGain [acbgM]float32
		celpAcbDequant(e.lowRate, int32(acbIdx[smplCelpIdxMain]), &acbGain)
		celpAcbSynthesize(fcbSubfrlen, acbBasis, &acbGain, acb)
		celpAddVecInplace(acb, excLpc, fcbSubfrlen)
	}

	copy(e.acbState[0:e.acbStateLen-fcbSubfrlen], e.acbState[fcbSubfrlen:e.acbStateLen])
	writeOff := e.acbStateLen - 2*fcbSubfrlen
	copy(e.acbState[writeOff:writeOff+fcbSubfrlen], excLpc[:fcbSubfrlen])

	if !e.ignoreZir {
		lpcResErr := make([]float32, smplMaxSfLen)
		celpSubVec(resLpc, excLpc, lpcResErr, fcbSubfrlen)
		for i := 0; i < SmplLPCOrder; i++ {
			e.stateWghtBuf[i] = e.stateErrLpcSyn[i]
		}
		celpFiltAr16(lpcResErr, fcbSubfrlen, predcoef[:], SmplLPCOrder, e.stateWghtBuf)
		for i := 0; i < SmplLPCOrder; i++ {
			e.stateErrLpcSyn[i] = e.stateWghtBuf[SmplLPCOrder+(fcbSubfrlen-SmplLPCOrder)+i]
		}
	}

	e.subfrCnt++
	if e.subfrCnt == e.subfrPerPacket {
		for r := 0; r < smplCelpMaxRates; r++ {
			e.prevAcbIdx[r] = -1
			e.prevFcbIdx[r] = -1
		}
		e.subfrCnt = 0
	} else {
		for r := 0; r < smplCelpMaxRates; r++ {
			if voiced {
				e.prevAcbIdx[r] = int32(acbIdx[r])
				e.prevFcbIdx[r] = int32(gainIdx[r])
			} else {
				e.prevAcbIdx[r] = -1
				e.prevFcbIdx[r] = -1
			}
		}
	}
	e.fcbgain = fcbgain

	nFec := int(nPulses[smplCelpIdxFec])
	if nFec < 0 {
		nFec = 0
	}
	nMain := int(nPulses[smplCelpIdxMain])
	if nMain < 0 {
		nMain = 0
	}
	pulsesFec := append([]int16(nil), pulses[smplCelpIdxFec][:nFec]...)
	pulsesMain := append([]int16(nil), pulses[smplCelpIdxMain][:nMain]...)

	return CelpSubframeOut{
		Pulses:  [smplCelpMaxRates][]int16{pulsesFec, pulsesMain},
		NPulses: nPulses,
		AcbIdx:  acbIdx,
		GainIdx: gainIdx,
		ExcLpc:  excLpc,
	}
}

// smplDistributeFcbSurv splits tot_surv survivors across pulse counts.
func smplDistributeFcbSurv(numsurv []int16, maxPulses, totSurv int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_celp.rs#L2155-L2182
	if maxPulses <= 1 {
		numsurv[0] = 1
		return
	}
	for i := 0; i < int(maxPulses); i++ {
		numsurv[i] = 1
	}
	sumSurv := maxPulses
	extraSurv := totSurv - maxPulses
	extra := extraSurv / (maxPulses - 1)
	if extra > fcbSrvMax-1 {
		extra = fcbSrvMax - 1
	}
	for i := 0; i < int(maxPulses-1); i++ {
		numsurv[i] += int16(extra)
	}
	sumSurv += extra * (maxPulses - 1)
	ix := maxPulses - 2
	for sumSurv < totSurv {
		if int32(numsurv[ix]) < fcbSrvMax {
			numsurv[ix]++
			sumSurv++
		}
		ix--
		if ix < 0 {
			break
		}
	}
}
