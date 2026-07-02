package mlow

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// TestDecodeSmplPitch is the decode-side pitch KAT against pitch_vectors.json: for
// each active captured frame, the chain LSF(0) -> pulses(0) -> pitch(0) must
// reproduce the recorded lag/contour/gain_idx/filt_idx/int_lag_q6. Mirrors the
// reference pitch_match_go.
func TestDecodeSmplPitch(t *testing.T) {
	raw, err := os.ReadFile("testdata/pitch_vectors.json")
	if err != nil {
		t.Fatalf("read pitch_vectors.json: %v", err)
	}
	var recs []struct {
		Frame    string  `json:"frame"`
		Lag      int32   `json:"lag"`
		Contour  int32   `json:"contour"`
		GainIdx  []int32 `json:"gain_idx"`
		FiltIdx  []int32 `json:"filt_idx"`
		IntLagQ6 []int32 `json:"int_lag_q6"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse pitch_vectors.json: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no pitch vectors")
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
		pr := DecodeSmplPitch(dec, mem, &st, 320, 4, 0, pulses.Subfr)

		if pr.Lag != rec.Lag {
			t.Errorf("rec %d: lag got %d want %d", i, pr.Lag, rec.Lag)
		}
		if pr.Contour != rec.Contour {
			t.Errorf("rec %d: contour got %d want %d", i, pr.Contour, rec.Contour)
		}
		if !reflect.DeepEqual(pr.GainIdx[:], rec.GainIdx) {
			t.Errorf("rec %d: gain_idx got %v want %v", i, pr.GainIdx, rec.GainIdx)
		}
		if !reflect.DeepEqual(pr.FiltIdx[:], rec.FiltIdx) {
			t.Errorf("rec %d: filt_idx got %v want %v", i, pr.FiltIdx, rec.FiltIdx)
		}
		if !reflect.DeepEqual(pr.IntLagQ6[:], rec.IntLagQ6) {
			t.Errorf("rec %d: int_lag_q6 got %v want %v", i, pr.IntLagQ6, rec.IntLagQ6)
		}
		if dec.Err != 0 {
			t.Errorf("rec %d: decode error %d", i, dec.Err)
		}
	}
}
