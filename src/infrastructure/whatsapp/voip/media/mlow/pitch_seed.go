package mlow

import (
	"bytes"
	"compress/zlib"
	_ "embed"
	"io"
)

// Build-from-seed for the MLow pitch runtime tables (port of smpl_pitch_seed.rs).
// The expanded PitchTables is the expansion of a small packed seed: the blocksegs
// bitstream (range-decoded), the index maps, and the DCMF arrays (integer CDF
// expansion). All integer — bit-exact with the reference. Replaces the larger
// smpl_pitch_tables.json blob.

//go:embed pitch_seed.bin
var pitchSeedBlob []byte

const (
	pitchNumBlocksegs   = 217
	pitchNumBlocktracks = 187
)

// pitchSeed mirrors tables.proto PitchSeed (7 length-delimited byte fields).
type pitchSeed struct {
	blocksegsBitstream  []byte // range-decoder source
	blocksegs2idx       []byte // [217]
	blocksegsIx         []byte // [187][2]
	firstblockRange     []byte // [9][2]
	blocksegIdxDcmf     []byte // [217]
	deltaLagDcmfs       []byte // [3][319]
	blockTransitionDcmf []byte // [9][9]
}

// parseProtoBytes reads the length-delimited (wiretype 2) fields of a protobuf
// message into field-number → bytes. The pitch/cc seeds are all byte fields.
func parseProtoBytes(b []byte) map[int][]byte {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_tables_blob.rs#L26-L29
	out := make(map[int][]byte)
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
		if wire != 2 {
			break // seeds are all length-delimited
		}
		ln, ok := readVarint()
		if !ok || i+int(ln) > len(b) {
			break
		}
		out[field] = b[i : i+int(ln)]
		i += int(ln)
	}
	return out
}

func loadPitchSeed() *pitchSeed {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L15-L31
	zr, err := zlib.NewReader(bytes.NewReader(pitchSeedBlob))
	if err != nil {
		panic("mlow: inflate pitch seed: " + err.Error())
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		panic("mlow: read pitch seed: " + err.Error())
	}
	f := parseProtoBytes(raw)
	return &pitchSeed{
		blocksegsBitstream:  f[1],
		blocksegs2idx:       f[2],
		blocksegsIx:         f[3],
		firstblockRange:     f[4],
		blocksegIdxDcmf:     f[5],
		deltaLagDcmfs:       f[6],
		blockTransitionDcmf: f[7],
	}
}

// ecDecodeUniform decodes a uniform symbol in [0, n).
func ecDecodeUniform(dec *RangeDecoder, n uint32) uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L34-L38
	v := dec.Decode(n)
	dec.Update(v, v+1, n)
	return v
}

// decodeBlockseg: len = uniform(6)+1, then len pairs of (uniform(9), uniform(4)+1).
func decodeBlockseg(dec *RangeDecoder) pitchBlockSeg {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L41-L57
	length := int(ecDecodeUniform(dec, 6) + 1)
	blocks := make([]int, length)
	seglens := make([]int, length)
	for j := 0; j < length; j++ {
		blocks[j] = int(ecDecodeUniform(dec, 9))
		seglens[j] = int(ecDecodeUniform(dec, 4) + 1)
	}
	return pitchBlockSeg{Nblocks: length, Blocks: blocks, Seglens: seglens}
}

// genBlocktracks expands each track's blockseg into per-subframe track + mean/deltas.
func genBlocktracks(blocksegs []pitchBlockSeg, blocksegsIx [][2]int) []pitchBlockTrack {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L60-L86
	out := make([]pitchBlockTrack, 0, pitchNumBlocktracks)
	for trackIdx := 0; trackIdx < pitchNumBlocktracks; trackIdx++ {
		seg := &blocksegs[blocksegsIx[trackIdx][0]]
		var track [NumSubframes]int
		segIdx := 0
		var meanblock, trackdeltas float32
		for b := 0; b < seg.Nblocks; b++ {
			for k := 0; k < seg.Seglens[b]; k++ {
				track[segIdx] = seg.Blocks[b]
				segIdx++
			}
			meanblock += float32(seg.Blocks[b] * seg.Seglens[b])
			if b != 0 {
				d := seg.Blocks[b-1] - seg.Blocks[b]
				if d < 0 {
					d = -d
				}
				trackdeltas += float32(d)
			}
		}
		meanblock /= float32(NumSubframes)
		out = append(out, pitchBlockTrack{Track: track, Meanblock: meanblock, Trackdeltas: trackdeltas})
	}
	return out
}

// pitchDcmfToCmf is the integer expansion of a DCMF to a cumulative CDF of length len+1.
func pitchDcmfToCmf(dcmf []byte) []uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L89-L109
	n := len(dcmf)
	cmf := make([]uint32, n+1)
	var sum int64
	for i := 0; i < n; i++ {
		tmp := int32(dcmf[i]) + 1
		tmp *= tmp
		if tmp > 65535 {
			tmp = 65535
		}
		cmf[i+1] = uint32(tmp)
		sum += int64(tmp)
	}
	cmf[0] = 0
	for i := 1; i <= n; i++ {
		prev := int64(cmf[i-1])
		add := int64(cmf[i])*(32767-int64(n))/sum + 1
		cmf[i] = uint32(prev + add)
	}
	return cmf
}

func chunkPairs(b []byte) [][2]int {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L119-L128
	out := make([][2]int, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		out = append(out, [2]int{int(b[i]), int(b[i+1])})
	}
	return out
}

// contourWindowParts is the pitch lag/contour heap window built from the same seed
// (port of smpl_pitch_seed.rs ContourWindowParts) — the tables Group D's pointer
// chase reads, laid out by mem.go at the fixed WASM addresses.
type contourWindowParts struct {
	records         [][2][]int // per contour: (blocks, seglens)
	contourMap      []byte     // == blocksegs2idx
	firstblockRange [][2]int
	lagCdf          []uint32   // dcmf_to_cmf(blockseg_idx_dcmf), 218
	fracCmfs        [][]uint32 // 3 × 320
	deltaCmfs       [][]uint32 // 9 × 10
}

// buildContourWindow re-decodes the blocksegs and expands the index maps + DCMFs.
func buildContourWindow() *contourWindowParts {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L178-L209
	s := loadPitchSeed()
	dec := NewRangeDecoder(s.blocksegsBitstream)
	records := make([][2][]int, 0, pitchNumBlocksegs)
	for i := 0; i < pitchNumBlocksegs; i++ {
		bs := decodeBlockseg(dec)
		records = append(records, [2][]int{bs.Blocks, bs.Seglens})
	}
	w := &contourWindowParts{
		records:         records,
		contourMap:      s.blocksegs2idx,
		firstblockRange: chunkPairs(s.firstblockRange),
		lagCdf:          pitchDcmfToCmf(s.blocksegIdxDcmf),
	}
	for i := 0; i+319 <= len(s.deltaLagDcmfs); i += 319 {
		w.fracCmfs = append(w.fracCmfs, pitchDcmfToCmf(s.deltaLagDcmfs[i:i+319]))
	}
	for i := 0; i+pitchNumBlocks <= len(s.blockTransitionDcmf); i += pitchNumBlocks {
		w.deltaCmfs = append(w.deltaCmfs, pitchDcmfToCmf(s.blockTransitionDcmf[i:i+pitchNumBlocks]))
	}
	return w
}

// buildPitchTablesFromSeed expands the embedded seed into the full PitchTables.
func buildPitchTablesFromSeed() *PitchTables {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_pitch_seed.rs#L111-L157
	s := loadPitchSeed()
	dec := NewRangeDecoder(s.blocksegsBitstream)
	blocksegs := make([]pitchBlockSeg, 0, pitchNumBlocksegs)
	for i := 0; i < pitchNumBlocksegs; i++ {
		blocksegs = append(blocksegs, decodeBlockseg(dec))
	}
	blocksegsIx := chunkPairs(s.blocksegsIx)
	firstblockRange := chunkPairs(s.firstblockRange)
	blocktracks := genBlocktracks(blocksegs, blocksegsIx)

	blocksegs2idx := make([]int, len(s.blocksegs2idx))
	for i, x := range s.blocksegs2idx {
		blocksegs2idx[i] = int(x)
	}
	blocksegIdxCmf := pitchDcmfToCmf(s.blocksegIdxDcmf)
	deltaLagCmfs := make([][]uint32, 0, 3)
	for i := 0; i+319 <= len(s.deltaLagDcmfs); i += 319 {
		deltaLagCmfs = append(deltaLagCmfs, pitchDcmfToCmf(s.deltaLagDcmfs[i:i+319]))
	}
	blockTransitionCmf := make([][]uint32, 0, pitchNumBlocks)
	for i := 0; i+pitchNumBlocks <= len(s.blockTransitionDcmf); i += pitchNumBlocks {
		blockTransitionCmf = append(blockTransitionCmf, pitchDcmfToCmf(s.blockTransitionDcmf[i:i+pitchNumBlocks]))
	}

	return &PitchTables{
		Blocksegs:          blocksegs,
		Blocktracks:        blocktracks,
		Blocksegs2idx:      blocksegs2idx,
		BlocksegIdxCmf:     blocksegIdxCmf,
		DeltaLagCmfs:       deltaLagCmfs,
		BlocksegsIx:        blocksegsIx,
		FirstblockRange:    firstblockRange,
		BlockTransitionCmf: blockTransitionCmf,
	}
}
