package mlow

import "github.com/rs/zerolog"

// MLow top-level decoder: RED strip → TOC routing → active-frame decode (3 chained
// 20 ms internal frames: LSF → pulses → pitch/gains → reconstruct → CELP synthesis)
// → per-packet harmonic postfilter → 60 ms PCM. Cross-frame predictor and synthesis
// history persist across calls (the stream is continuous).
//
// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L1-L218

const opusFrameSamps = 960 // 60 ms @ 16 kHz

// SmplDecoderState is the cross-frame decoder state: LSF predictor, previous NLSF,
// the CELP synthesis state, and the harmonic-postfilter state.
type SmplDecoderState struct {
	Lstate   SmplLsfState
	PrevNLSF []float32
	Celp     *CelpDecState
	Harm     *HarmPostfilterState
}

func newSmplDecoderState() *SmplDecoderState {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/smpl_synth.rs#L641-L672
	return &SmplDecoderState{Celp: NewCelpDecState(), Harm: NewHarmPostfilterState()}
}

// MlowDecoder is a stateful pure-Go MLow decoder.
type MlowDecoder struct {
	state      *SmplDecoderState
	redundancy int32
	log        zerolog.Logger
}

// NewMlowDecoder allocates a fresh decoder.
func NewMlowDecoder(opts ...Option) *MlowDecoder {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L36-L41
	return &MlowDecoder{state: newSmplDecoderState(), log: resolveConfig(opts).log}
}

// SetRedundancy sets the negotiated RED redundancy level (0 = bare frames).
func (d *MlowDecoder) SetRedundancy(n int) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L44-L46
	d.redundancy = int32(n)
}

// Reset clears the cross-frame state (call at a stream discontinuity).
func (d *MlowDecoder) Reset() {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L49-L51
	d.state = newSmplDecoderState()
}

// Decode decodes one RTP MLow payload into a 60 ms (960-sample) PCM frame, float in [-1, 1].
func (d *MlowDecoder) Decode(payload []byte) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L54-L72
	if len(payload) == 0 {
		d.log.Trace().Msg("decode: empty payload, emitting silence")
		return make([]float32, opusFrameSamps)
	}
	d.log.Trace().Int("payload_bytes", len(payload)).Int32("redundancy", d.redundancy).Msg("decode packet")
	if d.redundancy > 0 {
		frames, err := DepackSplitRed(payload, d.log)
		if err != nil {
			d.log.Debug().Err(err).Int("payload_bytes", len(payload)).Msg("decode: RED depack failed, emitting silence")
			return make([]float32, opusFrameSamps)
		}
		var main []byte
		if len(frames) > 0 {
			main = frames[len(frames)-1].Data // the main (current) frame is last
		}
		d.log.Trace().Int("red_frames", len(frames)).Int("main_bytes", len(main)).Msg("decode: RED depacked")
		return d.decodeFrame(main)
	}
	return d.decodeFrame(payload)
}

func (d *MlowDecoder) decodeFrame(frame []byte) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L74-L99
	if len(frame) == 0 {
		d.log.Trace().Msg("decode frame: empty, emitting silence")
		return make([]float32, opusFrameSamps)
	}
	toc := ParseSmplTOC(frame[0], d.log)
	var outLen int
	if toc.StdOpus {
		outLen = 16000 / 1000 * toc.FrameMs
	} else {
		outLen = toc.SampleRate / 1000 * toc.FrameMs
	}
	d.log.Trace().Int("frame_bytes", len(frame)).Uint8("toc_byte", frame[0]).
		Bool("std_opus", toc.StdOpus).Bool("sid", toc.SID).Bool("active", toc.Active).
		Bool("voiced", toc.Voiced).Int("frame_ms", toc.FrameMs).Int("sample_rate", toc.SampleRate).
		Int("out_len", outLen).Msg("decode frame")
	if toc.StdOpus {
		d.log.Debug().Msg("decode frame: standard-Opus packet, not handled, emitting silence")
		return make([]float32, outLen)
	}
	if toc.SID || !toc.Active {
		d.log.Trace().Bool("sid", toc.SID).Bool("active", toc.Active).Msg("decode frame: inactive/SID, emitting silence")
		return make([]float32, outLen)
	}
	return d.decodeActiveFrame(frame, outLen)
}

func (d *MlowDecoder) decodeActiveFrame(frame []byte, outLen int) []float32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/decoder.rs#L101-L217
	config := int(frame[0]>>2) & 1
	tbl := LoadSmplTables()
	synthT := LoadSmplSynthTables()
	mem := LoadSmplMem()
	dec := NewRangeDecoder(frame[1:])
	lowRate := (frame[0]>>2)&1 != 0

	d.log.Trace().Int("config", config).Bool("low_rate", lowRate).Int("body_bytes", len(frame)-1).Int("internal_frames", 3).Msg("decode active frame")

	out := make([]float32, 0, 3*SmplIntfLen)
	packetLags := make([]float32, 0, 3*8)
	var avgNormBr float32
	for f := 0; f < 3; f++ {
		lsf := DecodeSmplLsf(dec, tbl, &d.state.Lstate, config, f)
		pulses := DecodeSmplPulses(dec, mem, SmplIntfLen, 4, 1, int32(config), lsf.Stage1)
		voiced := lsf.Stage1 == 1
		var total int32
		for _, c := range pulses.Subfr {
			total += c
		}
		params := CelpDecParams{Voiced: voiced, SfPulses: pulses.Subfr, TotalPulses: total}
		if voiced {
			pr := DecodeSmplPitch(dec, mem, &d.state.Lstate, SmplIntfLen, 4, int32(config), pulses.Subfr)
			for b := 0; b < 8; b++ {
				v := float64(pr.BlockLags[b])*0.5 + 32.0
				if v > 320.0 {
					v = 320.0
				}
				params.BlockLags[b] = float32(v)
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
		packetLags = append(packetLags, params.BlockLags[:]...)
		avgNormBr += SmplGetNormalizedBitrate(params.TotalPulses, SmplIntfLen)

		d.log.Trace().Int("intf", f).Bool("voiced", voiced).Int32("stage1", lsf.Stage1).
			Int32("grid", lsf.Grid).Int32("total_pulses", total).Int("nlsf_len", len(d.state.PrevNLSF)).
			Msg("decode internal frame params")

		nlsf := SmplReconstructNLSF(synthT, int(lsf.Stage1), config, int(lsf.Grid), &lsf.Stage2, d.state.PrevNLSF)
		var sig [SmplIntfLen]float32
		d.state.Celp.SynthFrame(nlsf, int(lsf.Extra), pulses.Pulses, &params, lowRate, SmplIntfLen, sig[:])
		d.state.PrevNLSF = nlsf
		out = append(out, sig[:]...)
	}

	// Per-packet harmonic postfilter (final pitch comb + 48-sample group delay) over the whole packet.
	plen := len(out)
	d.log.Trace().Int("samples", plen).Int("packet_lags", len(packetLags)).Msg("decode active frame: applying harmonic postfilter")
	SmplHarmPostfilter(d.state.Harm, out, plen, packetLags, len(packetLags), avgNormBr/3.0)

	pcm := make([]float32, len(out))
	for i, v := range out {
		switch {
		case v > 1.0:
			v = 1.0
		case v < -1.0:
			v = -1.0
		}
		pcm[i] = v
	}
	if outLen > 0 && outLen != len(pcm) {
		if outLen <= len(pcm) {
			pcm = pcm[:outLen]
		} else {
			np := make([]float32, outLen)
			copy(np, pcm)
			pcm = np
		}
	}
	return pcm
}
