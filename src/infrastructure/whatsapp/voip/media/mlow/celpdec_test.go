package mlow

import (
	"encoding/hex"
	"testing"
)

// TestExcPre validates the pre-noise CELP excitation (FCB pulses × gain + voiced
// ACB/LTP) against the C exc_pre dump, per subframe, driving the full decode chain
// (LSF → pulses → pitch/gains → reconstruct → SynthFrame). This proves the
// excitation domain and the ACB/LTP synthesis, independent of the PRNG noise.
// Mirrors exc_pre_matches_c. The full SynthFrame output is validated e2e at the decoder.
func TestExcPre(t *testing.T) {
	var crecs []struct {
		Packet int         `json:"packet"`
		Frame  int         `json:"frame"`
		Sf     int         `json:"sf"`
		Lags   []float32   `json:"lags"`
		Nz     [][]float64 `json:"nz"`
	}
	loadJSON(t, "exc_pre_lags.json", &crecs)
	var frames []string
	loadJSON(t, "inbound_capture_frames.json", &frames)

	cmap := make(map[[3]int]int, len(crecs))
	for i, c := range crecs {
		cmap[[3]int{c.Packet, c.Frame, c.Sf}] = i
	}

	tbl := LoadSmplTables()
	synthT := LoadSmplSynthTables()
	mem := LoadSmplMem()
	var lstate SmplLsfState
	celp := NewCelpDecState()
	celp.traceExcPre = true
	var prevNLSF []float32

	uvOK, uvBad, vOK, vBad := 0, 0, 0, 0
	var worst float32
	for packet, hexFrame := range frames {
		frame, err := hex.DecodeString(hexFrame)
		if err != nil || len(frame) == 0 {
			continue
		}
		toc := ParseSmplTOC(frame[0])
		if toc.StdOpus || toc.SID || !toc.Active {
			continue
		}
		config := int(frame[0]>>2) & 1
		lowRate := (frame[0]>>2)&1 != 0
		dec := NewRangeDecoder(frame[1:])
		for f := 0; f < 3; f++ {
			lsf := DecodeSmplLsf(dec, tbl, &lstate, config, f)
			pulses := DecodeSmplPulses(dec, mem, 320, 4, 1, int32(config), lsf.Stage1)
			voiced := lsf.Stage1 == 1
			var params CelpDecParams
			params.Voiced = voiced
			params.SfPulses = pulses.Subfr
			var total int32
			for _, c := range pulses.Subfr {
				total += c
			}
			params.TotalPulses = total
			if voiced {
				pr := DecodeSmplPitch(dec, mem, &lstate, 320, 4, int32(config), pulses.Subfr)
				for b := 0; b < 8; b++ {
					v := float32(pr.BlockLags[b])*0.5 + 32.0
					if v > 320.0 {
						v = 320.0
					}
					params.BlockLags[b] = v
				}
				for sf := 0; sf < 4; sf++ {
					params.AcbgIdx[sf] = pr.GainIdx[sf]
					if pr.FiltIdx[sf] > 0 {
						params.FcbgIdx[sf] = pr.FiltIdx[sf]
					}
				}
			} else {
				g := DecodeSmplGains(dec, mem, 4, pulses.Subfr)
				params.NrgresDbqQ14 = g.GainQ
				params.FcbgIdx = g.NrgRes
			}
			nlsf := SmplReconstructNLSF(synthT, int(lsf.Stage1), config, int(lsf.Grid), &lsf.Stage2, prevNLSF)
			var sig [SmplIntfLen]float32
			celp.SynthFrame(nlsf, int(lsf.Extra), pulses.Pulses, &params, lowRate, 320, sig[:])
			prevNLSF = nlsf

			for sf := 0; sf < 4; sf++ {
				ci, ok := cmap[[3]int{packet, f, sf}]
				if !ok {
					continue
				}
				c := crecs[ci]
				if voiced {
					if params.BlockLags[2*sf] != c.Lags[0] || params.BlockLags[2*sf+1] != c.Lags[1] {
						t.Fatalf("per-block lags diverge at pkt=%d f=%d sf=%d", packet, f, sf)
					}
				}
				var cexc [SmplSubfrLen]float32
				for _, pair := range c.Nz {
					cexc[int(pair[0])] = float32(pair[1])
				}
				base := sf * SmplSubfrLen
				bad := false
				for i := 0; i < SmplSubfrLen; i++ {
					d := absF32(celp.ExcPre[base+i] - cexc[i])
					if d > worst {
						worst = d
					}
					if d > 2e-5 {
						bad = true
					}
				}
				switch {
				case voiced && bad:
					vBad++
				case voiced:
					vOK++
				case bad:
					uvBad++
				default:
					uvOK++
				}
			}
		}
	}
	t.Logf("exc_pre vs C: unvoiced ok=%d bad=%d; voiced ok=%d bad=%d; worst=%.2e", uvOK, uvBad, vOK, vBad, worst)
	if uvBad != 0 {
		t.Errorf("unvoiced exc_pre diverges from C (%d subframes)", uvBad)
	}
	if vBad != 0 {
		t.Errorf("voiced exc_pre diverges from C (%d subframes)", vBad)
	}
}
