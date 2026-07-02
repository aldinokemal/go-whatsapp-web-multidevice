package mlow

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

type ngJSON struct {
	EnvSmth       float32   `json:"env_smth"`
	EnvLast       float32   `json:"env_last"`
	OutStateUV    []float32 `json:"out_state_uv"`
	OutStateV     []float32 `json:"out_state_v"`
	CorrSmth      []float32 `json:"corr_smth"`
	ShapeState    []float32 `json:"shape_state"`
	PrevVoiced    int       `json:"prev_voiced"`
	SinceUnvoiced int32     `json:"since_unvoiced"`
	RandSeed      int32     `json:"rand_seed"`
}

func (j ngJSON) toNG() NoiseGenerator {
	ng := NoiseGenerator{
		EnvSmth: j.EnvSmth, EnvLast: j.EnvLast,
		PrevVoiced: j.PrevVoiced != 0, SinceUnvoiced: j.SinceUnvoiced, RandSeed: j.RandSeed,
	}
	copy(ng.OutStateUV[:], j.OutStateUV)
	copy(ng.OutStateV[:], j.OutStateV)
	copy(ng.CorrSmth[:], j.CorrSmth)
	copy(ng.ShapeState[:], j.ShapeState)
	return ng
}

// TestGenNoise validates the CELP noise generator bit-exactly against the
// instrumented-C vectors: each carries the input/output NoiseGenerator state, the
// excitation, params and lsf, and the expected noise[80]. Mirrors gen_noise_matches_c.
func TestGenNoise(t *testing.T) {
	raw, err := os.ReadFile("testdata/gennoise_vectors.json")
	if err != nil {
		t.Fatalf("read gennoise_vectors.json: %v", err)
	}
	var recs []struct {
		Voiced   int       `json:"voiced"`
		ExcPre   []float32 `json:"exc_pre"`
		Lsf      []float32 `json:"lsf"`
		Noise    []float32 `json:"noise"`
		Nrgres   float32   `json:"nrgres"`
		FcbgIdx  int32     `json:"fcbg_idx"`
		SfPulses int32     `json:"sf_pulses"`
		NormBr   float32   `json:"norm_br"`
		SeedOut  int32     `json:"seed_out"`
		NgIn     ngJSON    `json:"ng_in"`
		NgOut    ngJSON    `json:"ng_out"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("parse gennoise_vectors.json: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no gennoise vectors")
	}

	// fcbgains_uv[ix] = 10^(0.05*(ix-90)), ix in 0..=90.
	fcbgainsUV := make([]float32, 91)
	for ix := 0; ix <= 90; ix++ {
		fcbgainsUV[ix] = float32(math.Pow(10, 0.05*(float64(ix)-90.0)))
	}

	var vChecked, uv0Checked, uvpChecked int
	for n, r := range recs {
		ng := r.NgIn.toNG()
		ngOut := r.NgOut.toNG()
		var noise [smplMaxSFLen]float32
		SmplCelpGenNoise(&ng, r.ExcPre, 80, r.Voiced == 1, r.SfPulses, r.Nrgres, r.FcbgIdx, r.Lsf, r.NormBr, fcbgainsUV, noise[:])

		if ng.RandSeed != r.SeedOut || ng.RandSeed != ngOut.RandSeed {
			t.Errorf("rec %d: rand_seed got %d want %d/%d", n, ng.RandSeed, r.SeedOut, ngOut.RandSeed)
		}
		for i := 0; i < 80; i++ {
			if absF32(noise[i]-r.Noise[i]) >= 1e-6 {
				t.Errorf("rec %d: noise[%d] %.6g != %.6g (voiced=%d np=%d)", n, i, noise[i], r.Noise[i], r.Voiced, r.SfPulses)
				break
			}
		}
		if absF32(ng.EnvLast-ngOut.EnvLast) >= 1e-6 {
			t.Errorf("rec %d: env_last %.6g != %.6g", n, ng.EnvLast, ngOut.EnvLast)
		}
		for k := 0; k < 2; k++ {
			if absF32(ng.OutStateUV[k]-ngOut.OutStateUV[k]) >= 1e-6 {
				t.Errorf("rec %d: out_state_uv[%d]", n, k)
			}
			if absF32(ng.OutStateV[k]-ngOut.OutStateV[k]) >= 1e-6 {
				t.Errorf("rec %d: out_state_v[%d]", n, k)
			}
		}

		switch {
		case r.Voiced == 1:
			vChecked++
		case r.SfPulses == 0:
			uv0Checked++
		default:
			uvpChecked++
		}
	}
	if vChecked == 0 || uv0Checked == 0 || uvpChecked == 0 {
		t.Fatalf("vectors must exercise all paths: voiced=%d uv0=%d uvp=%d", vChecked, uv0Checked, uvpChecked)
	}
}
