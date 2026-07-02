package mlow

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestDecodeSmplGains is the gains KAT against gains_vectors.json: force-run on each
// active frame's post-pulse decoder state (LSF(0)->pulses(0)->gains), the decode is
// deterministic and must reproduce gain_q[] and nrg_res[]. Mirrors gains_match_go.
func TestDecodeSmplGains(t *testing.T) {
	raw, err := os.ReadFile("testdata/gains_vectors.json")
	if err != nil {
		t.Fatalf("read gains_vectors.json: %v", err)
	}
	var recs []struct {
		Frame  string  `json:"frame"`
		GainQ  []int32 `json:"gain_q"`
		NrgRes []int32 `json:"nrg_res"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse gains_vectors.json: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no gains vectors")
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
		pulses := DecodeSmplPulses(dec, mem, 320, 4, 1, 0, lsf.Stage1)
		g := DecodeSmplGains(dec, mem, 4, pulses.Subfr)

		if !reflect.DeepEqual(g.GainQ[:], rec.GainQ) {
			t.Errorf("rec %d: gain_q got %v want %v", i, g.GainQ, rec.GainQ)
		}
		if !reflect.DeepEqual(g.NrgRes[:], rec.NrgRes) {
			t.Errorf("rec %d: nrg_res got %v want %v", i, g.NrgRes, rec.NrgRes)
		}
	}
}
