package mlow

import "math/bits"

// SILK VAD (smpl_vad.c): per-internal-frame speech-activity probability and the
// coded_as_active_voice flag. Faithful fixed-point port of smpl_VAD_GetSA_Q8_c +
// GetNoiseLevels + the 2-band allpass filterbank + the per-packet hangover. Runs on
// raw int16 input PCM at 16 kHz, 320 samples per internal frame.
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_vad.rs#L1-L538

// ---- SILK fixed-point primitives ----

const (
	silkInt32Max = int32(0x7FFFFFFF)
	// silkInt16Max is defined in lpc.go (32767); reused here.
	silkInt16Min = int32(-0x8000)
	silkUint8Max = int32(0xFF)
)

func sat16(a int32) int32 {
	if a < silkInt16Min {
		return silkInt16Min
	}
	if a > silkInt16Max {
		return silkInt16Max
	}
	return a
}

func smulwb(a, b int32) int32 { return int32((int64(a) * int64(int16(b))) >> 16) }
func smlawb(a, b, c int32) int32 {
	return int32(int64(a) + ((int64(b) * int64(int16(c))) >> 16))
}
func smulww(a, b int32) int32 { return int32((int64(a) * int64(b)) >> 16) }
func smulbb(a, b int32) int32 { return int32(int16(a)) * int32(int16(b)) }
func smlabb(a, b, c int32) int32 {
	return a + int32(int16(b))*int32(int16(c))
}

func addPosSat32(a, b int32) int32 {
	if (uint32(a)+uint32(b))&0x80000000 != 0 {
		return silkInt32Max
	}
	return int32(uint32(a) + uint32(b))
}

func div32(a, b int32) int32 { return a / b }

func clz32(x int32) int32 { return int32(bits.LeadingZeros32(uint32(x))) }

// ror32: rotate right by rot (RotateLeft32 with -k rotates right).
func ror32(a32, rot int32) int32 {
	return int32(bits.RotateLeft32(uint32(a32), -int(rot&31)))
}

func clzFrac(inp int32) (int32, int32) {
	lz := clz32(inp)
	fracQ7 := ror32(inp, 24-lz) & 0x7f
	return lz, fracQ7
}

// lin2log: approximation of 128 * log2().
func lin2log(inLin int32) int32 {
	lz, fracQ7 := clzFrac(inLin)
	return smlawb(fracQ7, fracQ7*(128-fracQ7), 179) + ((31 - lz) << 7)
}

func sqrtApprox(x int32) int32 {
	if x <= 0 {
		return 0
	}
	lz, fracQ7 := clzFrac(x)
	var y int32
	if lz&1 != 0 {
		y = 32768
	} else {
		y = 46214
	}
	y >>= lz >> 1
	return smlawb(y, y, smulbb(213, fracQ7))
}

// sigmQ15: piecewise-linear sigmoid approximation.
func sigmQ15(inQ5 int32) int32 {
	slope := [6]int32{237, 153, 73, 30, 12, 7}
	pos := [6]int32{16384, 23955, 28861, 31213, 32178, 32548}
	neg := [6]int32{16384, 8812, 3906, 1554, 589, 219}
	if inQ5 < 0 {
		inQ5 = -inQ5
		if inQ5 >= 6*32 {
			return 0
		}
		ind := inQ5 >> 5
		return neg[ind] - smulbb(slope[ind], inQ5&0x1F)
	}
	if inQ5 >= 6*32 {
		return 32767
	}
	ind := inQ5 >> 5
	return pos[ind] + smulbb(slope[ind], inQ5&0x1F)
}

// rshiftRound: silk_RSHIFT_ROUND.
func rshiftRound(a, shift int32) int32 {
	if shift == 1 {
		return (a >> 1) + (a & 1)
	}
	return ((a >> (shift - 1)) + 1) >> 1
}

// ---- VAD constants ----

const (
	vadNBands                  = 4
	vadInternalSubframesLog2   = 2
	vadInternalSubframes       = 1 << vadInternalSubframesLog2
	vadNoiseLevelSmoothCoefQ16 = 1024
	vadNoiseLevelsBias         = 50
	vadNegativeOffsetQ5        = 128
	vadSnrFactorQ16            = 45000
	aFB120                     = 3894 << 1
	aFB121                     = -29322
	speechActivityDtxThresQ8   = 12 // SILK_FIX_CONST(0.05, 8)
)

var tiltWeights = [vadNBands]int32{30000, 6000, -12000, -12000}

// SmplVadState is the persistent SILK VAD state, carried across packets.
type SmplVadState struct {
	anaState             [2]int32
	anaState1            [2]int32
	anaState2            [2]int32
	xnrgSubfr            [vadNBands]int32
	nl                   [vadNBands]int32
	invNl                [vadNBands]int32
	noiseLevelBias       [vadNBands]int32
	counter              int32
	hpState              int32
	noiseLvlUpdateSpeed  int32
	nonBinariness        int32
	highpassSharpness    int32
	remainingDtxHangover int32
	hangoverMs           int32
}

type vadType int

const (
	vadActive vadType = iota
	vadInactive
	vadHangover
)

// VadPacketResult is the VAD output for one 60 ms packet.
type VadPacketResult struct {
	VadResults         [3]float32
	CodedAsActiveVoice bool
}

// NewSmplVadState initializes the VAD (smpl_VAD_Init).
func NewSmplVadState() *SmplVadState {
	s := &SmplVadState{counter: 15, remainingDtxHangover: 60, hangoverMs: 60}
	for b := 0; b < vadNBands; b++ {
		bias := vadNoiseLevelsBias / (int32(b) + 1)
		if bias < 1 {
			bias = 1
		}
		s.noiseLevelBias[b] = bias
		s.nl[b] = 100 * bias
		s.invNl[b] = silkInt32Max / s.nl[b]
	}
	return s
}

// filtHP: first-order ARMA HP filter with zero at DC, in place over len samples.
func (s *SmplVadState) filtHP(x []int32, bQ16, aNegQ16 int32, length int) {
	for i := 0; i < length; i++ {
		inval := smulwb(bQ16, x[i])
		outval := sat16(s.hpState - inval)
		s.hpState = smlawb(inval, aNegQ16, outval)
		x[i] = outval
	}
}

// anaFiltBank1: 2-band split via first-order allpass filters. Writes low band to
// outL[0..n/2] and high band to outH[0..n/2]; s is the carried 2-element state.
func anaFiltBank1(inp []int32, s *[2]int32, outL, outH []int32, n int) {
	n2 := n >> 1
	for k := 0; k < n2; k++ {
		in32 := inp[2*k] << 10
		y := in32 - s[0]
		x := smlawb(y, y, aFB121)
		out1 := s[0] + x
		s[0] = in32 + x

		in32 = inp[2*k+1] << 10
		y = in32 - s[1]
		x = smulwb(y, aFB120)
		out2 := s[1] + x
		s[1] = in32 + x

		outL[k] = sat16(rshiftRound(out2+out1, 11))
		outH[k] = sat16(rshiftRound(out2-out1, 11))
	}
}

// anaFiltBank1Inplace: in-place 2-band split — reads x[0..n], writes low band to
// x[0..n/2] and high band to x[hiOff..hiOff+n/2].
func anaFiltBank1Inplace(x []int32, hiOff int, s *[2]int32, n int) {
	n2 := n >> 1
	for k := 0; k < n2; k++ {
		in32 := x[2*k] << 10
		y := in32 - s[0]
		xx := smlawb(y, y, aFB121)
		out1 := s[0] + xx
		s[0] = in32 + xx

		in32 = x[2*k+1] << 10
		y = in32 - s[1]
		xx = smulwb(y, aFB120)
		out2 := s[1] + xx
		s[1] = in32 + xx

		x[hiOff+k] = sat16(rshiftRound(out2-out1, 11))
		x[k] = sat16(rshiftRound(out2+out1, 11))
	}
}

// getNoiseLevels: smpl_VAD_GetNoiseLevels.
func (s *SmplVadState) getNoiseLevels(pX *[vadNBands]int32) {
	var minCoef int32
	if s.counter < 1000 {
		minCoef = div32(silkInt16Max, (s.counter>>4)+1)
		s.counter++
	}
	for b := 0; b < vadNBands; b++ {
		nl := s.nl[b]
		nrg := addPosSat32(pX[b], s.noiseLevelBias[b])
		invNrg := div32(silkInt32Max, nrg)
		var coef int32
		switch {
		case nrg > (nl << 3):
			coef = vadNoiseLevelSmoothCoefQ16 >> 3
		case nrg < nl:
			coef = vadNoiseLevelSmoothCoefQ16
		default:
			coef = smulwb(smulww(invNrg, nl), vadNoiseLevelSmoothCoefQ16<<1)
		}
		coef = (coef * (100 + s.noiseLvlUpdateSpeed)) / 100
		if coef < minCoef {
			coef = minCoef
		}
		s.invNl[b] = smlawb(s.invNl[b], invNrg-s.invNl[b], coef)
		v := div32(silkInt32Max, s.invNl[b])
		if v > 0x00FFFFFF {
			v = 0x00FFFFFF
		}
		s.nl[b] = v
	}
}

// getSAQ8: smpl_VAD_GetSA_Q8_c — speech_activity_Q8 for one framelen-sample frame.
func (s *SmplVadState) getSAQ8(pIn []int32, framelen int) int32 {
	decFl1 := framelen >> 1
	decFl2 := framelen >> 2
	decFl3 := framelen >> 3

	var xOffset [vadNBands]int
	xOffset[0] = 0
	xOffset[1] = decFl3 + decFl2
	xOffset[2] = xOffset[1] + decFl3
	xOffset[3] = xOffset[2] + decFl2
	xTotal := xOffset[3] + decFl1
	x := make([]int32, xTotal)

	anaFiltBank1(pIn, &s.anaState, x[:xOffset[3]], x[xOffset[3]:], framelen)
	anaFiltBank1Inplace(x, xOffset[2], &s.anaState1, decFl1)
	anaFiltBank1Inplace(x, xOffset[1], &s.anaState2, decFl2)

	// HP filter on the lowest band, -3 dB @ 66 Hz.
	aNegQ16 := int32(53084)
	aNegQ16 = (aNegQ16 * (100 - s.highpassSharpness)) / 100
	bQ16 := (65536 + aNegQ16) / 2
	s.filtHP(x[:decFl3], bQ16, aNegQ16, decFl3)

	// Energy in each band.
	var xnrg [vadNBands]int32
	for b := 0; b < vadNBands; b++ {
		shift := vadNBands - b
		if shift > vadNBands-1 {
			shift = vadNBands - 1
		}
		dec := framelen >> shift
		decSubfrLen := dec >> vadInternalSubframesLog2
		decSubfrOffset := 0
		xnrg[b] = s.xnrgSubfr[b]
		var sumSquared int32
		for sub := 0; sub < vadInternalSubframes; sub++ {
			sumSquared = 0
			for i := 0; i < decSubfrLen; i++ {
				xTmp := x[xOffset[b]+i+decSubfrOffset] >> 3
				sumSquared = smlabb(sumSquared, xTmp, xTmp)
			}
			if sub < vadInternalSubframes-1 {
				xnrg[b] = addPosSat32(xnrg[b], sumSquared)
			} else {
				xnrg[b] = addPosSat32(xnrg[b], sumSquared>>1)
			}
			decSubfrOffset += decSubfrLen
		}
		s.xnrgSubfr[b] = sumSquared
	}

	s.getNoiseLevels(&xnrg)

	// Signal-plus-noise to noise ratio.
	var sumSquared int32
	var inputTilt int32
	for b := 0; b < vadNBands; b++ {
		speechNrg := xnrg[b] - s.nl[b]
		if speechNrg > 0 {
			var ratioQ8 int32
			if (xnrg[b] & -0x00800000) == 0 { // 0xFF800000 as int32
				ratioQ8 = div32(xnrg[b]<<8, s.nl[b]+1)
			} else {
				ratioQ8 = div32(xnrg[b], (s.nl[b]>>8)+1)
			}
			snrQ7 := lin2log(ratioQ8) - 8*128
			sumSquared = smlabb(sumSquared, snrQ7, snrQ7)
			if speechNrg < (1 << 20) {
				snrQ7 = smulwb(sqrtApprox(speechNrg)<<6, snrQ7)
			}
			inputTilt = smlawb(inputTilt, tiltWeights[b], snrQ7)
		}
	}
	sumSquared = div32(sumSquared, vadNBands)
	pSnrDbQ7 := int32(int16(3 * sqrtApprox(sumSquared)))

	vadSnrFactorQ16 := (int32(vadSnrFactorQ16) * (150 - s.nonBinariness)) / 150
	saQ15 := sigmQ15(smulwb(vadSnrFactorQ16, pSnrDbQ7) - vadNegativeOffsetQ5)

	_ = inputTilt
	r := saQ15 >> 7
	if r > silkUint8Max {
		r = silkUint8Max
	}
	return r
}

// ProcessPacket processes one 60 ms packet (3 internal frames of framelen int16 samples).
func (s *SmplVadState) ProcessPacket(pcmI16 []int16, framelen int) VadPacketResult {
	const framesPerPacket = 3
	const packetMs = 60
	var vadResults [3]float32
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/543302e762ef36913b3e2fdf7f84510c43265272/wacore/src/voip/mlow/smpl_vad.rs#L406-L412 (upstream short-packet guard)
	// Reject a short capture buffer up front so the fixed-stride frame loop can't
	// index out of range (mirrors the C VAD's short-packet guard).
	if len(pcmI16) < framesPerPacket*framelen {
		return VadPacketResult{}
	}
	var vt [3]vadType
	for i := 0; i < framesPerPacket; i++ {
		t := i * framelen
		frame := make([]int32, framelen)
		for j := 0; j < framelen; j++ {
			frame[j] = int32(pcmI16[t+j])
		}
		saQ8 := s.getSAQ8(frame, framelen)
		vadResults[i] = float32(saQ8) / 256.0
		if saQ8 > speechActivityDtxThresQ8 {
			vt[i] = vadActive
		} else {
			vt[i] = vadInactive
		}
	}

	codedAsActiveVoice := false
	for i := range vt {
		if vt[i] == vadActive {
			s.remainingDtxHangover = s.hangoverMs
		} else if s.remainingDtxHangover > 0 {
			vt[i] = vadHangover
			s.remainingDtxHangover -= packetMs / framesPerPacket
		}
		if vt[i] != vadInactive {
			codedAsActiveVoice = true
		}
	}

	return VadPacketResult{VadResults: vadResults, CodedAsActiveVoice: codedAsActiveVoice}
}
