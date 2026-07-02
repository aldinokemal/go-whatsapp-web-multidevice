package mlow

import "math"

// MLow pitch estimator — faithful port of smpl_pitch (smpl_pitch_enc.rs / the C
// smpl_pitch_util.c). HP-filters + 2x-downsamples the perceptually-weighted
// ltp_buf, runs an open-loop block-track survivor search at the coarse (16 kHz
// upsampled from 8 kHz) resolution, refines per-block at full resolution around
// the survivors, and folds in the rate / prev-lag / spectral-harmonicity biases.
// Only the 20 ms / 8-subframe config (the active MLow 1:1 path) is supported.
//
// Validated by pitchio_ground_truth.json: exact laginds/blockseg_idx + pitchcorr/
// avg_lag within 1e-3 + harm within the cache-aliasing tol.

const (
	peFsKhz          = 16
	peStage1FsKhz    = 8
	peCoarseFsKhz    = 16
	peTotInterpDelay = 6
	peMinpitchMs     = 2
	peMaxpitchMs     = 20
	peMinpitchLen    = peMinpitchMs * peFsKhz                        // 32
	peMaxpitchLen    = peMaxpitchMs * peFsKhz                        // 320
	peMinpitchStage1 = peMinpitchMs*peStage1FsKhz - peTotInterpDelay // 10
	peMaxpitchStage1 = peMaxpitchMs*peStage1FsKhz + peTotInterpDelay // 166

	pePitchDeltawght  float32 = 0.1439
	pePitchShortwght1 float32 = 0.04
	peSpecHarmBias    float32 = 2.5
	pePrevwght        float32 = 0.7981
	pePrevwghtSpan    float32 = 0.15
	peRatewghtHr      float32 = 0.022

	peLagSubfrlen       = 40
	peLagSubfrlenStage1 = peStage1FsKhz * peLagSubfrlen / peFsKhz // 20
	pePitchblockMs      = 2
	pePitchLookaheadLen = 7

	peDownsampDelay  = 7
	peInterpolDelayC = 4
	pePitchblock     = pePitchblockMs * peFsKhz                      // 32
	peNumLagsStage1  = peMaxpitchStage1 - peMinpitchStage1 + 1       // 157
	peNumlagsCoarse  = peCoarseFsKhz * (peMaxpitchMs - peMinpitchMs) // 288
	peNumlagsFs      = peFsKhz * (peMaxpitchMs - peMinpitchMs)       // 288
	peNumstates1     = 24
	peLowComplexity  = false
	peLowRate        = false
)

// --- filters / DSP helpers --------------------------------------------------

// pePitchHpFilter is smpl_filt_arma1 with pitch_hp_b={1,-1}, pitch_hp_a={1,-0.96},
// zero state: MA1 then AR1 in the C's 5-sample unrolled form.
func pePitchHpFilter(x []float32, out []float32) {
	n := len(x)
	var stateMa float32
	for i := 0; i < n; i++ {
		out[i] = x[i] - stateMa
		stateMa = x[i]
	}
	const ar1 float32 = 0.96
	ar12 := ar1 * ar1
	ar13 := ar1 * ar12
	ar14 := ar1 * ar13
	ar15 := ar1 * ar14
	var ytmp float32
	idx := 0
	for idx+4 < n {
		x0, x1, x2, x3, x4 := out[idx], out[idx+1], out[idx+2], out[idx+3], out[idx+4]
		out[idx+4] = x4 + ar1*x3 + ar12*x2 + ar13*x1 + ar14*x0 + ar15*ytmp
		out[idx] = x0 + ar1*ytmp
		out[idx+1] = x1 + ar1*x0 + ar12*ytmp
		out[idx+2] = x2 + ar1*x1 + ar12*x0 + ar13*ytmp
		out[idx+3] = x3 + ar1*x2 + ar12*x1 + ar13*x0 + ar14*ytmp
		ytmp = out[idx+4]
		idx += 5
	}
	for idx < n {
		ytmp = out[idx] + ytmp*ar1
		out[idx] = ytmp
		idx++
	}
}

var peDownsampFilt = [2*peDownsampDelay + 1]float32{
	-0.045472838, 0.0, 0.06366198, 0.0, -0.10610329, 0.0, 0.31830987,
	0.5, 0.31830987, 0.0, -0.10610329, 0.0, 0.06366198, 0.0, -0.045472838,
}

// pePitchDownsample is smpl_pitch_downsample: 2x decimating FIR.
func pePitchDownsample(ptrIn []float32, l int, ptrOut []float32) int {
	d := peDownsampDelay
	n := (l - 2*d) / 2
	for j := 0; j < n; j++ {
		tmp := ptrIn[2*j+d] * peDownsampFilt[d]
		for i := 0; i < d; i += 2 {
			tmp += (ptrIn[2*j+i] + ptrIn[2*j+2*d-i]) * peDownsampFilt[i]
		}
		ptrOut[j] = tmp
	}
	return n
}

var peInterpolFiltC = [2 * peInterpolDelayC]float32{
	-0.0024414062, 0.023925781, -0.119628906, 0.59814453,
	0.59814453, -0.119628906, 0.023925783, -0.0024414062,
}

// peUpsampECore: writes 2*len samples backwards; even taps copy, odd taps average.
func peUpsampECore(buf []float32, xEnd, yEnd, length int) {
	xi := xEnd
	yi := yEnd
	for k := 0; k < length; k++ {
		v := (buf[xi] + buf[xi+1]) * 0.5
		buf[yi] = v
		yi--
		buf[yi] = buf[xi]
		yi--
		xi--
	}
}

// peUpsampCCore: like upsamp_E but the interpolated sample uses the 8-tap filter.
func peUpsampCCore(buf []float32, xEnd, yEnd, length int) {
	xi := xEnd
	yi := yEnd
	for k := 0; k < length; k++ {
		var tmp float32
		for j := 0; j < peInterpolDelayC; j++ {
			a := buf[xi+j-(peInterpolDelayC-1)]
			b := buf[xi+peInterpolDelayC-j]
			tmp += (a + b) * peInterpolFiltC[j]
		}
		buf[yi] = tmp
		yi--
		buf[yi] = buf[xi]
		yi--
		xi--
	}
}

func peNrg(x []float32) float32 {
	var s float32
	for _, v := range x {
		s += v * v
	}
	return s
}

func peMaximum(x []float32) float32 {
	m := x[0]
	for _, v := range x[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// peGetMaxi is smpl_get_maxi: argmax, ties → first index (strict >).
func peGetMaxi(x []float32) int {
	bi := 0
	best := x[0]
	for n := 1; n < len(x); n++ {
		if x[n] > best {
			best = x[n]
			bi = n
		}
	}
	return bi
}

// peGetMaxiK is smpl_get_maxi_K: K highest indices in selection order (strict >, lowest-index-wins).
func peGetMaxiK(x []float32, k int) []int {
	taken := make([]bool, len(x))
	out := make([]int, 0, k)
	for c := 0; c < k; c++ {
		bi := -1
		var best float32
		for n := 0; n < len(x); n++ {
			if !taken[n] && (bi < 0 || x[n] > best) {
				best = x[n]
				bi = n
			}
		}
		if bi < 0 {
			break
		}
		taken[bi] = true
		out = append(out, bi)
	}
	return out
}

func peDotProd(a, b []float32, n int) float32 {
	var r float32
	for i := 0; i < n; i++ {
		r += a[i] * b[i]
	}
	return r
}

func peDotProd40(a, b []float32) float32 {
	var r float32
	for i := 0; i < 40; i++ {
		r += a[i] * b[i]
	}
	return r
}

// peCalcE1Inner is smpl_calc_E1: running energy of lag_subfrlen-length windows.
func peCalcE1Inner(e1, ltpbuf []float32, t int, minpitch, maxpitch, lagSubfrlen int) {
	numlags := maxpitch - minpitch + 1
	reg0 := t - minpitch
	e1[0] = maxF32(peNrg(ltpbuf[reg0:reg0+lagSubfrlen]), 1e-9)
	for i := 1; i < numlags; i++ {
		rm := ltpbuf[reg0-i]
		rs := ltpbuf[reg0+lagSubfrlen-i]
		e1[i] = maxF32(e1[i-1]+rm*rm-rs*rs, 1e-9)
	}
}

// peCalcE1 is smpl_pitch_calc_E1: per-subframe E1 via one extended E1_ then offsets.
func peCalcE1(e1, ltpbuf []float32, ltpbufLen, numsubfrs, minpitch, maxpitch, lagSubfrlen int) {
	numlags := maxpitch - minpitch + 1
	maxpitch_ := maxpitch + (numsubfrs-1)*lagSubfrlen
	numlags_ := maxpitch_ - minpitch + 1
	t := ltpbufLen - lagSubfrlen
	e1Ext := make([]float32, numlags_)
	peCalcE1Inner(e1Ext, ltpbuf, t, minpitch, maxpitch_, lagSubfrlen)
	offset := numlags_ - numlags
	for sf := 0; sf < numsubfrs; sf++ {
		for i := 0; i < numlags; i++ {
			e1[sf*numlags+i] = e1Ext[offset+i]
		}
		offset -= lagSubfrlen
	}
}

// peCalcCE2 is smpl_pitch_calc_C_E2: stage-1 cross-correlation C + target energy E2.
func peCalcCE2(c, e2, ltpbuf []float32, ltpbufLen, numsubfrs int) {
	t := ltpbufLen - peLagSubfrlenStage1*numsubfrs
	for sf := 0; sf < numsubfrs; sf++ {
		tgt := ltpbuf[t : t+20]
		reg0 := t - peMinpitchStage1
		for i := 0; i < peNumLagsStage1; i++ {
			r := ltpbuf[reg0-i : reg0-i+20]
			c[sf*peNumLagsStage1+i] = peDotProd(tgt, r, 20)
		}
		t += peLagSubfrlenStage1
		e2[sf] = maxF32(peDotProd(tgt, tgt, 20), 1e-9)
	}
}

// peUpsampEFast: in-place 2x upsample of a per-subframe E array, high subframe first.
func peUpsampEFast(buf []float32, numsubfrs int, minpitch *int, numlags *int) {
	nin := *numlags
	nout := (nin - 1) * 2
	for sf := numsubfrs - 1; sf >= 0; sf-- {
		xEnd := sf*nin + nin - 2
		yEnd := sf*nout + nout - 1
		peUpsampECore(buf, xEnd, yEnd, nin-1)
	}
	*numlags = nout
	*minpitch *= 2
}

// peUpsampCFast: in-place 2x upsample of a per-subframe C array via the interp filter.
func peUpsampCFast(buf []float32, numsubfrs int, minpitch *int, numlags *int) {
	nin := *numlags
	nout := (nin - peInterpolDelayC) * 2
	for sf := numsubfrs - 1; sf >= 0; sf-- {
		xEnd := sf*nin + nin - 1 - peInterpolDelayC
		yEnd := sf*nout + nout - 1
		peUpsampCCore(buf, xEnd, yEnd, nin-(peInterpolDelayC*2-1))
	}
	*numlags = nout
	*minpitch *= 2
}

func peSumdeltas(laginds []int32, numsubfrs int) int32 {
	var ret int32
	for i := 1; i < numsubfrs; i++ {
		d := laginds[i] - laginds[i-1]
		if d < 0 {
			d = -d
		}
		ret += d
	}
	return ret
}

// peEcEncodeBits is ec_encode_wrap with pEcCtx==NULL: -log2((fh-fl)/ft).
func peEcEncodeBits(fl, fh, ft uint32) float32 {
	p := (float32(fh) - float32(fl)) / float32(ft)
	if p <= 0.0 {
		return 0.0
	}
	return -float32(math.Log2(float64(p)))
}

// peEncodeLagsBits is smpl_encode_lags(.., pEcCtx=NULL): the bit cost used as a survivor bias.
func peEncodeLagsBits(tab *PitchTables, blocksegsIx int, laginds *[NumSubframes]int32, prevLagblk, prevLagidx int32, mode int) float32 {
	var nBits float32
	ixJulia := int32(tab.Blocksegs2idx[blocksegsIx])
	blocksize := int32(pePitchblockMs * peFsKhz * 2) // 64
	pblockseg := &tab.Blocksegs[blocksegsIx]

	if prevLagblk < 0 {
		cmf := tab.BlocksegIdxCmf
		nBits += peEcEncodeBits(cmf[ixJulia-1], cmf[ixJulia], cmf[len(tab.Blocksegs)])
	} else {
		cmf := tab.BlockTransitionCmf[prevLagblk]
		b0 := pblockseg.Blocks[0]
		nBits += peEcEncodeBits(cmf[b0], cmf[b0+1], cmf[pitchNumBlocks])
		startIx := int32(tab.FirstblockRange[b0][0])
		cmfLen := int32(tab.FirstblockRange[b0][1] - tab.FirstblockRange[b0][0] + 1)
		cmf2 := tab.BlocksegIdxCmf[startIx:]
		lo := ixJulia - startIx - 1
		hi := ixJulia - startIx
		nBits += peEcEncodeBits(cmf2[lo]-cmf2[0], cmf2[hi]-cmf2[0], cmf2[cmfLen]-cmf2[0])
	}

	blk := int32(pblockseg.Blocks[0])
	deltaBlk := blk - prevLagblk
	startSeg := 0
	lagindsIx := 0
	if !(prevLagblk > -1 && deltaBlk >= -1 && deltaBlk <= 2) {
		nBits += 6.0 // uniform first-lag cost
		prevLagblk = blk
		prevLagidx = laginds[lagindsIx]
		lagindsIx += pblockseg.Seglens[0]
		startSeg = 1
	}
	deltaLagCmf := tab.DeltaLagCmfs[mode]
	for k := startSeg; k < pblockseg.Nblocks; k++ {
		blk = int32(pblockseg.Blocks[k])
		idx := laginds[lagindsIx]
		lagindsIx += pblockseg.Seglens[k]
		deltaBlk = blk - prevLagblk
		deltaIdx := idx - prevLagidx
		prevLagidxMod := prevLagidx - prevLagblk*blocksize
		deltaRangeStart := -prevLagidxMod + deltaBlk*blocksize
		cmfBase := int(deltaRangeStart + 2*blocksize - 1)
		ix := int(deltaIdx - deltaRangeStart)
		p0 := deltaLagCmf[cmfBase]
		nBits += peEcEncodeBits(deltaLagCmf[cmfBase+ix]-p0, deltaLagCmf[cmfBase+ix+1]-p0, deltaLagCmf[cmfBase+int(blocksize)]-p0)
		prevLagblk = blk
		prevLagidx = idx
	}
	return nBits
}

// peSpectralHarmCached is spectral_harmonicity with a per-survivor cache keyed by harmonic bin.
func peSpectralHarmCached(avgLag float32, f2w *[SmplFLen]float32, cache []float32, reset bool) float32 {
	const harmUndef float32 = -10000.0
	if reset {
		for i := range cache {
			cache[i] = harmUndef
		}
	}
	invF2StepHz := 2.0 * float32(SmplFLen-1) / 16000.0
	harmHz := 16000.0 / avgLag
	harmIx := int(math.Round(float64(harmHz * 2.0 * invF2StepHz)))
	if harmIx < 0 || harmIx >= len(cache) {
		return HarmStrengthAt(avgLag, f2w)
	}
	if cache[harmIx] > harmUndef {
		return cache[harmIx]
	}
	hs := HarmStrengthAt(avgLag, f2w)
	cache[harmIx] = hs
	return hs
}

func peGetPrevLagBias(st *PitchEstState, lag float32) float32 {
	lagDiff := float32(math.Abs(float64(lag - st.PrevLag)))
	diffThres := pePrevwghtSpan * st.PrevLag
	if lagDiff < diffThres {
		return st.PrevPitchCorr * (1.0 - lagDiff/diffThres) * pePrevwght
	}
	return 0.0
}

// SmplPitch is the full pitch estimator. ltpBuf is the perceptually-weighted speech
// of length MaxLTPBufLen (last PITCH_LOOKAHEAD_LEN samples are lookahead); f2 is the
// LPC power spectrum; codedAsActiveVoice gates the search. Mutates the predictor in st.
func SmplPitch(st *PitchEstState, ltpBuf []float32, f2 *[SmplFLen]float32, codedAsActiveVoice bool) PitchResult {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pitch_enc.rs#L848-L1215
	tab := LoadPitchTables()
	numsubfrs := NumSubframes
	l := MaxLTPBufLen
	look := pePitchLookaheadLen

	if !codedAsActiveVoice {
		minLag := float32(peMinpitchMs * peFsKhz)
		st.PrevLag = 0.0
		st.PrevPitchCorr = 0.0
		st.PrevLagblk = -1
		st.PrevLagidx = -1
		res := PitchResult{Pitchcorr: 0.0, AvgLag: minLag, HarmStrength: 0.0, BlocksegIdx: 0}
		for i := 0; i < NumSubframes; i++ {
			res.Lags[i] = minLag
		}
		return res
	}

	offset := peDownsampDelay
	stage1 := make([]float32, l+offset+64)
	pePitchHpFilter(ltpBuf, stage1[offset:offset+l])
	hpLen := l - look
	ltpBufHp := make([]float32, hpLen)
	copy(ltpBufHp, stage1[offset:offset+hpLen])

	stage1Ds := make([]float32, (l+offset)/2+8)
	stage1Len := pePitchDownsample(stage1, l+offset, stage1Ds)

	numlags0 := peNumLagsStage1
	e1 := make([]float32, numlags0*numsubfrs+16)
	peCalcE1(e1, stage1Ds, stage1Len, numsubfrs, peMinpitchStage1, peMaxpitchStage1, peLagSubfrlenStage1)
	e2 := make([]float32, numsubfrs)
	cap := (2*peFsKhz/peStage1FsKhz)*peNumLagsStage1*numsubfrs + 64
	c := make([]float32, cap)
	e := make([]float32, cap)
	cStage1 := make([]float32, numlags0*numsubfrs)
	peCalcCE2(cStage1, e2, stage1Ds, stage1Len, numsubfrs)
	copy(c[:numlags0*numsubfrs], cStage1)

	numlags := numlags0
	for sf := 0; sf < numsubfrs; sf++ {
		sqrtE1 := make([]float32, numlags)
		for i := 0; i < numlags; i++ {
			sqrtE1[i] = float32(math.Sqrt(float64(e1[sf*numlags+i] + 1e-30)))
		}
		sqrtE2 := float32(math.Sqrt(float64(e2[sf] + 1e-30)))
		for i := 0; i < numlags; i++ {
			tmp := 0.5 * (sqrtE1[i] + sqrtE2)
			e[sf*numlags+i] = tmp * tmp
		}
	}

	minpitchC := peMinpitchStage1
	numlagsC := numlags
	minpitchE := peMinpitchStage1
	numlagsE := numlags
	if peLowComplexity {
		peUpsampEFast(c, numsubfrs, &minpitchC, &numlagsC)
	} else {
		peUpsampCFast(c, numsubfrs, &minpitchC, &numlagsC)
	}
	peUpsampEFast(e, numsubfrs, &minpitchE, &numlagsE)

	minpitchCoarse := peCoarseFsKhz * peMinpitchMs
	numlagsCoarse := peNumlagsCoarse
	offsetC0 := minpitchCoarse - minpitchC
	offsetE0 := minpitchCoarse - minpitchE

	h := make([]float32, numlagsCoarse*numsubfrs*2+64)
	for sf := 0; sf < numsubfrs; sf++ {
		for i := 0; i < numlagsCoarse; i++ {
			cv := c[sf*numlagsC+offsetC0+i]
			ev := e[sf*numlagsE+offsetE0+i]
			h[sf*numlagsCoarse+i] = cv / ev
		}
	}

	pitchblockCoarse := pePitchblockMs * peCoarseFsKhz // 32
	var hblk [NumSubframes][pitchNumBlocks]float32
	for sf := 0; sf < numsubfrs; sf++ {
		for block := 0; block < pitchNumBlocks; block++ {
			base := sf*numlagsCoarse + block*pitchblockCoarse
			hblk[sf][block] = peMaximum(h[base : base+pitchblockCoarse])
		}
	}

	blocksizeFs := pePitchblock * 2 // 64
	const reductionFactor float32 = 0.7
	pitchDeltawght := pePitchDeltawght / float32(blocksizeFs)
	var sfWght [NumSubframes]float32
	{
		var sumE2 float32
		for sf := 0; sf < numsubfrs; sf++ {
			sumE2 += e2[sf]
		}
		for sf := 0; sf < numsubfrs; sf++ {
			sfWght[sf] = e2[sf] / sumE2
		}
	}
	numBlocktracks := len(tab.Blocktracks)
	utils := make([]float32, numBlocktracks)
	for i := 0; i < numBlocktracks; i++ {
		bt := &tab.Blocktracks[i]
		var corr float32
		for sf := 0; sf < numsubfrs; sf++ {
			corr += hblk[sf][bt.Track[sf]] * sfWght[sf]
		}
		shortlagbias1 := (float32(peMaxpitchLen)/((bt.Meanblock+1.5)*float32(pePitchblock)) - 1.0) * pePitchShortwght1
		utils[i] = 1.0/(1.1-corr) - reductionFactor*float32(pePitchblock)*pitchDeltawght*bt.Trackdeltas + shortlagbias1
	}
	trackIdx := peGetMaxiK(utils, peNumstates1)

	e1Fs := make([]float32, numlagsE*numsubfrs+16)
	peCalcE1(e1Fs, ltpBufHp, l-look, numsubfrs, minpitchE, minpitchE+numlagsE-1, peLagSubfrlen)

	var uniqueblocks [NumSubframes]uint16
	for _, ti := range trackIdx {
		track := &tab.Blocktracks[ti].Track
		for sf := 0; sf < numsubfrs; sf++ {
			uniqueblocks[sf] |= 1 << uint(track[sf])
		}
	}

	var hThres float32
	if !peLowComplexity {
		hThres = 0.25
	}
	offsetC := peMinpitchMs*peFsKhz - minpitchC
	offsetE := peMinpitchMs*peFsKhz - minpitchE
	for sf := 0; sf < numsubfrs; sf++ {
		var mask uint16 = 1
		cPtr := offsetC + sf*numlagsC
		ePtr := offsetE + sf*numlagsE
		e1Ptr := offsetE + sf*numlagsE
		hPtr := sf * peNumlagsFs
		ltpOff := (l - look) + (sf-numsubfrs)*peLagSubfrlen
		e2sf := maxF32(peDotProd40(ltpBufHp[ltpOff:], ltpBufHp[ltpOff:]), 1e-9)
		e2[sf] = e2sf
		sqrtE2 := float32(math.Sqrt(float64(e2sf + 1e-30)))
		for block := 0; block < pitchNumBlocks; block++ {
			if uniqueblocks[sf]&mask != 0 {
				var sqrtE1 [pePitchblock + 1]float32
				for i := 0; i < pePitchblock+1; i++ {
					sqrtE1[i] = float32(math.Sqrt(float64(e1Fs[e1Ptr+block*pePitchblock+i] + 1e-30)))
				}
				for i := 0; i < pePitchblock+1; i++ {
					tmp := 0.5 * (sqrtE1[i] + sqrtE2)
					e[ePtr+block*pePitchblock+i] = 0.5 * tmp * tmp
				}
				for i := 0; i < pePitchblock; i++ {
					if h[hPtr+block*pePitchblock+i] > hThres {
						lag := peMinpitchLen + block*pePitchblock + i
						a := ltpBufHp[ltpOff:]
						b := ltpBufHp[ltpOff-lag:]
						c[cPtr+block*pePitchblock+i] = 0.5 * peDotProd40(a, b)
					}
				}
			}
			mask <<= 1
		}
	}

	strideC := pitchNumBlocks*2*pePitchblock + offsetC
	strideE := pitchNumBlocks*2*pePitchblock + offsetE
	for sf := numsubfrs - 1; sf >= 0; sf-- {
		cPtr := offsetC + sf*numlagsC
		cPtrFrac := offsetC + sf*strideC
		ePtr := offsetE + sf*numlagsE
		ePtrFrac := offsetE + sf*strideE
		hPtr := sf * 2 * pePitchblock * pitchNumBlocks
		var mask uint16 = 1 << uint(pitchNumBlocks-1)
		for block := pitchNumBlocks - 1; block >= 0; block-- {
			if uniqueblocks[sf]&mask != 0 {
				ein := ePtr + block*pePitchblock
				eout := ePtrFrac + block*2*pePitchblock
				peUpsampECore(e, ein+pePitchblock-1, eout+2*pePitchblock-1, pePitchblock)
				cin := cPtr + block*pePitchblock
				cout := cPtrFrac + block*2*pePitchblock
				if peLowComplexity {
					peUpsampECore(c, cin+pePitchblock-1, cout+2*pePitchblock-1, pePitchblock)
				} else {
					peUpsampCCore(c, cin+pePitchblock-1, cout+2*pePitchblock-1, pePitchblock)
				}
				for i := 0; i < 2*pePitchblock; i++ {
					h[hPtr+block*2*pePitchblock+i] = c[cout+i] / e[eout+i]
				}
			}
			mask >>= 1
		}
	}

	// Fine search.
	var lagindsSurv [][NumSubframes]int32
	var blocksegsIxList []int
	hComb := make([]float32, 2*pePitchblock)
	lagindCache := make(map[int32]int32)
	for _, idx := range trackIdx {
		rng := tab.BlocksegsIx[idx]
		for j := 0; j < rng[1]; j++ {
			bsx := rng[0] + j
			pblockseg := &tab.Blocksegs[bsx]
			var lagindsRow [NumSubframes]int32
			startSf := 0
			for n := 0; n < pblockseg.Nblocks; n++ {
				lookupKey := (((int32(startSf) << 3) + int32(pblockseg.Seglens[n])) << 4) | int32(pblockseg.Blocks[n])
				bestI, ok := lagindCache[lookupKey]
				if !ok {
					for v := range hComb {
						hComb[v] = 0.0
					}
					for sf := startSf; sf < startSf+pblockseg.Seglens[n]; sf++ {
						hPtr := sf*2*pePitchblock*pitchNumBlocks + pblockseg.Blocks[n]*2*pePitchblock
						for i := 0; i < 2*pePitchblock; i++ {
							hComb[i] += h[hPtr+i] * e2[sf]
						}
					}
					bestI = int32(peGetMaxi(hComb))
					lagindCache[lookupKey] = bestI
				}
				for sf := startSf; sf < startSf+pblockseg.Seglens[n]; sf++ {
					lagindsRow[sf] = bestI + int32(pblockseg.Blocks[n]*2*pePitchblock)
				}
				startSf += pblockseg.Seglens[n]
			}
			lagindsSurv = append(lagindsSurv, lagindsRow)
			blocksegsIxList = append(blocksegsIxList, bsx)
		}
	}
	nlaginds := len(lagindsSurv)

	pitchRatewght := peRatewghtHr
	if peLowRate {
		pitchRatewght = 0.028
	}
	f2w := BuildF2w(f2)
	maxIx := peGetMaxi(sfWght[:numsubfrs])
	spectralHarmCache := make([]float32, 50)

	var bestUtil, bestPitchcorr float32
	bestSurv := 0
	pitchDeltawghtFs := pePitchDeltawght / float32(blocksizeFs)

	for surv := 0; surv < nlaginds; surv++ {
		var sumC, sumE float32
		for sf := 0; sf < numsubfrs; sf++ {
			cBase := offsetC + sf*strideC
			eBase := offsetE + sf*strideE
			li := int(lagindsSurv[surv][sf])
			sumC += c[cBase+li]
			sumE += e[eBase+li]
		}
		rateBias := peEncodeLagsBits(tab, blocksegsIxList[surv], &lagindsSurv[surv], st.PrevLagblk, st.PrevLagidx, 1) * pitchRatewght
		meanLag := float32(lagindsSurv[surv][maxIx])*0.5 + float32(peMinpitchLen)
		pitchcorr := sumC / sumE
		firstLag := 0.5*float32(lagindsSurv[surv][0]) + float32(peMinpitchLen)
		prevLagBias := peGetPrevLagBias(st, firstLag)
		spectralHarmBias := peSpecHarmBias * peSpectralHarmCached(meanLag, &f2w, spectralHarmCache, surv == 0)
		util := 1.0/(1.1-pitchcorr) - pitchDeltawghtFs*float32(peSumdeltas(lagindsSurv[surv][:], numsubfrs)) + spectralHarmBias + prevLagBias - rateBias
		if surv == 0 || util > bestUtil {
			bestUtil = util
			bestSurv = surv
		}
		if surv == 0 || pitchcorr > bestPitchcorr {
			bestPitchcorr = pitchcorr
		}
	}

	var lags [NumSubframes]float32
	var lagindsOut [NumSubframes]int32
	for sf := 0; sf < numsubfrs; sf++ {
		lags[sf] = float32(lagindsSurv[bestSurv][sf])*0.5 + float32(peMinpitchLen)
		lagindsOut[sf] = lagindsSurv[bestSurv][sf]
	}
	avgLag := float32(lagindsSurv[bestSurv][maxIx])*0.5 + float32(peMinpitchLen)
	harmStrength := peSpectralHarmCached(avgLag, &f2w, spectralHarmCache, false)

	st.PrevLag = lags[numsubfrs-1]
	st.PrevPitchCorr = bestPitchcorr
	st.PrevLagidx = lagindsSurv[bestSurv][numsubfrs-1]
	st.PrevLagblk = st.PrevLagidx / int32(2*pePitchblock)

	return PitchResult{
		Pitchcorr:    bestPitchcorr,
		Lags:         lags,
		Laginds:      lagindsOut,
		AvgLag:       avgLag,
		HarmStrength: harmStrength,
		BlocksegIdx:  blocksegsIxList[bestSurv],
	}
}
