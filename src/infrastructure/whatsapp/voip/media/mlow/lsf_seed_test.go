package mlow

import (
	"math"
	"reflect"
	"testing"
)

// TestLsfSeedGoldenChecksums pins the Go seed-build output against the same golden
// to_bits constants the reference asserts (smpl_lsf_seed.rs lsf_seed_build_golden_checksums).
// Matching these bit-for-bit proves the Go float expansion (matmul / sqrt / log2 op order)
// reproduces the Rust seed-build exactly on the sampled fields.
func TestLsfSeedGoldenChecksums(t *testing.T) {
	b := loadLsfBuilt()
	st1v := b.cb.St1[1]
	chk := func(name string, got float32, want uint32) {
		if math.Float32bits(got) != want {
			t.Errorf("%s: got bits 0x%08x, want 0x%08x", name, math.Float32bits(got), want)
		}
	}
	chk("cbhalf[1][0][0]", st1v.Cbhalf[0][0], 0x3d93b440)
	chk("we[1][0][0][0]", st1v.We[0][0][0], 0x3e0ff885)
	chk("wie[1][0][0][0]", st1v.Wie[0][0][0], 0x4062b10f)
	chk("bits[1][0]", st1v.Bits[0], 0x40883c1d)

	if b.cb.St2[1][0][0].NumQlvls[0] != 6 {
		t.Errorf("numQlvls[1][0][0][0]: got %d, want 6", b.cb.St2[1][0][0].NumQlvls[0])
	}
	chk("Qlvls[1][0][0][0][0]", b.cb.St2[1][0][0].Qlvls[0][0], 0xbf2c0e76)

	wantStage2 := []uint16{0, 33, 140, 932, 6942, 28552, 32763}
	if !reflect.DeepEqual(b.tables.LsfStage2[1][0][0][0], wantStage2) {
		t.Errorf("lsf_stage2[1][0][0][0]: got %v, want %v", b.tables.LsfStage2[1][0][0][0], wantStage2)
	}
	if len(b.synth.Valtables[1][0][0][0]) != 6 {
		t.Errorf("valtables width: got %d, want 6", len(b.synth.Valtables[1][0][0][0]))
	}
}

// ulpDiff returns |bit distance| between two same-sign f32 (monotone ordering).
func f32Ulp(a, b float32) uint32 {
	ia := math.Float32bits(a)
	ib := math.Float32bits(b)
	if ia > ib {
		return ia - ib
	}
	return ib - ia
}

// TestLsfSeedMatchesBlobs cross-checks every field of the seed-built structs against the
// blob-loaded structs. Integer and non-transcendental float fields must be bit-identical;
// the sqrt-derived (We/Wie/Matrices) and log2-derived (Bits/BitsCond/NumBits) fields are
// allowed a few ULP (the reference seed-build itself diverges from the C-reference blob there).
func TestLsfSeedMatchesBlobs(t *testing.T) {
	b := loadLsfBuilt()
	cbBlob := LoadLsfCb()
	tblBlob := LoadSmplTables()
	synBlob := LoadSmplSynthTables()

	var maxSqrtUlp, maxLogUlp uint32
	exactF := func(name string, got, want float32) {
		if got != want {
			t.Errorf("%s: %v != %v (bits 0x%08x vs 0x%08x)", name, got, want, math.Float32bits(got), math.Float32bits(want))
		}
	}
	tolF := func(got, want float32, acc *uint32) {
		if u := f32Ulp(got, want); u > *acc {
			*acc = u
		}
	}

	// ---- SmplTables: fully integer, must be bit-exact ----
	if !reflect.DeepEqual(b.tables, tblBlob) {
		t.Error("SmplTables: seed-built != blob")
	}

	// ---- LsfCb ----
	if len(b.cb.St1) != len(cbBlob.St1) {
		t.Fatalf("St1 len %d != %d", len(b.cb.St1), len(cbBlob.St1))
	}
	for v := range b.cb.St1 {
		s, o := b.cb.St1[v], cbBlob.St1[v]
		for c := 0; c < lsfCentroids; c++ {
			for i := 0; i < lsfOrder; i++ {
				exactF("cbhalf", s.Cbhalf[c][i], o.Cbhalf[c][i])
				exactF("cb_cinv", s.CbCinv[c][i], o.CbCinv[c][i])
				for j := 0; j < lsfOrder; j++ {
					exactF("c_inv", s.CInv[i][j], o.CInv[i][j])
					tolF(s.We[c][i][j], o.We[c][i][j], &maxSqrtUlp)
					tolF(s.Wie[c][i][j], o.Wie[c][i][j], &maxSqrtUlp)
				}
			}
		}
		for lr := 0; lr < 2; lr++ {
			for i := 0; i < lsfOrder; i++ {
				for j := 0; j < lsfOrder; j++ {
					exactF("rotcond", s.Rotcond[lr][i][j], o.Rotcond[lr][i][j])
				}
			}
		}
		for i := range s.Bits {
			tolF(s.Bits[i], o.Bits[i], &maxLogUlp)
		}
		for i := range s.BitsCond {
			tolF(s.BitsCond[i], o.BitsCond[i], &maxLogUlp)
		}
	}
	for v := 0; v < 2; v++ {
		for lr := 0; lr < 2; lr++ {
			for c := 0; c < lsfCentroids+1; c++ {
				s, o := b.cb.St2[v][lr][c], cbBlob.St2[v][lr][c]
				if !reflect.DeepEqual(s.NumQlvls, o.NumQlvls) {
					t.Errorf("NumQlvls[%d][%d][%d] mismatch", v, lr, c)
				}
				for i := 0; i < lsfOrder; i++ {
					for k := range s.Qlvls[i] {
						exactF("qlvls", s.Qlvls[i][k], o.Qlvls[i][k])
					}
					for k := range s.NumBits[i] {
						tolF(s.NumBits[i][k], o.NumBits[i][k], &maxLogUlp)
					}
				}
			}
		}
	}
	if !reflect.DeepEqual(b.cb.MinQi, cbBlob.MinQi) {
		t.Error("MinQi mismatch")
	}
	if !reflect.DeepEqual(b.cb.MaxQi, cbBlob.MaxQi) {
		t.Error("MaxQi mismatch")
	}
	if !reflect.DeepEqual(b.cb.Qstep, cbBlob.Qstep) {
		t.Error("Qstep mismatch")
	}
	if !reflect.DeepEqual(b.cb.MeanV, cbBlob.MeanV) || !reflect.DeepEqual(b.cb.MeanUV, cbBlob.MeanUV) {
		t.Error("Mean mismatch")
	}
	if !reflect.DeepEqual(b.cb.RegCond, cbBlob.RegCond) {
		t.Error("RegCond mismatch")
	}
	if !reflect.DeepEqual(b.cb.MinDistV, cbBlob.MinDistV) || !reflect.DeepEqual(b.cb.MinDistUV, cbBlob.MinDistUV) {
		t.Error("MinDist mismatch")
	}

	// ---- SmplSynthTables ----
	// The seed refactor intentionally trims two synth tables vs the pre-seed blob:
	//   - Valtables width is numQlvls (the blob carried numQlvls+1; the decoder
	//     bounds-checks sym < numQlvls so the trailing entry is never read).
	//   - Centroids omits the grid==16 row (grid==16 returns before indexing it).
	// So compare the seed-built data as a bit-exact prefix of the blob, not via DeepEqual.
	for v := range b.synth.Valtables {
		for lr := range b.synth.Valtables[v] {
			for c := range b.synth.Valtables[v][lr] {
				for i := range b.synth.Valtables[v][lr][c] {
					sv := b.synth.Valtables[v][lr][c][i]
					ov := synBlob.Valtables[v][lr][c][i]
					if len(sv) > len(ov) {
						t.Errorf("valtables[%d][%d][%d][%d]: seed wider (%d) than blob (%d)", v, lr, c, i, len(sv), len(ov))
						continue
					}
					for k := range sv {
						exactF("valtables", sv[k], ov[k])
					}
				}
			}
		}
	}
	for v := range b.synth.Centroids {
		if len(b.synth.Centroids[v]) != lsfCentroids {
			t.Errorf("Centroids[%d] len = %d, want %d", v, len(b.synth.Centroids[v]), lsfCentroids)
		}
		for g := range b.synth.Centroids[v] {
			for i := range b.synth.Centroids[v][g] {
				exactF("centroids", b.synth.Centroids[v][g][i], synBlob.Centroids[v][g][i])
			}
		}
	}
	if !reflect.DeepEqual(b.synth.MinSpacing, synBlob.MinSpacing) {
		t.Error("MinSpacing mismatch")
	}
	if !reflect.DeepEqual(b.synth.Grid16W, synBlob.Grid16W) {
		t.Error("Grid16W mismatch")
	}
	if !reflect.DeepEqual(b.synth.Grid16Alpha, synBlob.Grid16Alpha) {
		t.Error("Grid16Alpha mismatch")
	}
	if !reflect.DeepEqual(b.synth.Grid16Matrices, synBlob.Grid16Matrices) {
		t.Error("Grid16Matrices mismatch")
	}
	for v := range b.synth.Matrices {
		for g := range b.synth.Matrices[v] {
			for i := range b.synth.Matrices[v][g] {
				for j := range b.synth.Matrices[v][g][i] {
					tolF(b.synth.Matrices[v][g][i][j], synBlob.Matrices[v][g][i][j], &maxSqrtUlp)
				}
			}
		}
	}

	t.Logf("max sqrt-field ULP (We/Wie/Matrices): %d", maxSqrtUlp)
	t.Logf("max log2-field ULP (Bits/BitsCond/NumBits): %d", maxLogUlp)
	if maxSqrtUlp > 8 {
		t.Errorf("sqrt-field divergence too large: %d ULP", maxSqrtUlp)
	}
	if maxLogUlp > 8 {
		t.Errorf("log2-field divergence too large: %d ULP", maxLogUlp)
	}
}
