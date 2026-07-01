package media

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp/voip/core"
)

type SrtpErrorType string

const (
	SrtpErrPacketTooShort SrtpErrorType = "packet_too_short"
	SrtpErrAuthFailed     SrtpErrorType = "auth_failed"
	SrtpErrEncryption     SrtpErrorType = "encryption"
	SrtpErrDecryption     SrtpErrorType = "decryption"
)

type SrtpError struct {
	Type SrtpErrorType
	Msg  string
}

func (e *SrtpError) Error() string { return fmt.Sprintf("srtp %s: %s", e.Type, e.Msg) }

type SrtpContext struct {
	sessionKey  []byte
	sessionSalt []byte
	authKey     []byte
	roc         uint32
	lastSeq     uint16
	initialized bool
	authTagLen  int
}

func NewSrtpContext(keying core.SrtpKeyingMaterial, authTagLen int) (*SrtpContext, error) {
	if authTagLen <= 0 {
		authTagLen = core.SRTPAuthTagLen
	}
	sk, err := deriveSrtpKey(keying.MasterKey, keying.MasterSalt, core.SRTPLabelEncryption, 16)
	if err != nil {
		return nil, err
	}
	ak, err := deriveSrtpKey(keying.MasterKey, keying.MasterSalt, core.SRTPLabelAuth, 20)
	if err != nil {
		return nil, err
	}
	ss, err := deriveSrtpKey(keying.MasterKey, keying.MasterSalt, core.SRTPLabelSalt, 14)
	if err != nil {
		return nil, err
	}
	return &SrtpContext{
		sessionKey:  sk,
		sessionSalt: ss,
		authKey:     ak,
		authTagLen:  authTagLen,
	}, nil
}

func (c *SrtpContext) SetAuthKeying(keying core.SrtpKeyingMaterial) error {
	ak, err := deriveSrtpKey(keying.MasterKey, keying.MasterSalt, core.SRTPLabelAuth, 20)
	if err != nil {
		return err
	}
	c.authKey = ak
	return nil
}

func (c *SrtpContext) Protect(packet *RtpPacket) ([]byte, error) {
	c.updateRoc(packet.Header.SequenceNumber)
	index := c.packetIndex(packet.Header.SequenceNumber)

	headerSize := packet.Header.Size()
	output := make([]byte, headerSize+len(packet.Payload)+c.authTagLen)

	if _, err := packet.Header.Encode(output); err != nil {
		return nil, &SrtpError{SrtpErrEncryption, err.Error()}
	}

	iv := c.generateIV(packet.Header.Ssrc, index)
	if err := aesCtrXor(c.sessionKey, iv, packet.Payload, output[headerSize:headerSize+len(packet.Payload)]); err != nil {
		return nil, &SrtpError{SrtpErrEncryption, err.Error()}
	}

	if c.authTagLen > 0 {
		authData := output[:headerSize+len(packet.Payload)]
		tag := c.computeAuthTag(authData, c.roc, c.authTagLen)
		copy(output[headerSize+len(packet.Payload):], tag)
	}

	return output, nil
}

func (c *SrtpContext) Unprotect(data []byte) (*RtpPacket, error) {
	if len(data) < 12 {
		return nil, &SrtpError{SrtpErrPacketTooShort, fmt.Sprintf("packet too short: %d bytes", len(data))}
	}

	header, err := DecodeRtpHeader(data)
	if err != nil {
		return nil, &SrtpError{SrtpErrDecryption, err.Error()}
	}
	headerSize := header.Size()
	payloadLen := len(data) - headerSize - c.authTagLen
	if payloadLen <= 0 {
		return nil, &SrtpError{SrtpErrPacketTooShort, fmt.Sprintf("no payload: %dB total, %dB header, auth=%d", len(data), headerSize, c.authTagLen)}
	}

	c.updateRoc(header.SequenceNumber)
	index := c.packetIndex(header.SequenceNumber)
	if c.authTagLen > 0 {
		expected := c.computeAuthTag(data[:headerSize+payloadLen], c.roc, c.authTagLen)
		if subtle.ConstantTimeCompare(expected, data[headerSize+payloadLen:]) != 1 {
			return nil, &SrtpError{SrtpErrAuthFailed, "srtp auth tag mismatch"}
		}
	}

	iv := c.generateIV(header.Ssrc, index)
	decrypted := make([]byte, payloadLen)
	if err := aesCtrXor(c.sessionKey, iv, data[headerSize:headerSize+payloadLen], decrypted); err != nil {
		return nil, &SrtpError{SrtpErrDecryption, err.Error()}
	}

	return &RtpPacket{Header: header, Payload: decrypted}, nil
}

func (c *SrtpContext) updateRoc(seq uint16) {
	if !c.initialized {
		c.lastSeq = seq
		c.initialized = true
		return
	}

	diff := int32(seq) - int32(c.lastSeq)
	if diff < -32768 {
		c.roc++
	}
	c.lastSeq = seq
}

func (c *SrtpContext) packetIndex(seq uint16) uint64 {
	return (uint64(c.roc) << 16) | uint64(seq)
}

func (c *SrtpContext) generateIV(ssrc uint32, index uint64) []byte {
	iv := make([]byte, 16)
	copy(iv, c.sessionSalt[:14])

	var ssrcBuf [4]byte
	binary.BigEndian.PutUint32(ssrcBuf[:], ssrc)
	for i := 0; i < 4; i++ {
		iv[4+i] ^= ssrcBuf[i]
	}

	var idxBuf [8]byte
	binary.BigEndian.PutUint64(idxBuf[:], index)
	for i := 0; i < 6; i++ {
		iv[8+i] ^= idxBuf[2+i]
	}

	return iv
}

func (c *SrtpContext) computeAuthTag(data []byte, roc uint32, tagLen int) []byte {
	mac := hmac.New(sha1.New, c.authKey)
	mac.Write(data)
	var rocBuf [4]byte
	binary.BigEndian.PutUint32(rocBuf[:], roc)
	mac.Write(rocBuf[:])
	sum := mac.Sum(nil)
	return sum[:tagLen]
}

type SrtpSession struct {
	sendCtx *SrtpContext
	recvCtx *SrtpContext
}

func NewSrtpSession(sendKey, recvKey core.SrtpKeyingMaterial, sendAuthLen, recvAuthLen int) (*SrtpSession, error) {
	sc, err := NewSrtpContext(sendKey, sendAuthLen)
	if err != nil {
		return nil, err
	}
	rc, err := NewSrtpContext(recvKey, recvAuthLen)
	if err != nil {
		return nil, err
	}
	return &SrtpSession{sendCtx: sc, recvCtx: rc}, nil
}

func (s *SrtpSession) Protect(packet *RtpPacket) ([]byte, error) { return s.sendCtx.Protect(packet) }

func (s *SrtpSession) Unprotect(data []byte) (*RtpPacket, error) { return s.recvCtx.Unprotect(data) }

func (s *SrtpSession) SetSendAuthKeying(keying core.SrtpKeyingMaterial) error {
	return s.sendCtx.SetAuthKeying(keying)
}

func deriveSrtpKey(masterKey, masterSalt []byte, label byte, length int) ([]byte, error) {
	iv := make([]byte, 16)
	copy(iv, masterSalt[:14])
	iv[7] ^= label

	out := make([]byte, length)
	if err := aesCtrXor(masterKey, iv, make([]byte, length), out); err != nil {
		return nil, err
	}
	return out, nil
}

func aesCtrXor(key, iv, src, dst []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	cipher.NewCTR(block, iv).XORKeyStream(dst, src)
	return nil
}
