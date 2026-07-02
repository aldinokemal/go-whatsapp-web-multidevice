package mlow

import (
	"bytes"
	"compress/zlib"
	_ "embed"
	"encoding/binary"
	"io"
	"math"
	"sync"
)

// Build-from-seed for the MLow LSF runtime tables. The expanded LSF tables
// (SmplSynthTables, SmplTables, LsfCb) are the expansion of one small packed ROM
// (lsf_seed.bin), so we store the ROM and rerun the init at load instead of
// committing the pre-expanded f32. The float op order here is load-bearing
// (matmul accumulation, sqrt-then-reciprocal in rotApplyWght, integer truncation
// in lsfDcmfToCmf, scalar unpack8) so the rebuilt tables are bit-faithful.
//
// min_spacing and lsf_extra are NOT separate ROM: min_spacing[v] is min_dist[1-v],
// and lsf_extra is the extra-symbol selector CDF carried in the seed.

// lsfSeedBlob is the packed LSF ROM (zlib-compressed tables.proto LsfSeed),
// expanded at load — mirrors pitch_seed.bin / cc_seed.bin.
//
//go:embed lsf_seed.bin
var lsfSeedBlob []byte

const (
	lsfOrder     = SmplLPCOrder // 16
	lsfCentroids = LSFCBCentroids
	lsfCinvLen   = lsfOrder * (lsfOrder + 1) / 2 // 136
	lsfST2Len    = 9593                          // LSF_ST2_ALL_QLVLS_LEN
)

// Per-voiced (index 0 = unvoiced, 1 = voiced) scale/min constants.
var (
	lsfCBMin        = [2]float32{-0.5873778, -0.24721986}
	lsfCBScale      = [2]float32{1.3145164e-5, 7.226229e-6}
	lsfCinvMin      = [2]float32{-3.5960955e-5, -2.778548e-5}
	lsfCinvScale    = [2]float32{1.8589316e-9, 1.2180106e-9}
	lsfRotMin       = [2]float32{-0.9124832, -0.8455929}
	lsfRotScale     = [2]float32{0.006554049, 0.0069253775}
	lsfRotCondMin   = [2]float32{-0.67291605, -0.8248211}
	lsfRotCondScale = [2]float32{0.0052386564, 0.0064186584}
)

const (
	lsfST2QlvlsMin   = float32(-0.45)
	lsfST2QlvlsScale = float32(0.0034478905)
)

// lsfSeed is the packed ROM reshaped into the nested arrays the expansion indexes.
// Outer index [voiced] (0 = unvoiced, 1 = voiced).
type lsfSeed struct {
	cb16      [2][lsfCentroids][lsfOrder]uint16
	cinv16    [2][lsfCinvLen]uint16
	rot8      [2][lsfCentroids][lsfOrder][lsfOrder]byte
	rotCond8  [2][2][lsfOrder][lsfOrder]byte
	mean      [2][lsfOrder]float32
	cmf       [2][17]uint16
	cmfCond   [2][18]uint16
	minDist   [2][17]float32
	regCond   [2]float32
	minQi     [2][2][17][lsfOrder]int8
	maxQi     [2][2][17][lsfOrder]int8
	qstep     [2][2]float32
	st2Qlvls8 []byte // [9593]
	st2Dcmfs  []byte // [9593]
	lsfSel    [3][3]uint16
	lsfExtra  [3]uint16
}

// decodeVarintsU32 decodes a packed repeated uint32 protobuf field (plain varints).
func decodeVarintsU32(b []byte) []uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L53-L64
	var out []uint32
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
		out = append(out, uint32(v))
	}
	return out
}

// decodeFloats decodes a packed repeated float protobuf field (fixed32 little-endian).
func decodeFloats(b []byte) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L65-L72
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// loadLsfSeed inflates and parses the packed ROM into the nested seed arrays
// (the reference's LsfSeed::reshape, expressed as fixed-shape fills).
func loadLsfSeed() *lsfSeed {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L138-L208
	zr, err := zlib.NewReader(bytes.NewReader(lsfSeedBlob))
	if err != nil {
		panic("mlow: inflate lsf seed: " + err.Error())
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		panic("mlow: read lsf seed: " + err.Error())
	}
	f := parseProto(raw)
	s := &lsfSeed{}

	// rot_8 [2][16][16][16] u8 (flat row-major).
	p := 0
	for v := 0; v < 2; v++ {
		for c := 0; c < lsfCentroids; c++ {
			for i := 0; i < lsfOrder; i++ {
				for j := 0; j < lsfOrder; j++ {
					s.rot8[v][c][i][j] = f[1].bytes[p]
					p++
				}
			}
		}
	}
	// rot_cond_8 [2][2][16][16] u8.
	p = 0
	for v := 0; v < 2; v++ {
		for lr := 0; lr < 2; lr++ {
			for i := 0; i < lsfOrder; i++ {
				for j := 0; j < lsfOrder; j++ {
					s.rotCond8[v][lr][i][j] = f[2].bytes[p]
					p++
				}
			}
		}
	}
	s.st2Qlvls8 = append([]byte(nil), f[3].bytes...)
	s.st2Dcmfs = append([]byte(nil), f[4].bytes...)
	// st2_min_qi / st2_max_qi [2][2][17][16] i8.
	for idx, src := range [][]byte{f[5].bytes, f[6].bytes} {
		p = 0
		for v := 0; v < 2; v++ {
			for lr := 0; lr < 2; lr++ {
				for c := 0; c < 17; c++ {
					for i := 0; i < lsfOrder; i++ {
						q := int8(src[p])
						if idx == 0 {
							s.minQi[v][lr][c][i] = q
						} else {
							s.maxQi[v][lr][c][i] = q
						}
						p++
					}
				}
			}
		}
	}
	// cb_16 [2][16][16] (u32 -> u16).
	cb := decodeVarintsU32(f[7].bytes)
	p = 0
	for v := 0; v < 2; v++ {
		for c := 0; c < lsfCentroids; c++ {
			for i := 0; i < lsfOrder; i++ {
				s.cb16[v][c][i] = uint16(cb[p])
				p++
			}
		}
	}
	// cinv_16 [2][136].
	cinv := decodeVarintsU32(f[8].bytes)
	p = 0
	for v := 0; v < 2; v++ {
		for i := 0; i < lsfCinvLen; i++ {
			s.cinv16[v][i] = uint16(cinv[p])
			p++
		}
	}
	// cmf [2][17].
	cmf := decodeVarintsU32(f[9].bytes)
	p = 0
	for v := 0; v < 2; v++ {
		for i := 0; i < 17; i++ {
			s.cmf[v][i] = uint16(cmf[p])
			p++
		}
	}
	// cmf_cond [2][18].
	cmfc := decodeVarintsU32(f[10].bytes)
	p = 0
	for v := 0; v < 2; v++ {
		for i := 0; i < 18; i++ {
			s.cmfCond[v][i] = uint16(cmfc[p])
			p++
		}
	}
	// lsf_sel [3][3].
	sel := decodeVarintsU32(f[11].bytes)
	p = 0
	for a := 0; a < 3; a++ {
		for b := 0; b < 3; b++ {
			s.lsfSel[a][b] = uint16(sel[p])
			p++
		}
	}
	// lsf_extra [3].
	ex := decodeVarintsU32(f[12].bytes)
	for i := 0; i < 3; i++ {
		s.lsfExtra[i] = uint16(ex[i])
	}
	// mean [2][16].
	mean := decodeFloats(f[13].bytes)
	p = 0
	for v := 0; v < 2; v++ {
		for i := 0; i < lsfOrder; i++ {
			s.mean[v][i] = mean[p]
			p++
		}
	}
	// min_dist [2][17].
	md := decodeFloats(f[14].bytes)
	p = 0
	for v := 0; v < 2; v++ {
		for i := 0; i < 17; i++ {
			s.minDist[v][i] = md[p]
			p++
		}
	}
	// reg_cond [2].
	rc := decodeFloats(f[15].bytes)
	s.regCond[0], s.regCond[1] = rc[0], rc[1]
	// qstep [2][2].
	qs := decodeFloats(f[16].bytes)
	p = 0
	for v := 0; v < 2; v++ {
		for i := 0; i < 2; i++ {
			s.qstep[v][i] = qs[p]
			p++
		}
	}
	return s
}

// ---- float expansion primitives (op order is load-bearing) ----

// lsfMatMultTransp16: transposed 16x16 matrix-vector multiply,
// y[i] = sum_j C[j][i] * x[j] (accumulate seeded at j=0, then += for j>0).
func lsfMatMultTransp16(c *[lsfOrder][lsfOrder]float32, x *[lsfOrder]float32) [lsfOrder]float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L210-L225
	var y [lsfOrder]float32
	x0 := x[0]
	for i := 0; i < lsfOrder; i++ {
		y[i] = c[0][i] * x0
	}
	for j := 1; j < lsfOrder; j++ {
		xj := x[j]
		for i := 0; i < lsfOrder; i++ {
			// Round the product before accumulating: Go would otherwise fuse
			// `y[i] + c*xj` into an FMA (one rounding), but the reference rounds
			// the multiply and the add separately.
			prod := float32(c[j][i] * xj)
			y[i] += prod
		}
	}
	return y
}

// lsfSeedLaroia: Laroia inverse-gap LSF weights, with the gap floored at 1e-3.
func lsfSeedLaroia(lsf *[lsfOrder]float32) [lsfOrder]float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L227-L242
	const minDist = float32(1e-3)
	var inv [lsfOrder + 1]float32
	inv[0] = 1.0 / maxF32(lsf[0], minDist)
	for i := 1; i < lsfOrder; i++ {
		inv[i] = 1.0 / maxF32(lsf[i]-lsf[i-1], minDist)
	}
	inv[lsfOrder] = 1.0 / maxF32(smplPi-lsf[lsfOrder-1], minDist)
	var w [lsfOrder]float32
	for i := 0; i < lsfOrder; i++ {
		w[i] = inv[i] + inv[i+1]
	}
	return w
}

// lsfRotApplyWght: apply the Laroia weights to the rotation. lsfw = sqrt(laroia(lsf)),
// we[i][j] = rot[i][j]/lsfw[j], wie[j][i] = rot[i][j]*lsfw[j].
func lsfRotApplyWght(rot *[lsfOrder][lsfOrder]float32, lsf *[lsfOrder]float32) (we, wie [lsfOrder][lsfOrder]float32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L244-L267
	lsfw := lsfSeedLaroia(lsf)
	for i := range lsfw {
		lsfw[i] = sqrtF32(lsfw[i])
	}
	var lsfwInv [lsfOrder]float32
	for i := 0; i < lsfOrder; i++ {
		lsfwInv[i] = 1.0 / lsfw[i]
	}
	for i := 0; i < lsfOrder; i++ {
		for j := 0; j < lsfOrder; j++ {
			we[i][j] = rot[i][j] * lsfwInv[j]
			wie[j][i] = rot[i][j] * lsfw[j]
		}
	}
	return
}

// lsfCmfToBits: per-symbol bit cost, bits[i] = -log2f((cmf[i+1]-cmf[i]) / cmf[len-1]).
func lsfCmfToBits(cmf []uint16) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L269-L279
	n := len(cmf)
	den := float32(cmf[n-1])
	bits := make([]float32, n-1)
	for i := 0; i < n-1; i++ {
		num := float32(int32(cmf[i+1]) - int32(cmf[i]))
		bits[i] = -log2F32(num / den)
	}
	return bits
}

// lsfDcmfToCmf: integer expansion of a delta-CMF to a cumulative u16 CDF of length len+1.
func lsfDcmfToCmf(dcmf []byte) []uint16 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L281-L302
	n := len(dcmf)
	cmf := make([]uint16, n+1)
	var sum int64
	for i := 0; i < n; i++ {
		tmp := int32(dcmf[i]) + 1
		tmp *= tmp
		if tmp > 65535 {
			tmp = 65535
		}
		cmf[i+1] = uint16(tmp)
		sum += int64(tmp)
	}
	cmf[0] = 0
	for i := 1; i < n+1; i++ {
		prev := int64(cmf[i-1])
		add := int64(cmf[i])*int64(32767-n)/sum + 1
		cmf[i] = uint16(prev + add)
	}
	return cmf
}

// lsfUnpack8: out[i][j] = min + packed[i][j]*scale, scalar.
func lsfUnpack8(packed *[lsfOrder][lsfOrder]byte, scale, min float32) [lsfOrder][lsfOrder]float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L304-L312
	var out [lsfOrder][lsfOrder]float32
	for i := 0; i < lsfOrder; i++ {
		for j := 0; j < lsfOrder; j++ {
			// Round packed*scale before adding min (defeat FMA fusion; reference rounds separately).
			prod := float32(float32(packed[i][j]) * scale)
			out[i][j] = min + prod
		}
	}
	return out
}

// ---- small slice converters ----

func arr16ToSlice(a *[lsfOrder]float32) []float32 {
	out := make([]float32, lsfOrder)
	copy(out, a[:])
	return out
}

func mat16ToSlice(m *[lsfOrder][lsfOrder]float32) [][]float32 {
	out := make([][]float32, lsfOrder)
	for i := range out {
		out[i] = arr16ToSlice(&m[i])
	}
	return out
}

// lsfBuilt holds the three LSF runtime structs rebuilt from one seed.
type lsfBuilt struct {
	synth  *SmplSynthTables
	tables *SmplTables
	cb     *LsfCb
}

// buildLsfFromSeed runs the LSF codebook expansion to produce all three runtime structs.
func buildLsfFromSeed(s *lsfSeed) *lsfBuilt {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L314-L401
	st1 := make([]st1Tables, 0, 2)
	// Decoder-side accumulators for SmplSynthTables (centroids/matrices = cbhalf/we).
	synthCentroids := make([][][]float32, 2)
	synthMatrices := make([][][][]float32, 2)
	// grid==16 decorr matrices: the same Rotcond unpack8(rot_cond_8), flattened [lr][256].
	synthGrid16Matrices := make([][][]float32, 2)

	for voiced := 0; voiced < 2; voiced++ {
		// cInv (symmetric lower-triangular fill).
		var cInv [lsfOrder][lsfOrder]float32
		p := 0
		for i := 0; i < lsfOrder; i++ {
			for j := 0; j <= i; j++ {
				// Round scale*cinv before adding min (defeat FMA fusion).
				prod := float32(lsfCinvScale[voiced] * float32(s.cinv16[voiced][p]))
				v := lsfCinvMin[voiced] + prod
				cInv[i][j] = v
				cInv[j][i] = v
				p++
			}
		}

		var cbhalf [lsfCentroids][lsfOrder]float32
		var cbCinv [lsfCentroids][lsfOrder]float32
		var we [lsfCentroids][lsfOrder][lsfOrder]float32
		var wie [lsfCentroids][lsfOrder][lsfOrder]float32
		for c := 0; c < lsfCentroids; c++ {
			var lsfCB [lsfOrder]float32
			for i := 0; i < lsfOrder; i++ {
				// Round cb16*scale before the additions (defeat FMA fusion of min + cb16*scale).
				prod := float32(float32(s.cb16[voiced][c][i]) * lsfCBScale[voiced])
				lsfCB[i] = lsfCBMin[voiced] + prod + s.mean[voiced][i]
				cbhalf[c][i] = lsfCB[i] * 0.5
			}
			cbCinv[c] = lsfMatMultTransp16(&cInv, &lsfCB)
			rot := lsfUnpack8(&s.rot8[voiced][c], lsfRotScale[voiced], lsfRotMin[voiced])
			weC, wieC := lsfRotApplyWght(&rot, &lsfCB)
			we[c] = weC
			wie[c] = wieC
		}

		// Rotcond[lowRate] = unpack8(rot_cond_8[lowRate]).
		var rotcond [2][lsfOrder][lsfOrder]float32
		for lr := 0; lr < 2; lr++ {
			rotcond[lr] = lsfUnpack8(&s.rotCond8[voiced][lr], lsfRotCondScale[voiced], lsfRotCondMin[voiced])
		}

		bits := lsfCmfToBits(s.cmf[voiced][:])         // 16
		bitsCond := lsfCmfToBits(s.cmfCond[voiced][:]) // 17

		t := st1Tables{
			Cbhalf:   make([][]float32, lsfCentroids),
			CInv:     mat16ToSlice(&cInv),
			BitsCond: bitsCond,
			Rotcond:  [][][]float32{mat16ToSlice(&rotcond[0]), mat16ToSlice(&rotcond[1])},
			CbCinv:   make([][]float32, lsfCentroids),
			We:       make([][][]float32, lsfCentroids),
			Bits:     bits,
			Wie:      make([][][]float32, lsfCentroids),
		}
		for c := 0; c < lsfCentroids; c++ {
			t.Cbhalf[c] = arr16ToSlice(&cbhalf[c])
			t.CbCinv[c] = arr16ToSlice(&cbCinv[c])
			t.We[c] = mat16ToSlice(&we[c])
			t.Wie[c] = mat16ToSlice(&wie[c])
		}
		st1 = append(st1, t)

		// SmplSynthTables decoder centroids/matrices: grid g<16 == cbhalf[g]/we[g]. The
		// grid==16 row is never read (grid==16 returns before indexing it), so not appended.
		sc := make([][]float32, lsfCentroids)
		sm := make([][][]float32, lsfCentroids)
		for g := 0; g < lsfCentroids; g++ {
			sc[g] = arr16ToSlice(&cbhalf[g])
			sm[g] = mat16ToSlice(&we[g])
		}
		synthCentroids[voiced] = sc
		synthMatrices[voiced] = sm
		// grid16_matrices[voiced][lr] = the Rotcond computed above, flattened row-major to 256.
		g16 := make([][]float32, 2)
		for lr := 0; lr < 2; lr++ {
			flat := make([]float32, 0, lsfOrder*lsfOrder)
			for i := 0; i < lsfOrder; i++ {
				flat = append(flat, rotcond[lr][i][:]...)
			}
			g16[lr] = flat
		}
		synthGrid16Matrices[voiced] = g16
	}

	// Stage 2: the flat QlvlsTable / cmfTable / numBitsTable walks.
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L403-L480
	qlvlsFlat := make([]float32, lsfST2Len)
	var numqlvlsFlat [2][2][17][lsfOrder]int32
	var qoffFlat [2][2][17][lsfOrder]int
	var cmfSlices [2][2][17][lsfOrder][]uint16
	var numbitsSlices [2][2][17][lsfOrder][]float32

	qPtr, q8Ptr, dcmfPtr := 0, 0, 0
	for voiced := 0; voiced < 2; voiced++ {
		for lr := 0; lr < 2; lr++ {
			for c := 0; c < lsfCentroids+1; c++ {
				qstep := s.qstep[voiced][lr]
				if c == lsfCentroids {
					qstep *= lsfQstepCondMult
				}
				for i := 0; i < lsfOrder; i++ {
					minQi := int32(s.minQi[voiced][lr][c][i])
					maxQi := int32(s.maxQi[voiced][lr][c][i])
					numQlvls := int(maxQi - minQi + 1)
					numqlvlsFlat[voiced][lr][c][i] = int32(numQlvls)
					qoffFlat[voiced][lr][c][i] = qPtr
					for lvl := 0; lvl < numQlvls; lvl++ {
						q8 := float32(s.st2Qlvls8[q8Ptr])
						// Round scale*q8 before adding min (defeat FMA fusion).
						prod := float32(lsfST2QlvlsScale * q8)
						qlvlsFlat[qPtr] = (lsfST2QlvlsMin + prod +
							float32(lvl) + float32(minQi)) * qstep
						qPtr++
						q8Ptr++
					}
					dcmf := s.st2Dcmfs[dcmfPtr : dcmfPtr+numQlvls]
					cmf := lsfDcmfToCmf(dcmf) // numQlvls+1
					nb := lsfCmfToBits(cmf)   // numQlvls
					dcmfPtr += numQlvls
					cmfSlices[voiced][lr][c][i] = cmf
					numbitsSlices[voiced][lr][c][i] = nb
				}
			}
		}
	}
	if qPtr != lsfST2Len || q8Ptr != lsfST2Len || dcmfPtr != lsfST2Len {
		panic("mlow: lsf seed stage-2 pointer miscount (corrupt seed)")
	}

	// Assemble st2 (LsfCb) and valtables / lsf_stage2 (sliced from the flat tables).
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L482-L529
	st2 := make([][][]st2Tables, 2)
	valtables := make([][][][][]float32, 2)
	lsfStage2 := make([][][][][]uint16, 2)
	for voiced := 0; voiced < 2; voiced++ {
		st2[voiced] = make([][]st2Tables, 2)
		valtables[voiced] = make([][][][]float32, 2)
		lsfStage2[voiced] = make([][][][]uint16, 2)
		for lr := 0; lr < 2; lr++ {
			st2[voiced][lr] = make([]st2Tables, lsfCentroids+1)
			valtables[voiced][lr] = make([][][]float32, lsfCentroids+1)
			lsfStage2[voiced][lr] = make([][][]uint16, lsfCentroids+1)
			for c := 0; c < lsfCentroids+1; c++ {
				nq := make([]int32, lsfOrder)
				qlvls := make([][]float32, lsfOrder)
				vt := make([][]float32, lsfOrder)
				nb := make([][]float32, lsfOrder)
				cmfRows := make([][]uint16, lsfOrder)
				for i := 0; i < lsfOrder; i++ {
					n := int(numqlvlsFlat[voiced][lr][c][i])
					off := qoffFlat[voiced][lr][c][i]
					slice := append([]float32(nil), qlvlsFlat[off:off+n]...)
					nq[i] = int32(n)
					qlvls[i] = slice
					vt[i] = append([]float32(nil), qlvlsFlat[off:off+n]...)
					nb[i] = numbitsSlices[voiced][lr][c][i]
					cmfRows[i] = cmfSlices[voiced][lr][c][i]
				}
				st2[voiced][lr][c] = st2Tables{NumQlvls: nq, Qlvls: qlvls, NumBits: nb}
				valtables[voiced][lr][c] = vt
				lsfStage2[voiced][lr][c] = cmfRows
			}
		}
	}

	// Assemble the runtime structs.
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L531-L578
	cb := &LsfCb{
		St1:       st1,
		St2:       st2,
		MinQi:     lsfCloneQi(&s.minQi),
		MaxQi:     lsfCloneQi(&s.maxQi),
		Qstep:     [][]float32{{s.qstep[0][0], s.qstep[0][1]}, {s.qstep[1][0], s.qstep[1][1]}},
		MeanV:     arr16ToSlice(&s.mean[1]),
		MeanUV:    arr16ToSlice(&s.mean[0]),
		RegCond:   []float32{s.regCond[0], s.regCond[1]},
		MinDistV:  append([]float32(nil), s.minDist[1][:]...),
		MinDistUV: append([]float32(nil), s.minDist[0][:]...),
	}

	tables := &SmplTables{
		LsfSel: [][]uint16{
			{s.lsfSel[0][0], s.lsfSel[0][1], s.lsfSel[0][2]},
			{s.lsfSel[1][0], s.lsfSel[1][1], s.lsfSel[1][2]},
			{s.lsfSel[2][0], s.lsfSel[2][1], s.lsfSel[2][2]},
		},
		LsfGrid: LsfGrid{
			// match1 = CMF_cond_v, match1_alt = CMF_cond_uv, match0 = CMF_uv, match0_alt = CMF_v.
			Match1:    append([]uint16(nil), s.cmfCond[1][:]...),
			Match1Alt: append([]uint16(nil), s.cmfCond[0][:]...),
			Match0:    append([]uint16(nil), s.cmf[0][:]...),
			Match0Alt: append([]uint16(nil), s.cmf[1][:]...),
		},
		LsfStage2: lsfStage2,
		LsfExtra:  []uint16{s.lsfExtra[0], s.lsfExtra[1], s.lsfExtra[2]},
	}

	synth := &SmplSynthTables{
		Valtables: valtables,
		Centroids: synthCentroids,
		Matrices:  synthMatrices,
		// min_spacing[v] = min_dist[1-v] (the index swap), not separate ROM.
		MinSpacing: [][]float32{append([]float32(nil), s.minDist[1][:]...), append([]float32(nil), s.minDist[0][:]...)},
		// grid16_w[v] = mean[1-v] (the 1-v swap bakes in the synth's INVERTED selection);
		// grid16_alpha = reg_cond; grid16_matrices = unpack8(rot_cond_8) computed above.
		Grid16W:        [][]float32{arr16ToSlice(&s.mean[1]), arr16ToSlice(&s.mean[0])},
		Grid16Alpha:    []float32{s.regCond[0], s.regCond[1]},
		Grid16Matrices: synthGrid16Matrices,
	}

	return &lsfBuilt{synth: synth, tables: tables, cb: cb}
}

// lsfCloneQi widens the i8 stage-2 qi bounds to the [2][2][17][16]int32 runtime shape.
func lsfCloneQi(qi *[2][2][17][lsfOrder]int8) [][][][]int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L581-L593
	out := make([][][][]int32, 2)
	for v := 0; v < 2; v++ {
		out[v] = make([][][]int32, 2)
		for lr := 0; lr < 2; lr++ {
			out[v][lr] = make([][]int32, 17)
			for c := 0; c < 17; c++ {
				row := make([]int32, lsfOrder)
				for i := 0; i < lsfOrder; i++ {
					row[i] = int32(qi[v][lr][c][i])
				}
				out[v][lr][c] = row
			}
		}
	}
	return out
}

var (
	lsfBuiltOnce sync.Once
	lsfBuiltVal  *lsfBuilt
)

// loadLsfBuilt loads the LSF seed ROM and builds all three runtime structs once.
func loadLsfBuilt() *lsfBuilt {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/dbf10066a15f5c8c83c27908ad4284873331e1a4/wacore/src/voip/mlow/smpl_lsf_seed.rs#L595-L604
	lsfBuiltOnce.Do(func() {
		lsfBuiltVal = buildLsfFromSeed(loadLsfSeed())
	})
	return lsfBuiltVal
}
