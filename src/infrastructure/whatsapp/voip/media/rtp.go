package media

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math/big"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
)

const (
	rtpVersion       = 2
	rtpMinHeaderSize = 12
)

type RtpHeader struct {
	Version          uint8
	Padding          bool
	Extension        bool
	CsrcCount        uint8
	Marker           bool
	PayloadType      uint8
	SequenceNumber   uint16
	Timestamp        uint32
	Ssrc             uint32
	Csrc             []uint32
	ExtensionProfile uint16
	ExtensionData    []byte
}

func NewRtpHeader(payloadType uint8, seq uint16, ts, ssrc uint32) *RtpHeader {
	return &RtpHeader{
		Version:        rtpVersion,
		PayloadType:    payloadType,
		SequenceNumber: seq,
		Timestamp:      ts,
		Ssrc:           ssrc,
	}
}

func (h *RtpHeader) Size() int {
	s := rtpMinHeaderSize + int(h.CsrcCount)*4
	if h.Extension {
		s += 4 + len(h.ExtensionData)
	}
	return s
}

func (h *RtpHeader) Encode(buf []byte) (int, error) {
	if len(buf) < h.Size() {
		return 0, errors.New("buffer too small for RTP header")
	}

	buf[0] = (h.Version&0x03)<<6 |
		boolBit(h.Padding)<<5 |
		boolBit(h.Extension)<<4 |
		(h.CsrcCount & 0x0f)

	buf[1] = boolBit(h.Marker)<<7 | (h.PayloadType & 0x7f)

	binary.BigEndian.PutUint16(buf[2:], h.SequenceNumber)
	binary.BigEndian.PutUint32(buf[4:], h.Timestamp)
	binary.BigEndian.PutUint32(buf[8:], h.Ssrc)

	offset := 12
	for _, c := range h.Csrc {
		binary.BigEndian.PutUint32(buf[offset:], c)
		offset += 4
	}

	if h.Extension {
		binary.BigEndian.PutUint16(buf[offset:], h.ExtensionProfile)
		binary.BigEndian.PutUint16(buf[offset+2:], uint16(len(h.ExtensionData)/4))
		copy(buf[offset+4:], h.ExtensionData)
	}

	return h.Size(), nil
}

func DecodeRtpHeader(buf []byte) (*RtpHeader, error) {
	if len(buf) < rtpMinHeaderSize {
		return nil, errors.New("buffer too small for RTP header")
	}

	version := (buf[0] >> 6) & 0x03
	if version != rtpVersion {
		return nil, errors.New("invalid RTP version")
	}

	h := &RtpHeader{
		Version:        version,
		Padding:        (buf[0]>>5)&0x01 != 0,
		Extension:      (buf[0]>>4)&0x01 != 0,
		CsrcCount:      buf[0] & 0x0f,
		Marker:         (buf[1]>>7)&0x01 != 0,
		PayloadType:    buf[1] & 0x7f,
		SequenceNumber: binary.BigEndian.Uint16(buf[2:]),
		Timestamp:      binary.BigEndian.Uint32(buf[4:]),
		Ssrc:           binary.BigEndian.Uint32(buf[8:]),
	}

	headerSize := rtpMinHeaderSize + int(h.CsrcCount)*4
	if len(buf) < headerSize {
		return nil, errors.New("buffer too small for CSRC list")
	}

	offset := 12
	for i := 0; i < int(h.CsrcCount); i++ {
		h.Csrc = append(h.Csrc, binary.BigEndian.Uint32(buf[offset:]))
		offset += 4
	}

	if h.Extension && len(buf) >= offset+4 {
		h.ExtensionProfile = binary.BigEndian.Uint16(buf[offset:])
		extWords := binary.BigEndian.Uint16(buf[offset+2:])
		extBytes := int(extWords) * 4
		offset += 4
		if len(buf) >= offset+extBytes {
			h.ExtensionData = append([]byte(nil), buf[offset:offset+extBytes]...)
		}
	}

	return h, nil
}

type RtpPacket struct {
	Header  *RtpHeader
	Payload []byte
}

func (p *RtpPacket) Size() int {
	return p.Header.Size() + len(p.Payload)
}

func (p *RtpPacket) Encode() ([]byte, error) {
	buf := make([]byte, p.Size())
	headerSize, err := p.Header.Encode(buf)
	if err != nil {
		return nil, err
	}
	copy(buf[headerSize:], p.Payload)
	return buf, nil
}

func DecodeRtpPacket(buf []byte) (*RtpPacket, error) {
	header, err := DecodeRtpHeader(buf)
	if err != nil {
		return nil, err
	}
	payload := append([]byte(nil), buf[header.Size():]...)
	return &RtpPacket{Header: header, Payload: payload}, nil
}

type RtpSession struct {
	ssrc             uint32
	payloadType      uint8
	sequenceNumber   uint16
	sampleRate       int
	timestamp        uint32
	samplesPerPacket int
}

func NewRtpSession(ssrc uint32, payloadType uint8, sampleRate, samplesPerPacket int) *RtpSession {
	return &RtpSession{
		ssrc:             ssrc,
		payloadType:      payloadType,
		sequenceNumber:   uint16(randUint(65536)),
		sampleRate:       sampleRate,
		timestamp:        uint32(randUint(1 << 32)),
		samplesPerPacket: samplesPerPacket,
	}
}

func NewWhatsAppOpusSession(ssrc uint32) *RtpSession {
	return NewRtpSession(ssrc, core.PayloadTypeWhatsAppOpus, 16000, 960)
}

func (s *RtpSession) CreatePacket(payload []byte, marker bool) *RtpPacket {
	return s.CreatePacketWithDuration(payload, s.samplesPerPacket, marker)
}

func (s *RtpSession) CreatePacketWithDuration(payload []byte, durationSamples int, marker bool) *RtpPacket {
	header := NewRtpHeader(s.payloadType, s.sequenceNumber, s.timestamp, s.ssrc)
	header.Marker = marker

	s.sequenceNumber++
	s.timestamp += uint32(durationSamples)

	return &RtpPacket{Header: header, Payload: payload}
}

func boolBit(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func randUint(max int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		panic(err)
	}
	return n.Int64()
}
