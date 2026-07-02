package mlow

// Excitation pulse decode (PVQ-style) for one internal frame: the total pulse
// count, the recursive split across subframes, the per-position magnitudes, and the
// signs — read straight from the range-coded bitstream against the heap-window ROM.

// smplPulseCountByte is the static gain-helper table at rodata 0xe8990, indexed by
// [config*3 + (p4+s1)]. Verbatim from the reference.
var smplPulseCountByte = [8]uint8{80, 160, 160, 16, 32, 32, 0, 0}

// Mem8Static reads the one static rodata table the pulse path needs (0xe8990..0xe8998);
// every other address reads as 0.
func Mem8Static(addr uint32) byte {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pulse.rs#L13-L19
	if addr >= 0xe8990 && addr < 0xe8998 {
		return smplPulseCountByte[addr-0xe8990]
	}
	return 0
}

// SmplPulseResult is the decoded excitation for one internal (20 ms) frame.
type SmplPulseResult struct {
	Pulses []int32  // signed pulse magnitudes per sample position (len = p2)
	Subfr  [4]int32 // per-subframe pulse counts
	// Raw entropy symbols (for the encoder to replay byte-exactly): the per-position
	// run-length magnitude symbols and the batched raw sign symbols, in read order.
	MagRuns  []int32
	SignSyms []SmplRawSym
}

// DecodeSmplPulses decodes the pulse blocks of one internal frame. p2 = frame
// samples (320), p3 = num subframes (4), p4 = regular flag (1), p6 = config (0/1),
// s1 = LSF stage-1 selector.
func DecodeSmplPulses(dec *RangeDecoder, _ *SmplMem, p2, p3, p4, p6, s1 int32) SmplPulseResult {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pulse.rs#L29-L206
	n := p2
	if n < 0 {
		n = 0
	}
	res := SmplPulseResult{Pulses: make([]int32, n)}
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_pulse.rs#L26-L185 (seed cc-table rewire: count/split/runlen from CcTables)
	cc := LoadCcTables()

	idx := p4 + s1
	bByte := int32(Mem8Static(0xe8990 + uint32(p6*3+idx)))
	frameLen4k := bByte * p2 / 320
	// ASSUMPTION: p3 (subframe count) is nonzero — always 4 on the 1:1 decode path.
	// p3==0 divides by zero exactly as the reference does (a malformed-frame crash);
	// we don't add a guard the reference lacks, to stay bit-faithful.
	subfrLen16 := frameLen4k / p3
	posPerSubfr := p2 / p3

	// --- pulse COUNT ---
	var total int32
	if p6 != 0 {
		// WB low-rate: the pulse-count CDF for this voicing class.
		total = dec.DecodeCDF(cc.NPulseCount(idx))
	} else {
		// NB (config=0, our path): a TRIANGULAR prior over [0, frame_len4k].
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
		val := dec.Decode(ft)
		limit := uint32(frameLen4k) + 1
		var prevCum uint32
		var k uint32
		for {
			if k == limit {
				break
			}
			cum := triT(k)
			// found when prevCum <= val < cum (the cumulative-triangular interval).
			if prevCum <= val && val < cum {
				dec.Update(prevCum, cum, ft)
				break
			}
			prevCum = cum
			k++
		}
		total = int32(k)
	}

	// --- recursive binary SPLIT (p3==4 path) ---
	var split [8]int32
	if total != 0 {
		sum := total - subfrLen16*2
		if sum < 0 {
			sum = 0
		}
		lo := total - 80
		if lo < 0 {
			lo = 0
		}
		if sum < lo {
			// min_split2 >= min_split assert path; treat as parse error (zeroed subframes).
			return res
		}
		hiBound := total - lo
		if sum < hiBound {
			// window the split CDF at (sum - lo); n entries from the table base.
			sum += dec.DecodeCDF(cdfWindow(cc.SplitCmf(total), int(sum-lo), int((hiBound-sum)+2)))
		}
		if sum > 0 {
			s0 := smplSplit3537(dec, cc, sum, subfrLen16)
			split[0] = s0
			split[1] = sum - s0
		}
		if sum < total {
			s2 := smplSplit3537(dec, cc, total-sum, subfrLen16)
			split[2] = s2
			split[3] = (total - sum) - s2
		}
		// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/543302e762ef36913b3e2fdf7f84510c43265272/wacore/src/voip/mlow/smpl_pulse.rs#L109-L113 (upstream corrupt-split guard)
		// C smpl_pulse_coding zeroes the whole split (and n_pulses) on a corrupt -1
		// from either half, rather than copying the sentinel into res.Subfr.
		if split[0] == -1 || split[2] == -1 {
			split = [8]int32{}
		}
	}

	take := p3
	if take < 0 {
		take = 0
	}
	if take > 4 {
		take = 4
	}
	copy(res.Subfr[:take], split[:take])

	// --- MAGNITUDE block: per-subframe run-length pulse positions ---
	posPer := posPerSubfr
	var posList []int32
	var magList []int32
	pulseIdx := int32(-1)
	for subfr := int32(0); subfr < p3; subfr++ {
		cnt := split[subfr]
		if cnt <= 0 {
			continue
		}
		basePos := posPer * subfr
		runPos := basePos
		pos := posPer
		c := cnt
		k := int32(0)
		for k < cnt {
			if pos < 0 {
				break // defensive: malformed frame must not drive a huge CDF length
			}
			oct := (pos + 7) / 8
			// window the c-pulses run-length CDF by (max_samples - pos), reading pos+1 entries.
			bucket := cc.Runlen(oct)
			start := int(bucket.MaxSamples() - pos)
			m := dec.DecodeCDF(cdfWindow(bucket.Cmf(c), start, int(pos+1)))
			res.MagRuns = append(res.MagRuns, m)
			if m > 0 || k == 0 {
				pulseIdx++
				runPos += m
				posList = append(posList, runPos)
				magList = append(magList, 1)
				pos -= m
			} else if pulseIdx >= 0 {
				magList[pulseIdx]++
			}
			c--
			k++
		}
	}

	numPos := pulseIdx + 1

	// --- SIGN block: batched uniform sign reads (1 bit per position) ---
	if numPos > 0 {
		p := int32(0)
		for p <= pulseIdx {
			nbits := numPos - p
			if nbits >= 15 {
				nbits = 15
			}
			if nbits <= 0 {
				break
			}
			sym := dec.DecodeRawSymbol(uint32(nbits))
			res.SignSyms = append(res.SignSyms, SmplRawSym{Sym: sym, Nbits: uint32(nbits)})
			bitfield := sym << uint32(16-nbits)
			end := p + nbits
			for q := p; q < end; q++ {
				sign := int32((bitfield>>14)&2) - 1 // +1 if MSB set else -1
				magList[q] *= sign
				bitfield <<= 1
			}
			p = end
		}
	}

	// scatter signed magnitudes into the pulse vector at their absolute positions.
	for i := int32(0); i < numPos; i++ {
		pp := posList[i]
		if pp >= 0 && int(pp) < len(res.Pulses) {
			res.Pulses[pp] = magList[i]
		}
	}
	return res
}

// smplSplit3537 splits count pulses across a range, returning the count assigned to
// the first half (func 3537). The split CDF now comes from the seed-built CcTables.
func smplSplit3537(dec *RangeDecoder, cc *CcTables, count, granularity int32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_pulse.rs#L208-L230
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_pulse.rs#L188-L201 (seed cc-table rewire: SplitCmf)
	lo := count
	if granularity < lo {
		lo = granularity
	}
	minSplit := count - granularity
	if minSplit < 0 {
		minSplit = 0
	}
	if lo < minSplit {
		return -1
	}
	if minSplit == lo {
		return minSplit
	}
	n := int((lo - minSplit) + 2)
	return dec.DecodeCDF(cdfWindow(cc.SplitCmf(count), int(minSplit), n)) + minSplit
}
