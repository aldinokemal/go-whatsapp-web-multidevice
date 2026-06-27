package mlow

import (
	"errors"

	"github.com/rs/zerolog"
)

// MLow RED ("SplitRed") depacketization — the outermost wire layer of a WhatsApp
// MLow RTP audio payload (WASM func 3819). OPTIONAL: applied only when the call
// negotiated redundancy > 0; otherwise the RTP payload is a single bare MLow frame
// and this MUST NOT run (a bare frame's high-bit-set first byte would misparse).

// MlowFrame is one frame extracted from a SplitRed payload: raw MLow frame bytes
// (TOC + body) plus RED metadata. Data is a subslice of the input payload (no copy).
type MlowFrame struct {
	Data     []byte
	TimeCode uint8
	IsMain   bool
}

var (
	ErrPktSizeZero       = errors.New("mlow red: packet size zero")
	ErrHeaderTooShort    = errors.New("mlow red: header too short")
	ErrRedundantTooShort = errors.New("mlow red: redundant block too short")
	ErrMainTooShort      = errors.New("mlow red: main frame too short")
)

// DepackSplitRed parses a SplitRed RED packet into its frames (redundant blocks in
// header order, then the main frame last). Only call when RED was negotiated.
func DepackSplitRed(p []byte, log ...zerolog.Logger) ([]MlowFrame, error) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/ed12f359a086b28e807ba236f0977af1000859fe/wacore/src/voip/mlow/red.rs#L32-L95
	lg := pickLog(log)
	n := len(p)
	if n == 0 {
		lg.Debug().Msg("red depack: empty packet")
		return nil, ErrPktSizeZero
	}
	lg.Trace().Int("packet_bytes", n).Msg("red depack")
	type redBlock struct {
		code uint8
		size uint8
	}
	var red []redBlock
	cur := 0
	rem := n
	for {
		if rem == 0 {
			return nil, ErrHeaderTooShort
		}
		b0 := p[cur]
		if b0 < 0x80 {
			// main marker (high bit clear) terminates the header run
			if rem <= 1 {
				return nil, ErrMainTooShort
			}
			break
		}
		if rem <= 2 {
			return nil, ErrRedundantTooShort
		}
		size := p[cur+1]
		if int(size)+2 >= rem {
			return nil, ErrRedundantTooShort
		}
		red = append(red, redBlock{code: b0 & 0x7f, size: size})
		cur += 2
		rem -= int(size) + 2
	}

	mainCode := p[cur] & 0x7f
	cur++

	frames := make([]MlowFrame, 0, len(red)+1)
	for _, r := range red {
		frames = append(frames, MlowFrame{Data: p[cur : cur+int(r.size)], TimeCode: r.code, IsMain: false})
		cur += int(r.size)
	}
	mainSize := rem - 1 // total - header_size - sum(redundant sizes)
	frames = append(frames, MlowFrame{Data: p[cur : cur+mainSize], TimeCode: mainCode, IsMain: true})
	lg.Trace().Int("redundant_blocks", len(red)).Int("main_bytes", mainSize).Int("total_frames", len(frames)).Msg("red depack: done")
	return frames, nil
}
