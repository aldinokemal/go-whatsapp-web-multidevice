package mlow

import (
	"encoding/json"
	"os"
	"testing"
)

// TestLsfQuant is the LSF-quantizer KAT: for each captured record, the indices qi[]
// must match the C reference bit-for-bit and the reconstructed qlsf within 1e-4.
// Mirrors the reference lsf_quant_matches_c test.
func TestLsfQuant(t *testing.T) {
	raw, err := os.ReadFile("testdata/lsf_quant_io.json")
	if err != nil {
		t.Fatalf("read lsf_quant_io.json: %v", err)
	}
	var recs []struct {
		Lsf      []float32 `json:"lsf"`
		A        []float32 `json:"A"`
		Voiced   int       `json:"voiced"`
		LowRate  int       `json:"lowRate"`
		Surv     int       `json:"surv"`
		RDwAdj   float32   `json:"RDw_adj"`
		CondCode int       `json:"cond_coding"`
		PrevLsf  []float32 `json:"prev_lsf"`
		Qi       []int32   `json:"qi"`
		Qlsf     []float32 `json:"qlsf"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse lsf_quant_io.json: %v", err)
	}
	if len(recs) < 12 {
		t.Fatalf("need vectors, got %d", len(recs))
	}

	for n, r := range recs {
		var res LsfQuantResult
		if r.CondCode != 0 {
			res = LsfQuantCond(r.A, r.Lsf, r.PrevLsf, r.Voiced, r.LowRate, r.RDwAdj, r.Surv)
		} else {
			res = LsfQuant(r.A, r.Lsf, r.Voiced, r.LowRate, r.RDwAdj, r.Surv)
		}
		for k := 0; k < SmplLPCOrder+1; k++ {
			if res.Qi[k] != r.Qi[k] {
				t.Fatalf("rec %d (voiced=%d cond=%d): qi mismatch\n got  %v\n want %v",
					n, r.Voiced, r.CondCode, res.Qi, r.Qi)
			}
		}
		for k := 0; k < SmplLPCOrder; k++ {
			if d := res.QLsf[k] - r.Qlsf[k]; d > 1e-4 || d < -1e-4 {
				t.Errorf("rec %d: qlsf[%d] %.6f != C %.6f", n, k, res.QLsf[k], r.Qlsf[k])
			}
		}
	}
}
