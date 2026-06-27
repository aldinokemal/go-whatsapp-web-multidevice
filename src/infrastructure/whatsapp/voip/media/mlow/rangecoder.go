package mlow

import "math/bits"

const (
	ecSymBits    = 8
	ecCodeBits   = 32
	ecSymMax     = 255
	ecCodeTop    = 1 << (ecCodeBits - 1)
	ecCodeBot    = ecCodeTop >> ecSymBits
	ecCodeExtra  = (ecCodeBits-2)%ecSymBits + 1
	ecWindowSize = 32
	ecUintBits   = 8
	ecCodeShift  = ecCodeBits - ecSymBits - 1
)

// ilog is floor(log2(x))+1 for x>0 and 0 for x==0.
func ilog(x uint32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L24-L26
	return int32(bits.Len32(x))
}

func ecMini(a, b uint32) uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L29-L31
	if a < b {
		return a
	}
	return b
}

// RangeDecoder is the Opus/CELT range entropy decoder. Range-coded symbols are
// read from the front of the buffer, raw bits from the back.
type RangeDecoder struct {
	buf        []byte
	storage    uint32
	endOffs    uint32
	endWindow  uint32
	nendBits   int32
	nbitsTotal int32
	offs       uint32
	rng        uint32
	val        uint32
	ext        uint32
	rem        int32
	// Err is a sticky decode error (degenerate/malformed table or exhausted bits).
	Err int32
}

// NewRangeDecoder initializes a decoder over buf.
func NewRangeDecoder(buf []byte) *RangeDecoder {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L52-L72
	d := &RangeDecoder{
		buf:        buf,
		storage:    uint32(len(buf)),
		nbitsTotal: ecCodeBits + 1 - ((ecCodeBits-ecCodeExtra)/ecSymBits)*ecSymBits,
		rng:        1 << ecCodeExtra,
	}
	d.rem = int32(d.readByte())
	d.val = d.rng - 1 - uint32(d.rem>>(ecSymBits-ecCodeExtra))
	d.normalize()
	return d
}

func (d *RangeDecoder) readByte() uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L74-L82
	if d.offs < d.storage {
		b := d.buf[d.offs]
		d.offs++
		return uint32(b)
	}
	return 0
}

func (d *RangeDecoder) readByteFromEnd() uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L84-L91
	if d.endOffs < d.storage {
		d.endOffs++
		return uint32(d.buf[d.storage-d.endOffs])
	}
	return 0
}

func (d *RangeDecoder) normalize() {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L93-L106
	for d.rng <= ecCodeBot {
		d.nbitsTotal += ecSymBits
		d.rng <<= ecSymBits
		sym0 := d.rem
		d.rem = int32(d.readByte())
		sym := (sym0<<ecSymBits | d.rem) >> (ecSymBits - ecCodeExtra)
		d.val = (d.val<<ecSymBits + (ecSymMax &^ uint32(sym))) & (ecCodeTop - 1)
	}
}

// Decode returns the cumulative frequency in [0, ft) for the next symbol; the
// caller locates the symbol and calls Update.
func (d *RangeDecoder) Decode(ft uint32) uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L110-L124
	if ft == 0 {
		d.Err = 1
		d.ext = 1
		return 0
	}
	d.ext = d.rng / ft
	if d.ext == 0 {
		d.Err = 1
		d.ext = 1
		return 0
	}
	s := d.val / d.ext
	return ft - ecMini(s+1, ft)
}

func (d *RangeDecoder) decodeBin(bitsN uint32) uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L127-L137
	d.ext = d.rng >> bitsN
	if d.ext == 0 {
		d.Err = 1
		d.ext = 1
		return 0
	}
	s := d.val / d.ext
	ft := uint32(1) << bitsN
	return ft - ecMini(s+1, ft)
}

// DecodeRawSymbol decodes a uniform nbits-bit symbol directly off the range stream.
func (d *RangeDecoder) DecodeRawSymbol(nbits uint32) uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L141-L145
	sym := d.decodeBin(nbits)
	d.Update(sym, sym+1, uint32(1)<<nbits)
	return sym
}

// Update advances past the symbol with cumulative range [fl, fh) out of ft.
func (d *RangeDecoder) Update(fl, fh, ft uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L148-L157
	s := d.ext * (ft - fh)
	d.val -= s
	if fl > 0 {
		d.rng = d.ext * (fh - fl)
	} else {
		d.rng -= s
	}
	d.normalize()
}

// BitLogp decodes one bit with P(0) = 1/2^logp.
func (d *RangeDecoder) BitLogp(logp uint32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L160-L173
	r := d.rng
	dv := d.val
	s := r >> logp
	var ret int32
	if dv < s {
		ret = 1
	}
	if ret == 0 {
		d.val = dv - s
		d.rng = r - s
	} else {
		d.rng = s
	}
	d.normalize()
	return ret
}

// DecodeICDF decodes a symbol against an inverse-CDF table; ftb = log2(ft).
func (d *RangeDecoder) DecodeICDF(icdf []byte, ftb uint32) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L176-L199
	if len(icdf) == 0 {
		d.Err = 1
		return 0
	}
	s0 := d.rng
	dv := d.val
	r := s0 >> ftb
	ret := int32(-1)
	var t uint32
	s := s0
	for {
		t = s
		ret++
		s = r * uint32(icdf[ret])
		if dv >= s || int(ret) >= len(icdf)-1 {
			break
		}
	}
	d.val = dv - s
	d.rng = t - s
	d.normalize()
	return ret
}

// DecodeCDF decodes a symbol against a uint16 cumulative CDF table; the effective
// total is cdf[n-1]-cdf[0].
func (d *RangeDecoder) DecodeCDF(cdf []uint16) int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L203-L226
	n := len(cdf)
	if n < 2 {
		d.Err = 1
		return 0
	}
	base := uint32(cdf[0])
	if uint32(cdf[n-1]) <= base {
		d.Err = 1
		return 0
	}
	ft := uint32(cdf[n-1]) - base
	fs := d.Decode(ft)
	target := base + fs
	k := 0
	for k < n-1 {
		if uint32(cdf[k+1]) > target {
			break
		}
		k++
	}
	d.Update(uint32(cdf[k])-base, uint32(cdf[k+1])-base, ft)
	return int32(k)
}

// BitsN reads n raw bits from the back of the buffer, LSB-first.
func (d *RangeDecoder) BitsN(n uint32) uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L229-L248
	window := d.endWindow
	available := d.nendBits
	if uint32(available) < n {
		for {
			window |= d.readByteFromEnd() << uint32(available)
			available += ecSymBits
			if uint32(available) > ecWindowSize-ecSymBits {
				break
			}
		}
	}
	ret := window & ((uint32(1) << n) - 1)
	window >>= n
	available -= int32(n)
	d.endWindow = window
	d.nendBits = available
	d.nbitsTotal += int32(n)
	return ret
}

// DecodeUint decodes an integer uniformly distributed in [0, ft0) for ft0 > 1.
func (d *RangeDecoder) DecodeUint(ft0 uint32) uint32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L251-L270
	ft := ft0 - 1
	ftb := ilog(ft)
	if ftb > ecUintBits {
		ftb -= ecUintBits
		t := (ft >> uint32(ftb)) + 1
		s := d.Decode(t)
		d.Update(s, s+1, t)
		v := (s << uint32(ftb)) | d.BitsN(uint32(ftb))
		if v <= ft {
			return v
		}
		d.Err = 1
		return ft
	}
	ft++
	s := d.Decode(ft)
	d.Update(s, s+1, ft)
	return s
}

// Decode64FineSym decodes the 64-symbol uniform fine-lag value.
func (d *RangeDecoder) Decode64FineSym() int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L274-L285
	d.ext = d.rng >> 6
	if d.ext == 0 {
		d.Err = 1
		d.ext = 1
		return 0
	}
	s := d.val / d.ext
	sym := int64(63) - int64(s)
	if sym < 0 {
		sym = 0
	} else if sym > 64 {
		sym = 64
	}
	d.Update(uint32(sym), uint32(sym)+1, 64)
	return int32(sym)
}

// Tell reports the number of bits consumed so far, rounded up.
func (d *RangeDecoder) Tell() int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L289-L291
	return d.nbitsTotal - ilog(d.rng)
}

// RangeEncoder is the Opus/CELT range entropy encoder, the exact inverse of
// RangeDecoder. Range-coded symbols are written toward the front of the buffer,
// raw bits toward the back; Done flushes and merges them.
type RangeEncoder struct {
	buf        []byte
	storage    uint32
	endOffs    uint32
	endWindow  uint32
	nendBits   int32
	nbitsTotal int32
	offs       uint32
	rng        uint32
	val        uint32
	ext        uint32
	rem        int32
	err        int32
}

// NewRangeEncoder allocates an encoder writing into a size-byte buffer.
func NewRangeEncoder(size int) *RangeEncoder {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L313-L328
	return &RangeEncoder{
		buf:        make([]byte, size),
		storage:    uint32(size),
		nbitsTotal: ecCodeBits + 1,
		rng:        ecCodeTop,
		rem:        -1,
	}
}

// Err returns the sticky encode error (-1 on failure).
func (e *RangeEncoder) Err() int32 {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L330-L332
	return e.err
}

func (e *RangeEncoder) writeByte(b uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L334-L341
	if e.offs+e.endOffs < e.storage {
		e.buf[e.offs] = byte(b)
		e.offs++
	} else {
		e.err = -1
	}
}

func (e *RangeEncoder) writeByteAtEnd(b uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L343-L350
	if e.offs+e.endOffs < e.storage {
		e.endOffs++
		e.buf[e.storage-e.endOffs] = byte(b)
	} else {
		e.err = -1
	}
}

func (e *RangeEncoder) carryOut(c int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L352-L372
	if uint32(c) != ecSymMax {
		carry := c >> ecSymBits
		if e.rem >= 0 {
			e.writeByte(uint32(e.rem + carry))
		}
		if e.ext > 0 {
			sym := uint32((ecSymMax + carry) & ecSymMax)
			for {
				e.writeByte(sym)
				e.ext--
				if e.ext == 0 {
					break
				}
			}
		}
		e.rem = c & ecSymMax
	} else {
		e.ext++
	}
}

func (e *RangeEncoder) normalize() {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L374-L381
	for e.rng <= ecCodeBot {
		e.carryOut(int32(e.val >> ecCodeShift))
		e.val = (e.val << ecSymBits) & (ecCodeTop - 1)
		e.rng <<= ecSymBits
		e.nbitsTotal += ecSymBits
	}
}

// Encode encodes the symbol with cumulative range [fl, fh) out of ft.
func (e *RangeEncoder) Encode(fl, fh, ft uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L383-L398
	if ft == 0 {
		e.err = -1
		return
	}
	r := e.rng / ft
	if fl > 0 {
		e.val += e.rng - r*(ft-fl)
		e.rng = r * (fh - fl)
	} else {
		e.rng -= r * (ft - fh)
	}
	e.normalize()
}

// BitLogp encodes one bit with P(0) = 1/2^logp.
func (e *RangeEncoder) BitLogp(val int32, logp uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L400-L412
	r := e.rng
	l := e.val
	s := r >> logp
	r2 := r - s
	if val != 0 {
		e.val = l + r2
		e.rng = s
	} else {
		e.rng = r2
	}
	e.normalize()
}

// EncodeICDF encodes symbol s against an inverse-CDF table; ftb = log2(ft).
func (e *RangeEncoder) EncodeICDF(s int32, icdf []byte, ftb uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L414-L428
	r := e.rng >> ftb
	if s > 0 {
		e.val += e.rng - r*uint32(icdf[s-1])
		e.rng = r * uint32(icdf[s-1]-icdf[s])
	} else {
		e.rng -= r * uint32(icdf[s])
	}
	e.normalize()
}

// EncodeCDF encodes symbol s against a uint16 cumulative CDF table.
func (e *RangeEncoder) EncodeCDF(s int32, cdf []uint16) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L431-L448
	n := len(cdf)
	if n < 2 || s < 0 || int(s+1) >= n {
		e.err = -1
		return
	}
	base := uint32(cdf[0])
	if uint32(cdf[n-1]) <= base {
		e.err = -1
		return
	}
	ft := uint32(cdf[n-1]) - base
	e.Encode(uint32(cdf[s])-base, uint32(cdf[s+1])-base, ft)
}

// BitsN writes the low n bits of fl as raw bits toward the back of the buffer.
func (e *RangeEncoder) BitsN(fl, n uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L451-L469
	window := e.endWindow
	used := e.nendBits
	if used+int32(n) > ecWindowSize {
		for {
			e.writeByteAtEnd(window & ecSymMax)
			window >>= ecSymBits
			used -= ecSymBits
			if used < ecSymBits {
				break
			}
		}
	}
	window |= fl << uint32(used)
	used += int32(n)
	e.endWindow = window
	e.nendBits = used
	e.nbitsTotal += int32(n)
}

// EncodeUint encodes an integer uniformly distributed in [0, ft0).
func (e *RangeEncoder) EncodeUint(fl, ft0 uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L471-L482
	ft := ft0 - 1
	ftb := ilog(ft)
	if ftb > ecUintBits {
		shift := uint32(ftb - ecUintBits)
		t := (ft >> shift) + 1
		e.Encode(fl>>shift, (fl>>shift)+1, t)
		e.BitsN(fl&((uint32(1)<<shift)-1), shift)
	} else {
		e.Encode(fl, fl+1, ft+1)
	}
}

// EncodeRawSymbol encodes a uniform nbits-bit symbol on the range stream.
func (e *RangeEncoder) EncodeRawSymbol(sym, nbits uint32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L485-L487
	e.Encode(sym, sym+1, uint32(1)<<nbits)
}

// Encode64FineSym encodes the 64-symbol uniform fine-lag value.
func (e *RangeEncoder) Encode64FineSym(sym int32) {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L490-L492
	e.Encode(uint32(sym), uint32(sym)+1, 64)
}

// Done flushes the range coder and merges the back raw-bit stream. After this,
// Bytes is the finished payload.
func (e *RangeEncoder) Done() {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L495-L531
	l := int32(ecCodeBits) - ilog(e.rng)
	msk := uint32(ecCodeTop-1) >> uint32(l)
	end := (e.val + msk) &^ msk
	if (end | msk) >= e.val+e.rng {
		l++
		msk >>= 1
		end = (e.val + msk) &^ msk
	}
	for l > 0 {
		e.carryOut(int32(end >> ecCodeShift))
		end = (end << ecSymBits) & (ecCodeTop - 1)
		l -= ecSymBits
	}
	if e.rem >= 0 || e.ext > 0 {
		e.carryOut(0)
	}
	window := e.endWindow
	used := e.nendBits
	for used >= ecSymBits {
		e.writeByteAtEnd(window & ecSymMax)
		window >>= ecSymBits
		used -= ecSymBits
	}
	if e.err == 0 {
		for i := e.offs; i < e.storage-e.endOffs; i++ {
			e.buf[i] = 0
		}
		if used > 0 {
			if e.endOffs >= e.storage-e.offs {
				e.err = -1
			} else {
				e.buf[e.storage-e.endOffs-1] |= byte(window)
			}
		}
	}
}

// Bytes returns the encoder's output buffer.
func (e *RangeEncoder) Bytes() []byte {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L533-L535
	return e.buf
}

// ConsumedLen reports the meaningful body length: front range bytes plus back
// raw-bit bytes (the gap between is zero-fill padding).
func (e *RangeEncoder) ConsumedLen() int {
	// Source of truth: https://github.com/oxidezap/whatsapp-rust/blob/674e85164b35ca19115dfebcf605708d15951ee7/wacore/src/voip/mlow/rangecoder.rs#L539-L541
	return int(e.offs + e.endOffs)
}
