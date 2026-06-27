package mlow

import (
	"bytes"
	"compress/zlib"
	_ "embed"
	"io"
	"math/bits"
)

// Logical seed-built tables for the nrgres/gains (Group A/E), LTP gain (Group C),
// and pulse (Group B) decode — built from a small DCMF seed (cc_seed.bin) instead
// of read by absolute pointer off the old cc_blob heap window. Port of
// smpl_cc_tables.rs. CDFs are the integer dcmf_to_cmf expansion; the split/runlen
// pulse CDFs are computed from the SILK fixed-point model; the gain-reconstruction
// rodata is carried verbatim. (Group D pitch lag/contour still uses SmplMem.)

//go:embed cc_seed.bin
var ccSeedBlob []byte

const (
	ccMaxPulsesPerSf    = 40
	ccRunlengthStep     = 8
	ccNumRunlenCmfs     = 20 // SMPL_MAX_SF_LEN(160)/RUNLENGTH_STEP
	ccSplitNumTables    = ccMaxPulsesPerSf*4 - 1
	ccFcbgOffsetSteps   = 176
	ccFcbgOffsetBuckets = 4
	ccAcbgN             = 16
	ccAcbgRows          = ccAcbgN + 1
	ccFcbgVN            = 34
	ccFcbgVDeltaN       = 67
)

// --- SILK fixed-point primitives (cc-prefixed to avoid the vad.go set) ---

func ccSmulbb(a, b int32) int32 { return int32(int16(a)) * int32(int16(b)) }

func ccSmlawb(a, b, c int32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L18-L21
	return int32(int64(a) + ((int64(b) * int64(int16(c))) >> 16))
}

func ccClzFrac(in int32) (int32, int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L24-L30
	u := uint32(in)
	lz := int32(bits.LeadingZeros32(u))
	fracQ7 := int32(bits.RotateLeft32(u, -int((24-lz)&31))) & 0x7f
	return lz, fracQ7
}

func ccLin2log(inLin int32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L33-L37
	lz, fracQ7 := ccClzFrac(inLin)
	return ccSmlawb(fracQ7, fracQ7*(128-fracQ7), 179) + ((31 - lz) << 7)
}

func ccLog2lin(inLogQ7 int32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L39-L57
	if inLogQ7 < 0 {
		return 0
	}
	if inLogQ7 >= 3967 {
		return 0x7fffffff
	}
	out := int32(1) << uint(inLogQ7>>7)
	fracQ7 := inLogQ7 & 0x7f
	inner := ccSmlawb(fracQ7, ccSmulbb(fracQ7, 128-fracQ7), -174)
	if inLogQ7 < 2048 {
		out += (out * inner) >> 7
	} else {
		out += (out >> 7) * inner
	}
	return out
}

func ccSigmQ15(inQ5 int32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L59-L79
	slope := [6]int32{237, 153, 73, 30, 12, 7}
	pos := [6]int32{16384, 23955, 28861, 31213, 32178, 32548}
	neg := [6]int32{16384, 8812, 3906, 1554, 589, 219}
	if inQ5 < 0 {
		v := -inQ5
		if v >= 6*32 {
			return 0
		}
		ind := v >> 5
		return neg[ind] - ccSmulbb(slope[ind], v&0x1f)
	}
	if inQ5 >= 6*32 {
		return 32767
	}
	ind := inQ5 >> 5
	return pos[ind] + ccSmulbb(slope[ind], inQ5&0x1f)
}

// --- pulse-coding table builders (all integer/deterministic) ---

// pdfToCmf is smpl_pdf_to_CMF (maxval==-1 path): truncating-int normalize into a u16 CDF.
func pdfToCmf(pdf []int32) []uint16 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L83-L96
	n := int64(len(pdf))
	const maxval int64 = 32767
	var sump int64
	for _, x := range pdf {
		sump += int64(x)
	}
	cmf := make([]uint16, len(pdf)+1)
	for i := 0; i < len(pdf); i++ {
		p := (int64(pdf[i])*(maxval-n))/sump + 1
		cmf[i+1] = uint16(int32(cmf[i]) + int32(p))
	}
	return cmf
}

const (
	ccLog2Exp1Q15 = 47274
	ccLog22piQ14  = 43442
	ccOneQ31      = int64(1) << 31
)

func ccStirling(n int32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L101-L110
	if n == 0 {
		return 0
	}
	ret := ((n << 1) + 1) * (ccLin2log(n) << 7)
	ret -= int32(ccLog2Exp1Q15) * n
	ret += ccLog22piQ14
	return ret + ccLog2Exp1Q15/(12*n)
}

func ccProbSplitFast(k, n int32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L112-L123
	tmp := ccStirling(n) - ccStirling(k) - ccStirling(n-k) - n*(1<<15)
	if tmp == 0 {
		return 1 << 30
	}
	ret := ccLog2lin((-tmp) >> 8)
	return (1 << 30) / ret
}

func ccCreateSplitCmfs() [][]uint16 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L125-L137
	out := make([][]uint16, 0, ccSplitNumTables)
	for numPulses := int32(1); numPulses <= ccSplitNumTables; numPulses++ {
		minSplit := numPulses - ccMaxPulsesPerSf*2
		if minSplit < 0 {
			minSplit = 0
		}
		maxSplit := numPulses - minSplit
		p := make([]int32, 0, maxSplit-minSplit+1)
		for k := minSplit; k <= maxSplit; k++ {
			p = append(p, ccProbSplitFast(k, numPulses))
		}
		out = append(out, pdfToCmf(p))
	}
	return out
}

type runlenCmfs struct {
	maxSamples int32
	cmfs       [][]uint16
}

func ccCreateRunlenTable(maxSamples int32) runlenCmfs {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L141-L185
	ms := maxSamples
	cmfs := make([][]uint16, 0, ccMaxPulsesPerSf)
	for nump := int32(1); nump <= ccMaxPulsesPerSf; nump++ {
		plongerQ31 := ccOneQ31
		p := make([]int32, ms)
		for nums := int32(1); nums <= ms; nums++ {
			tmp := ccOneQ31 - (ccOneQ31 / int64(ms-nums+1))
			p1Q31 := tmp
			for r := int32(0); r < nump-1; r++ {
				p1Q31 = (p1Q31 * tmp) >> 31
			}
			p1Q31 = ccOneQ31 - p1Q31
			if p1Q31 > 2147376274 {
				p1Q31 = 2147376274
			}
			var logOutQ7 int32
			if nump > ms {
				logOutQ7 = ccLin2log((nump<<10)/ms) - 10*128
			} else {
				logOutQ7 = -(ccLin2log((ms<<10)/nump) - 10*128)
			}
			const sigmBiasQ5 = 146
			const scaleMaxQ15 = 36000
			const scaleMinQ15 = 26000
			scaleFacQ15 := int32(scaleMaxQ15) - (((scaleMaxQ15 - scaleMinQ15) * ccSigmQ15((logOutQ7>>2)+sigmBiasQ5)) >> 15)
			p1Q31 = ccOneQ31 - int64(ccLog2lin(((scaleFacQ15*(ccLin2log(int32(ccOneQ31-p1Q31))-31*128))>>15)+31*128))
			if p1Q31 > 2147376274 {
				p1Q31 = 2147376274
			}
			p[nums-1] = int32((plongerQ31 * p1Q31) >> 31)
			plongerQ31 = (plongerQ31 * (ccOneQ31 - p1Q31)) >> 31
		}
		cmfs = append(cmfs, pdfToCmf(p))
	}
	return runlenCmfs{maxSamples: ms, cmfs: cmfs}
}

func (r *runlenCmfs) MaxSamples() int32    { return r.maxSamples }
func (r *runlenCmfs) Cmf(c int32) []uint16 { return r.cmfs[c-1] }

// --- seed parse + table build ---

type ccSeed struct {
	nrgresGain4Dcmf    []byte
	nrgresShape4Dcmf   []byte
	fcbgOffsetDcmf     []byte
	acbgainsHrDcmf     []byte
	fcbgainsVDcmf      []byte
	fcbgainsVDeltaDcmf []byte
	acbgainsCbHrQ14    []int32
	gainReconBase      uint32
	gainRecon          []byte
	nPulsesDcmfBgn     []byte
	nPulsesDcmfUv      []byte
	nPulsesDcmfV       []byte
}

// protoField holds one decoded protobuf field (wiretype 0 varint or 2 bytes).
type protoField struct {
	wire   int
	varint uint64
	bytes  []byte
}

func parseProto(b []byte) map[int]protoField {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_tables_blob.rs#L26-L29
	out := make(map[int]protoField)
	i := 0
	readVarint := func() (uint64, bool) {
		var v uint64
		var shift uint
		for i < len(b) {
			c := b[i]
			i++
			v |= uint64(c&0x7f) << shift
			if c&0x80 == 0 {
				return v, true
			}
			shift += 7
		}
		return 0, false
	}
	for i < len(b) {
		key, ok := readVarint()
		if !ok {
			break
		}
		field := int(key >> 3)
		wire := int(key & 7)
		switch wire {
		case 0:
			v, ok := readVarint()
			if !ok {
				return out
			}
			out[field] = protoField{wire: 0, varint: v}
		case 2:
			ln, ok := readVarint()
			if !ok || i+int(ln) > len(b) {
				return out
			}
			out[field] = protoField{wire: 2, bytes: b[i : i+int(ln)]}
			i += int(ln)
		default:
			return out
		}
	}
	return out
}

// decodeZigzagVarints decodes a packed repeated sint32 field.
func decodeZigzagVarints(b []byte) []int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L217-L218
	var out []int32
	i := 0
	for i < len(b) {
		var v uint64
		var shift uint
		for i < len(b) {
			c := b[i]
			i++
			v |= uint64(c&0x7f) << shift
			if c&0x80 == 0 {
				break
			}
			shift += 7
		}
		out = append(out, int32(int64(v>>1)^-int64(v&1)))
	}
	return out
}

func loadCcSeed() *ccSeed {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L331-L337
	zr, err := zlib.NewReader(bytes.NewReader(ccSeedBlob))
	if err != nil {
		panic("mlow: inflate cc seed: " + err.Error())
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		panic("mlow: read cc seed: " + err.Error())
	}
	f := parseProto(raw)
	return &ccSeed{
		nrgresGain4Dcmf:    f[1].bytes,
		nrgresShape4Dcmf:   f[2].bytes,
		fcbgOffsetDcmf:     f[3].bytes,
		acbgainsHrDcmf:     f[4].bytes,
		fcbgainsVDcmf:      f[5].bytes,
		fcbgainsVDeltaDcmf: f[6].bytes,
		acbgainsCbHrQ14:    decodeZigzagVarints(f[7].bytes),
		gainReconBase:      uint32(f[8].varint),
		gainRecon:          f[9].bytes,
		nPulsesDcmfBgn:     f[10].bytes,
		nPulsesDcmfUv:      f[11].bytes,
		nPulsesDcmfV:       f[12].bytes,
	}
}

// ccDcmf is the integer dcmf→cmf (reusing the CELP port), returning a u16 CDF.
func ccDcmf(dcmf []byte) []uint16 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L83-L96
	c := make([]uint16, len(dcmf)+1)
	celpDcmfToCmf(dcmf, len(dcmf), c)
	return c
}

func ccDcmfChunks(b []byte, step int) [][]uint16 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L262-L266
	out := make([][]uint16, 0, len(b)/step)
	for i := 0; i+step <= len(b); i += step {
		out = append(out, ccDcmf(b[i:i+step]))
	}
	return out
}

// CcTables is the runtime nrgres/gains/LTP/pulse table set.
type CcTables struct {
	nrgresGain4     []uint16
	nrgresShape4    []uint16
	fcbgOffset      [][]uint16
	acbgainsHr      []uint16
	acbgainsLr      []uint16
	fcbgainsV       []uint16
	fcbgainsVDelta  []uint16
	acbgainsCbHrQ14 []int16
	acbgainsCbLrQ14 []int16
	gainRecon       []int16
	gainReconBase   uint32
	nPulseCmfs      [3][]uint16
	splitCmfs       [][]uint16
	runlen          []runlenCmfs
}

func (s *ccSeed) build() *CcTables {
	t := &CcTables{
		nrgresGain4:    ccDcmf(s.nrgresGain4Dcmf),
		nrgresShape4:   ccDcmf(s.nrgresShape4Dcmf),
		fcbgOffset:     ccDcmfChunks(s.fcbgOffsetDcmf, ccFcbgOffsetSteps),
		fcbgainsV:      ccDcmf(s.fcbgainsVDcmf),
		fcbgainsVDelta: ccDcmf(s.fcbgainsVDeltaDcmf),
		gainReconBase:  s.gainReconBase,
	}
	// acbgains HR rows (17×17, flattened), then the LR variant from the const DCMF.
	for i := 0; i+ccAcbgN <= len(s.acbgainsHrDcmf); i += ccAcbgN {
		t.acbgainsHr = append(t.acbgainsHr, ccDcmf(s.acbgainsHrDcmf[i:i+ccAcbgN])...)
	}
	for i := 0; i+ccAcbgN <= len(celpAcbgainsDcmfLR); i += ccAcbgN {
		t.acbgainsLr = append(t.acbgainsLr, ccDcmf(celpAcbgainsDcmfLR[i:i+ccAcbgN])...)
	}
	for _, x := range s.acbgainsCbHrQ14 {
		t.acbgainsCbHrQ14 = append(t.acbgainsCbHrQ14, int16(x))
	}
	t.acbgainsCbLrQ14 = cbAcbgainsLRQ14[:]
	for i := 0; i+1 < len(s.gainRecon); i += 2 {
		t.gainRecon = append(t.gainRecon, int16(uint16(s.gainRecon[i])|uint16(s.gainRecon[i+1])<<8))
	}
	t.nPulseCmfs = [3][]uint16{ccDcmf(s.nPulsesDcmfBgn), ccDcmf(s.nPulsesDcmfUv), ccDcmf(s.nPulsesDcmfV)}
	t.splitCmfs = ccCreateSplitCmfs()
	for oct := int32(1); oct <= ccNumRunlenCmfs; oct++ {
		t.runlen = append(t.runlen, ccCreateRunlenTable(oct*ccRunlengthStep))
	}
	return t
}

var ccTablesInst *CcTables

// LoadCcTables expands the embedded cc seed ROM into the nrgres/gains/LTP/pulse tables once.
func LoadCcTables() *CcTables {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L331-L337
	if ccTablesInst == nil {
		ccTablesInst = loadCcSeed().build()
	}
	return ccTablesInst
}

// --- accessors (logical-index API matching smpl_cc_tables.rs) ---

func (t *CcTables) NrgresGain4() []uint16  { return t.nrgresGain4 }
func (t *CcTables) NrgresShape4() []uint16 { return t.nrgresShape4 }

func (t *CcTables) FcbgOffset(tableIx, bucket, minOffset int) []uint16 {
	row := t.fcbgOffset[tableIx*ccFcbgOffsetBuckets+bucket]
	return row[minOffset : minOffset+92]
}

func (t *CcTables) AcbgainRow(prev int32) []uint16 {
	base := int(prev+1) * (ccAcbgN + 1)
	return t.acbgainsHr[base : base+ccAcbgN+1]
}

func (t *CcTables) AcbgainRowLr(prev int32) []uint16 {
	base := int(prev+1) * (ccAcbgN + 1)
	return t.acbgainsLr[base : base+ccAcbgN+1]
}

func (t *CcTables) AcbgainWeights(gi int32) (int32, int32) {
	i := int(gi) * 2
	return int32(t.acbgainsCbHrQ14[i]), int32(t.acbgainsCbHrQ14[i+1])
}

func (t *CcTables) AcbgainWeightsLr(gi int32) (int32, int32) {
	i := int(gi) * 2
	return int32(t.acbgainsCbLrQ14[i]), int32(t.acbgainsCbLrQ14[i+1])
}

func (t *CcTables) FcbgainV() []uint16 { return t.fcbgainsV }

func (t *CcTables) FcbgainVDelta(prevFilt int32) []uint16 {
	start := int(ccFcbgVN) - 1 - int(prevFilt)
	return t.fcbgainsVDelta[start : start+35]
}

func (t *CcTables) gainReconAt(addr uint32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_cc_tables.rs#L412-L421
	off := int(addr - t.gainReconBase)
	if off >= 0 && off%2 == 0 && off/2 < len(t.gainRecon) {
		return int32(t.gainRecon[off/2])
	}
	return 0
}

func (t *CcTables) NrgStep(cfg int32) int32 {
	return t.gainReconAt(t.gainReconBase + uint32(cfg)*2)
}

func (t *CcTables) GainRecon(p4 bool, idx int32) int32 {
	base := uint32(0xf3970)
	if p4 {
		base = 0xf35f0
	}
	return t.gainReconAt(base + uint32(idx)*2)
}

func (t *CcTables) NPulseCount(idx int32) []uint16 { return t.nPulseCmfs[idx] }

func (t *CcTables) SplitCmf(total int32) []uint16 {
	i := int(total - 1)
	if i < 0 || i >= len(t.splitCmfs) {
		return nil
	}
	return t.splitCmfs[i]
}

func (t *CcTables) Runlen(oct int32) *runlenCmfs { return &t.runlen[oct-1] }

// cdfWindow returns the n-entry CDF window base[start:start+n], zero-filling any
// out-of-range entries — the seed-table equivalent of the old mem.CDFAt zero-fill
// (RangeDecoder.decode_cdf_window in the reference).
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/rangecoder.rs#L228-L245
func cdfWindow(base []uint16, start, n int) []uint16 {
	w := make([]uint16, n)
	for i := 0; i < n; i++ {
		if j := start + i; j >= 0 && j < len(base) {
			w[i] = base[j]
		}
	}
	return w
}
