package mlow

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// captureUnvoicedFrame decodes one active frame, capturing the exact per-internal
// params + raw entropy symbols the decoder read. Returns nil if any internal frame
// is voiced (the pitch block has no decode→params inverse — see the pitch tests).
func captureUnvoicedFrame(fb []byte) *SmplFrameParams {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L101-L217
	toc := ParseSmplTOC(fb[0])
	if toc.StdOpus || toc.SID || !toc.Active {
		return nil
	}
	config := int(fb[0]>>2) & 1
	tbl := LoadSmplTables()
	mem := LoadSmplMem()
	dec := NewRangeDecoder(fb[1:])
	var st SmplLsfState
	fp := &SmplFrameParams{TOC: fb[0], Config: config}
	for f := 0; f < 3; f++ {
		lsf := DecodeSmplLsf(dec, tbl, &st, config, f)
		pul := DecodeSmplPulses(dec, mem, SmplIntfLen, 4, 1, int32(config), lsf.Stage1)
		ip := &fp.Internal[f]
		ip.Lsf = SmplLsfParams{Stage1: lsf.Stage1, Grid: lsf.Grid, Stage2: lsf.Stage2, Extra: lsf.Extra}
		var total int32
		for _, c := range pul.Subfr {
			total += c
		}
		ip.Pulses = SmplPulseParams{Total: total, Subfr: pul.Subfr, MagRuns: pul.MagRuns, SignSyms: pul.SignSyms}
		if lsf.Stage1 == 1 {
			return nil // voiced: not byte-exact roundtrippable from a decode
		}
		g := DecodeSmplGains(dec, mem, 4, pul.Subfr)
		ip.Gains = SmplGainParams{GainMain: g.GainMain, GainDelta: g.GainDelta, NrgRes: g.NrgRes}
	}
	return fp
}

// TestPitchBlockRoundTripsContour is the isolated voiced pitch-block round-trip
// (the reference's pitch_block_round_trips_contour): encode the LTP gains + the
// estimator contour (BlocksegIdx/Laginds), decode them back, and require the
// decoder's reconstructed BlockLags to equal the encoded Laginds — proving the
// wire encode (encodeSmplPitch + encodeLagsWire) is the inverse of DecodeSmplPitch.
func TestPitchBlockRoundTripsContour(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L416-L466
	mem := LoadSmplMem()
	cases := []struct {
		bsx     int
		laginds [8]int32
		gains   [4]int32
	}{
		{142, [8]int32{128, 129, 129, 118, 118, 121, 121, 123}, [4]int32{5, 2, 2, 2}},
		{142, [8]int32{128, 129, 129, 118, 118, 121, 121, 123}, [4]int32{5, 6, 2, 2}},
		{59, [8]int32{123, 123, 123, 123, 128, 128, 132, 132}, [4]int32{2, 6, 6, 6}},
	}
	subfr := [4]int32{1, 1, 1, 1}
	for ci, c := range cases {
		pp := SmplPitchParams{GainIdx: c.gains, BlocksegIdx: c.bsx, Laginds: c.laginds}
		est := SmplLsfState{PrevLag: -1, PrevFracLag: -1, PrevLagblk: -1, PrevLagidx: -1}
		enc := NewRangeEncoder(64)
		encodeSmplPitch(enc, mem, &est, 320, 4, 0, subfr, &pp)
		enc.Done()
		if enc.Err() != 0 {
			t.Fatalf("case %d: encoder error", ci)
		}
		body := enc.Bytes()[:enc.ConsumedLen()]

		dec := NewRangeDecoder(body)
		dst := SmplLsfState{PrevLag: -1, PrevFracLag: -1}
		pr := DecodeSmplPitch(dec, mem, &dst, 320, 4, 0, subfr)
		if pr.BlockLags != c.laginds {
			t.Errorf("case %d (bsx=%d): decoded BlockLags %v != encoded laginds %v", ci, c.bsx, pr.BlockLags, c.laginds)
		}
	}
}

// sameFrameParams compares the unvoiced-path fields of two captured frames.
func sameFrameParams(a, b *SmplFrameParams) bool {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/params.rs#L36-L67
	if a.TOC != b.TOC || a.Config != b.Config {
		return false
	}
	for f := 0; f < 3; f++ {
		x, y := &a.Internal[f], &b.Internal[f]
		if x.Lsf != y.Lsf || x.Gains != y.Gains {
			return false
		}
		if x.Pulses.Total != y.Pulses.Total || x.Pulses.Subfr != y.Pulses.Subfr {
			return false
		}
	}
	return true
}

// TestEntropyEncoderByteExact proves EncodeSmplFrame is the exact inverse of the
// decoder for the spectral/unvoiced path: decode each fully-unvoiced active frame
// in the real capture into params, re-encode, and require byte-identical output.
// Mirrors the reference's "encode_smpl_frame is the inverse of the byte-exact
// decoder" guarantee on LSF + pulses + gains.
func TestEntropyEncoderByteExact(t *testing.T) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/encode.rs#L61-L102
	var frames []string
	loadJSON(t, "inbound_capture_frames.json", &frames)

	checked := 0
	for i, hf := range frames {
		fb, err := hex.DecodeString(hf)
		if err != nil || len(fb) == 0 {
			continue
		}
		fp := captureUnvoicedFrame(fb)
		if fp == nil {
			continue
		}
		got, err := EncodeSmplFrame(fp)
		if err != nil {
			t.Fatalf("frame %d: encode: %v", i, err)
		}
		// Byte-exact on the decoder-consumed prefix. The reference encoder does not
		// trim the final range-coder padding byte(s) that the peer (libopus) encoder
		// drops, so the only allowed difference is trailing zeros — which the decoder
		// never reads (proven by re-decoding below).
		if len(got) < len(fb) || !bytes.Equal(got[:len(fb)], fb) {
			t.Fatalf("frame %d: re-encode prefix != original\n got %x\nwant %x", i, got, fb)
		}
		for j := len(fb); j < len(got); j++ {
			if got[j] != 0 {
				t.Fatalf("frame %d: re-encode differs beyond original in a non-zero byte at %d: %x", i, j, got)
			}
		}
		// Re-decode our bitstream and require the same params we encoded.
		if rt := captureUnvoicedFrame(got); rt == nil {
			t.Fatalf("frame %d: re-decode of our encode failed", i)
		} else if !sameFrameParams(fp, rt) {
			t.Fatalf("frame %d: re-decode params differ from encoded", i)
		}
		checked++
	}
	t.Logf("byte-exact re-encode verified on %d fully-unvoiced active frames", checked)
	if checked < 20 {
		t.Fatalf("too few unvoiced frames exercised: %d", checked)
	}
}
