package mlow

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestLoadSmplTables is the KAT for the LSF table asset: the protobuf blob embedded
// at the package root must decode to exactly the tables captured in the reference
// JSON dump (testdata/smpl_tables.json), proving the zlib+protobuf round-trip is
// lossless and the Go port reads the same bytes the Rust reference generated.
func TestLoadSmplTables(t *testing.T) {
	raw, err := os.ReadFile("testdata/smpl_tables.json")
	if err != nil {
		t.Fatalf("read smpl_tables.json: %v", err)
	}
	var want SmplTables // unknown gain_* keys are ignored by encoding/json
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("parse smpl_tables.json: %v", err)
	}

	got := LoadSmplTables()
	if got == nil {
		t.Fatal("LoadSmplTables returned nil")
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("blob tables differ from JSON capture:\n sel:    %v\n grid:   %v\n extra:  %v\n stage2 dims: %d",
			len(got.LsfSel) == len(want.LsfSel),
			reflect.DeepEqual(got.LsfGrid, want.LsfGrid),
			reflect.DeepEqual(got.LsfExtra, want.LsfExtra),
			len(got.LsfStage2))
	}
}

// TestDecodeSmplLsf is the per-frame LSF decode KAT: parsing each captured frame
// body (after the TOC byte) at the range-coder start must reproduce the recorded
// stage1/grid/extra/stage2. Mirrors the reference lsf_frame0_matches_go test.
func TestDecodeSmplLsf(t *testing.T) {
	raw, err := os.ReadFile("testdata/lsf_vectors.json")
	if err != nil {
		t.Fatalf("read lsf_vectors.json: %v", err)
	}
	var recs []struct {
		Frame  string  `json:"frame"`
		Stage1 int32   `json:"stage1"`
		Grid   int32   `json:"grid"`
		Stage2 []int32 `json:"stage2"`
		Extra  int32   `json:"extra"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse lsf_vectors.json: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no LSF vectors")
	}

	tbl := LoadSmplTables()
	for i, rec := range recs {
		frame, err := hex.DecodeString(rec.Frame)
		if err != nil {
			t.Fatalf("rec %d: bad hex: %v", i, err)
		}
		var st SmplLsfState
		dec := NewRangeDecoder(frame[1:]) // skip the leading TOC byte
		idx := DecodeSmplLsf(dec, tbl, &st, 0, 0)

		if idx.Stage1 != rec.Stage1 {
			t.Errorf("rec %d: stage1 got %d want %d", i, idx.Stage1, rec.Stage1)
		}
		if idx.Grid != rec.Grid {
			t.Errorf("rec %d: grid got %d want %d", i, idx.Grid, rec.Grid)
		}
		if idx.Extra != rec.Extra {
			t.Errorf("rec %d: extra got %d want %d", i, idx.Extra, rec.Extra)
		}
		if !reflect.DeepEqual(idx.Stage2[:], rec.Stage2) {
			t.Errorf("rec %d: stage2 got %v want %v", i, idx.Stage2, rec.Stage2)
		}
		if dec.Err != 0 {
			t.Errorf("rec %d: decode error %d", i, dec.Err)
		}
	}
}
