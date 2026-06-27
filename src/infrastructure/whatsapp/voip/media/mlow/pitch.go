package mlow

import "sync"

// Pitch / LTP parameters. The decode side (DecodeSmplPitch) reads the LTP gains and
// pitch lags from the bitstream and is the KAT-verified path; the estimator side
// (SmplPitch) is the encoder analysis and is a known soft-divergence (see datasheet).

const (
	// NumSubframes is the estimator's 8 pitch sub-blocks per 20 ms internal frame.
	NumSubframes = 8
	// MaxLTPBufLen is the perceptually-weighted speech buffer length the estimator reads.
	MaxLTPBufLen = 659
)

// ---- Decode side ----

// SmplPitchResult is the decoded LTP/pitch parameters for one internal frame.
type SmplPitchResult struct {
	GainIdx     [4]int32
	FiltIdx     [4]int32
	Lag         int32
	Contour     int32
	SampleLagQ6 [8]int32 // per-segment reconstructed pitch lag in Q6 (1/64-sample)
	NumSeg      int32
	IntLagQ6    [4]int32 // per-subframe pitch lag in Q6
	BlockLags   [8]int32 // per-40-sample-block lags (8 per 20 ms frame)
	NumSubfr    int32
}

// DecodeSmplPitch decodes the LTP gains and pitch lags. p3 = num subframes,
// p6 = config, subfrCounts = per-subframe pulse counts (from the pulse decode).
func DecodeSmplPitch(dec *RangeDecoder, mem *SmplMem, st *SmplLsfState, p2, p3, p6 int32, subfrCounts [4]int32) SmplPitchResult {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pitch.rs#L32-L198
	res := SmplPitchResult{FiltIdx: [4]int32{-1, -1, -1, -1}}
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_pitch.rs#L60-L88 (seed cc-table rewire: Group C LTP gains from CcTables; lag block below still mem)
	cc := LoadCcTables()

	// --- LTP gains loop (Group C, served from CcTables; the lag block below still
	// reads the Group-D heap window via mem). Both selects key on p6 (active WB path
	// is p6==0 HR; the p6!=0 LR variant); the filter CDFs are shared across p6.
	var gainAccum int32
	take := int(p3)
	if take > 4 {
		take = 4
	}
	for sf := 0; sf < take; sf++ {
		cnt := subfrCounts[sf]
		var gi int32
		if p6 != 0 {
			gi = dec.DecodeCDF(cc.AcbgainRowLr(st.PrevGainIdx))
		} else {
			gi = dec.DecodeCDF(cc.AcbgainRow(st.PrevGainIdx))
		}
		res.GainIdx[sf] = gi
		st.PrevGainIdx = gi

		var w0, w2 int32
		if p6 != 0 {
			w0, w2 = cc.AcbgainWeightsLr(gi)
		} else {
			w0, w2 = cc.AcbgainWeights(gi)
		}
		gainAccum += w0 + 2*w2

		if cnt > 0 {
			var fi int32
			if st.PrevFiltIdx == -1 {
				fi = dec.DecodeCDF(cc.FcbgainV())
			} else {
				fi = dec.DecodeCDF(cc.FcbgainVDelta(st.PrevFiltIdx))
			}
			res.FiltIdx[sf] = fi
			st.PrevFiltIdx = fi
		}
	}
	avgGain := gainAccum / p3 // drives the fractional-lag segment select

	// --- Lag block ---
	pcfg := mem.GClk + 0x5704
	numContours := int32(mem.U32(pcfg + 22240))
	lagCdf := mem.U32(pcfg + 22248)
	contourMap := mem.U32(pcfg + 22244)
	fracBase := mem.U32(pcfg + 22252)
	deltaCdf := mem.U32(pcfg + 22268)

	// primary lag:
	var lag int32
	if st.PrevLag < 0 {
		cnt := numContours + 1
		if cnt < 0 {
			cnt = 0
		}
		lag = dec.DecodeCDF(mem.CDFAt(lagCdf, int(cnt)))
	} else {
		di := dec.DecodeCDF(mem.CDFAt(deltaCdf+uint32(st.PrevLag)*20, 10))
		lo := int32(mem.U8(0xe7ef0 + uint32(di)*2))
		hi := int32(mem.U8(0xe7ef0 + uint32(di)*2 + 1))
		rN := (hi - lo) + 2
		if rN < 2 {
			res.Lag = -1
			return res // malformed delta interval
		}
		sym := dec.DecodeCDF(mem.CDFAt(lagCdf+uint32(lo)*2, int(rN)))
		lag = sym + lo
	}

	// contour-map search: find index where contour_map[i] == lag+1.
	target := lag + 1
	contour := int32(-1)
	for i := int32(0); i < 217; i++ {
		if int32(mem.U8(contourMap+uint32(i))) == target {
			contour = i
			break
		}
	}
	res.Lag = lag
	res.Contour = contour
	if contour < 0 || contour >= numContours {
		return res // out-of-range; stop consuming pitch bits
	}

	ctrBase := pcfg + uint32(contour)*0x44
	baseLag := mem.I32(ctrBase + 0x1d38) // contour base lag

	// (a) 64-symbol fine lag — read UNLESS prev_lag>=0 && -1 <= (base_lag-prev_lag) < 3.
	curLag2 := baseLag
	readFine := true
	if st.PrevLag >= 0 {
		delta := baseLag - st.PrevLag
		if delta >= -1 && delta < 3 {
			readFine = false
		}
	}
	var subfrW int32
	if readFine {
		sym := dec.Decode64FineSym()
		curLag2 = (baseLag << 6) + sym
		st.PrevFracLag = curLag2
		st.PrevLag = baseLag
		segLen0 := mem.I32(ctrBase + 0x1d58)
		for i := int32(0); i < segLen0; i++ {
			if subfrW < 4 {
				res.IntLagQ6[subfrW] = curLag2
			}
			if subfrW < 8 {
				res.BlockLags[subfrW] = curLag2
			}
			subfrW++
		}
		if subfrW < 4 {
			res.IntLagQ6[subfrW] = curLag2 // trailing write, subfr_w not incremented
		}
		if subfrW < 8 {
			res.BlockLags[subfrW] = curLag2
		}
	}

	// (b) fractional per-segment loop:
	cnt2 := mem.I32(ctrBase + 0x1d78)
	var segSel int32
	if avgGain >= 10007 {
		if avgGain < 14085 {
			segSel = 1
		} else {
			segSel = 2
		}
	}
	fracSegBase := fracBase + uint32(segSel)*0x280
	l3 := st.PrevFracLag
	l2 := curLag2
	startSeg := int32(0)
	if readFine {
		startSeg = 1
	}
	res.NumSeg = cnt2
	for seg := startSeg; seg < cnt2; seg++ {
		segLag := mem.I32(ctrBase + 0x1d38 + uint32(seg)*4)
		nl2 := ((l2 << 6) - l3) + ((segLag - l2) << 6)
		off := fracSegBase + uint32(nl2*2) + 0xfe
		sym := dec.DecodeCDF(mem.CDFAt(off, 65))
		l3 = sym + st.PrevFracLag + nl2
		if seg < 8 {
			res.SampleLagQ6[seg] = l3
		}
		segLen := mem.I32(ctrBase + 0x1d58 + uint32(seg)*4)
		for i := int32(0); i < segLen; i++ {
			if subfrW < 4 {
				res.IntLagQ6[subfrW] = l3
			}
			if subfrW < 8 {
				res.BlockLags[subfrW] = l3
			}
			subfrW++
		}
		l2 = segLag
		st.PrevFracLag = l3
		st.PrevLag = segLag
	}
	res.NumSubfr = subfrW
	return res
}

// ---- Estimator side ----

// PitchEstState is the per-stream estimator state (cross-frame lag-block predictor).
type PitchEstState struct {
	PrevLag       float32
	PrevPitchCorr float32
	PrevLagblk    int32
	PrevLagidx    int32
}

// ResetCond clears the cross-frame lag-block predictor (smpl_pitch_reset_cond):
// called after the last frame of a packet and after any unvoiced frame.
func (s *PitchEstState) ResetCond() {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pitch_enc.rs#L337-L341
	s.PrevLagblk = -1
	s.PrevLagidx = -1
}

// PitchResult is the pitch estimator result for one internal frame.
type PitchResult struct {
	Pitchcorr    float32
	Lags         [NumSubframes]float32
	Laginds      [NumSubframes]int32
	AvgLag       float32
	HarmStrength float32
	BlocksegIdx  int
}

// pitchBlockSeg / pitchBlockTrack mirror the reference PitchTables sub-records.
type pitchBlockSeg struct {
	Nblocks int
	Blocks  []int
	Seglens []int
}

type pitchBlockTrack struct {
	Track       [NumSubframes]int
	Meanblock   float32
	Trackdeltas float32
}

// PitchTables holds the loaded constant tables (the smpl_pitch_tables dump).
type PitchTables struct {
	Blocksegs          []pitchBlockSeg
	Blocktracks        []pitchBlockTrack
	Blocksegs2idx      []int
	BlocksegIdxCmf     []uint32
	DeltaLagCmfs       [][]uint32
	BlocksegsIx        [][2]int
	FirstblockRange    [][2]int
	BlockTransitionCmf [][]uint32
}

var (
	pitchTablesOnce sync.Once
	pitchTables     *PitchTables
)

// LoadPitchTables expands the embedded pitch seed ROM (pitch_seed.bin) into the full
// pitch tables once and returns the shared set. The expansion (range-decode of the
// blocksegs bitstream + integer DCMF→CDF) is in pitch_seed.go (buildPitchTablesFromSeed)
// and is bit-identical to the old smpl_pitch_tables.json blob.
func LoadPitchTables() *PitchTables {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L111-L158
	pitchTablesOnce.Do(func() { pitchTables = buildPitchTablesFromSeed() })
	return pitchTables
}

// pitch lag-contour wire constants (smpl_pitch_enc.rs).
const (
	pitchBlocksize    = 64 // PITCHBLOCK_MS(2) * FS_KHZ(16) * 2
	pitchNumBlocks    = 9  // (MAXPITCH_MS - MINPITCH_MS)/PITCHBLOCK_MS
	pitchNumSubframes = NumSubframes
)

// encodeLagsWire is the faithful port of C smpl_encode_lags (pEcCtx != NULL): write
// the blockseg selector + the per-40-block lag indices (laginds) to the range stream.
// This IS the voiced lag wire encode, the inverse of DecodeSmplPitch's contour
// reconstruction. prevLagblk/prevLagidx are the lag predictor (-1 at packet start /
// after a no-match); mode (0/1/2 by mean ACB gain) selects the delta-lag CMF.
func encodeLagsWire(tab *PitchTables, enc *RangeEncoder, blocksegsIx int, laginds *[NumSubframes]int32, prevLagblk, prevLagidx int32, mode int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pitch_enc.rs#L726-L799
	ixJulia := int32(tab.Blocksegs2idx[blocksegsIx])
	blocksize := int32(pitchBlocksize)
	pblockseg := &tab.Blocksegs[blocksegsIx]

	if prevLagblk < 0 {
		cmf := tab.BlocksegIdxCmf
		enc.Encode(cmf[ixJulia-1], cmf[ixJulia], cmf[len(tab.Blocksegs)])
	} else {
		cmf := tab.BlockTransitionCmf[prevLagblk]
		b0 := pblockseg.Blocks[0]
		enc.Encode(cmf[b0], cmf[b0+1], cmf[pitchNumBlocks])
		startIx := int32(tab.FirstblockRange[b0][0])
		cmfLen := int32(tab.FirstblockRange[b0][1] - tab.FirstblockRange[b0][0] + 1)
		cmf2 := tab.BlocksegIdxCmf[startIx:]
		lo := ixJulia - startIx - 1
		hi := ixJulia - startIx
		enc.Encode(cmf2[lo]-cmf2[0], cmf2[hi]-cmf2[0], cmf2[cmfLen]-cmf2[0])
	}

	blk := int32(pblockseg.Blocks[0])
	deltaBlk := blk - prevLagblk
	startSeg := 0
	lagindsIx := 0
	if !(prevLagblk > -1 && deltaBlk >= -1 && deltaBlk <= 2) {
		idxMod := uint32(laginds[lagindsIx] - blk*blocksize)
		enc.Encode(idxMod, idxMod+1, uint32(blocksize))
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
		enc.Encode(deltaLagCmf[cmfBase+ix]-p0, deltaLagCmf[cmfBase+ix+1]-p0, deltaLagCmf[cmfBase+int(blocksize)]-p0)
		prevLagblk = blk
		prevLagidx = idx
	}
}

// smplLagsPredictorAfter is the lag predictor after the voiced lag encode:
// prevLagblk = blocks[nblocks-1], prevLagidx = laginds[NumSubframes-1].
func smplLagsPredictorAfter(tab *PitchTables, blocksegsIx int, laginds *[NumSubframes]int32) (int32, int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pitch_enc.rs#L803-L811
	pblockseg := &tab.Blocksegs[blocksegsIx]
	lastBlk := int32(pblockseg.Blocks[pblockseg.Nblocks-1])
	return lastBlk, laginds[NumSubframes-1]
}

// SmplPitch (the full multi-stage estimator) is implemented in pitch_enc.go.
