package mlow

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestDecodeSmplPulses is the pulse KAT against pulse_vectors.json: for each active
// captured frame, decoding LSF(0) then pulses(0) must reproduce the per-subframe
// counts and the full signed pulse vector. Mirrors the reference pulses_match_go.
//
// Fails until DecodeSmplPulses lands (the chain only needs LSF, which exists).
func TestDecodeSmplPulses(t *testing.T) {
	raw, err := os.ReadFile("testdata/pulse_vectors.json")
	if err != nil {
		t.Fatalf("read pulse_vectors.json: %v", err)
	}
	var recs []struct {
		Frame  string  `json:"frame"`
		Subfr  []int32 `json:"subfr"`
		Pulses []struct {
			Pos int   `json:"pos"`
			Val int32 `json:"val"`
		} `json:"pulses"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse pulse_vectors.json: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no pulse vectors")
	}

	tbl := LoadSmplTables()
	mem := LoadSmplMem()
	for i, rec := range recs {
		frame, err := hex.DecodeString(rec.Frame)
		if err != nil {
			t.Fatalf("rec %d: bad hex: %v", i, err)
		}
		var st SmplLsfState
		dec := NewRangeDecoder(frame[1:])
		lsf := DecodeSmplLsf(dec, tbl, &st, 0, 0)
		pr := DecodeSmplPulses(dec, mem, 320, 4, 1, 0, lsf.Stage1)

		if !reflect.DeepEqual(pr.Subfr[:], rec.Subfr) {
			t.Errorf("rec %d: subfr got %v want %v", i, pr.Subfr, rec.Subfr)
		}
		want := make([]int32, len(pr.Pulses))
		for _, pu := range rec.Pulses {
			want[pu.Pos] = pu.Val
		}
		if !reflect.DeepEqual(pr.Pulses, want) {
			t.Errorf("rec %d: pulse vector mismatch", i)
		}
		if dec.Err != 0 {
			t.Errorf("rec %d: decode error %d", i, dec.Err)
		}
	}
}
