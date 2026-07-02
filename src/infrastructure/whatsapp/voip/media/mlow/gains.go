package mlow

// Per-subframe gains + energy-residual decode (func 3545 GAINS block), for UNVOICED
// internal frames (LSF stage-1 selector 0) — mutually exclusive with the pitch block.

// SmplGainResult holds the decoded per-subframe gains and energy-residual symbols.
type SmplGainResult struct {
	GainQ  [4]int32 // per-subframe quantized log-gain (Q-domain)
	NrgRes [4]int32 // per-subframe energy-residual symbol (only subframes with pulses are read)
	// Raw entropy symbols (for the encoder to replay): the main + delta gain symbols.
	GainMain  int32
	GainDelta int32
}

// DecodeSmplGains decodes the gains+nrgres reads (the p3==4 path). subfrCounts are
// the per-subframe pulse counts. Group A/E tables come from the seed-built CcTables
// (the mem param is retained for call-site stability; only pitch lag reads use it).
func DecodeSmplGains(dec *RangeDecoder, _ *SmplMem, p3 int32, subfrCounts [4]int32) SmplGainResult {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_gains.rs#L18-L69
	var res SmplGainResult
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/924eb2c15aa9ffc7362293c74b2888e171831434/wacore/src/voip/mlow/smpl_gains.rs#L29-L67 (seed cc-table rewire: Group A/E from CcTables)
	cc := LoadCcTables()

	// main gain (n=85) + delta gain (n=99)
	gainMain := dec.DecodeCDF(cc.NrgresGain4())
	gainDelta := dec.DecodeCDF(cc.NrgresShape4())
	res.GainMain = gainMain
	res.GainDelta = gainDelta
	cfgSel := int32(2)

	// gain reconstruction: base7 = gain_main*nrg_step - 0x154000; cbv = gain_recon[sf + p3*delta].
	off6 := p3 * gainDelta
	base7 := gainMain*cc.NrgStep(cfgSel) - 0x154000
	take := int(p3)
	if take > 4 {
		take = 4
	}
	for sf := 0; sf < take; sf++ {
		cbv := cc.GainRecon(p3 == 4, int32(sf)+off6)
		res.GainQ[sf] = base7 + (cbv << 4)
	}

	// nrgres: per-subframe bucketed CDF (n=92) sliced by the gain-derived offset.
	for sf := 0; sf < take; sf++ {
		cnt := subfrCounts[sf]
		if cnt <= 0 {
			continue
		}
		var bucket int32
		if cnt >= 30 {
			bucket = 3
		} else {
			bucket = (cnt & 0xffff) / 10
		}
		// g = clamp((gainQ[sf]+8192)>>14, floor -85); min_offset = -neg_part (forward entry shift).
		g := (res.GainQ[sf] + 8192) >> 14
		if g < -85 {
			g = -85
		}
		negPart := (g >> 31) & g
		minOffset := int(-negPart)
		res.NrgRes[sf] = dec.DecodeCDF(cc.FcbgOffset(int(cfgSel), int(bucket), minOffset))
	}
	return res
}
