package transport

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"strings"
)

const (
	stunMagicCookie     = 0x2112a442
	stunFingerprintXor  = 0x5354554e
	stunBindingRequest  = 0x0001
	stunAllocateRequest = 0x0003
	whatsappPing        = 0x0801

	attrUsername            = 0x0006
	attrMessageIntegrity    = 0x0008
	attrLifetime            = 0x000d
	attrXorRelayedAddress   = 0x0016
	attrRequestedTransport  = 0x0019
	attrPriority            = 0x0024
	attrSenderSubscriptions = 0x4000
	attrSsrcList            = 0x4024
	attrIceControlled       = 0x8029
	attrIceControlling      = 0x802a
	attrFingerprint         = 0x8028

	defaultICEPriority = 16_777_215
)

func generateTransactionID() []byte {
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return id
}

func encodeAttribute(attrType int, data []byte) []byte {
	header := make([]byte, 4)
	binary.BigEndian.PutUint16(header[0:], uint16(attrType))
	binary.BigEndian.PutUint16(header[2:], uint16(len(data)))
	padding := (4 - (len(data) % 4)) % 4
	out := append(header, data...)
	if padding > 0 {
		out = append(out, make([]byte, padding)...)
	}
	return out
}

func crc32stun(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

func buildStunMessage(msgType int, attrs, transactionID, integrityKey []byte, includeFingerprint bool) []byte {
	attrsData := append([]byte(nil), attrs...)

	if integrityKey != nil {
		msgLenForHmac := len(attrsData) + 24
		hmacHeader := make([]byte, 20)
		binary.BigEndian.PutUint16(hmacHeader[0:], uint16(msgType))
		binary.BigEndian.PutUint16(hmacHeader[2:], uint16(msgLenForHmac))
		binary.BigEndian.PutUint32(hmacHeader[4:], stunMagicCookie)
		copy(hmacHeader[8:], transactionID)

		mac := hmac.New(sha1.New, integrityKey)
		mac.Write(hmacHeader)
		mac.Write(attrsData)
		miAttr := encodeAttribute(attrMessageIntegrity, mac.Sum(nil))
		attrsData = append(attrsData, miAttr...)
	}

	if includeFingerprint {
		msgLenForCrc := len(attrsData) + 8
		crcHeader := make([]byte, 20)
		binary.BigEndian.PutUint16(crcHeader[0:], uint16(msgType))
		binary.BigEndian.PutUint16(crcHeader[2:], uint16(msgLenForCrc))
		binary.BigEndian.PutUint32(crcHeader[4:], stunMagicCookie)
		copy(crcHeader[8:], transactionID)

		crcInput := append(append([]byte(nil), crcHeader...), attrsData...)
		fingerprint := crc32stun(crcInput) ^ stunFingerprintXor
		fpBuf := make([]byte, 4)
		binary.BigEndian.PutUint32(fpBuf, fingerprint)
		fpAttr := encodeAttribute(attrFingerprint, fpBuf)
		attrsData = append(attrsData, fpAttr...)
	}

	header := make([]byte, 20)
	binary.BigEndian.PutUint16(header[0:], uint16(msgType))
	binary.BigEndian.PutUint16(header[2:], uint16(len(attrsData)))
	binary.BigEndian.PutUint32(header[4:], stunMagicCookie)
	copy(header[8:], transactionID)

	return append(header, attrsData...)
}

func encodeXorRelayedAddress(ip string, port int) []byte {
	data := make([]byte, 8)
	data[0] = 0x00
	data[1] = 0x01
	binary.BigEndian.PutUint16(data[2:], uint16(port)^uint16(stunMagicCookie>>16))
	var p0, p1, p2, p3 int
	fmt.Sscanf(ip, "%d.%d.%d.%d", &p0, &p1, &p2, &p3)
	ipNum := uint32(p0)<<24 | uint32(p1)<<16 | uint32(p2)<<8 | uint32(p3)
	binary.BigEndian.PutUint32(data[4:], ipNum^stunMagicCookie)
	return data
}

func BuildAllocateForRelay(senderSubscriptions, ssrcList, hmacKey []byte, relayIP string, relayPort int) []byte {
	txid := generateTransactionID()
	var parts [][]byte
	parts = append(parts, encodeAttribute(attrSenderSubscriptions, senderSubscriptions))
	parts = append(parts, encodeAttribute(attrSsrcList, ssrcList))
	if relayIP != "" && relayPort != 0 {
		parts = append(parts, encodeAttribute(attrXorRelayedAddress, encodeXorRelayedAddress(relayIP, relayPort)))
	}
	return buildStunMessage(stunAllocateRequest, concat(parts...), txid, hmacKey, false)
}

func BuildBindingRequestWithSubs(username, hmacKey, senderSubscriptions []byte, includeIceControlling, includeFingerprint bool) []byte {
	txid := generateTransactionID()
	var parts [][]byte

	if len(username) > 0 {
		parts = append(parts, encodeAttribute(attrUsername, username))
	}

	priorityBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(priorityBuf, defaultICEPriority)
	parts = append(parts, encodeAttribute(attrPriority, priorityBuf))

	if includeIceControlling {
		tieBreaker := make([]byte, 8)
		rand.Read(tieBreaker)
		parts = append(parts, encodeAttribute(attrIceControlling, tieBreaker))
	}

	if len(senderSubscriptions) > 0 {
		parts = append(parts, encodeAttribute(attrSenderSubscriptions, senderSubscriptions))
	}

	var key []byte
	if len(hmacKey) > 0 {
		key = hmacKey
	}
	return buildStunMessage(stunBindingRequest, concat(parts...), txid, key, includeFingerprint)
}

func BuildWhatsAppPing() []byte {
	txid := generateTransactionID()
	header := make([]byte, 20)
	binary.BigEndian.PutUint16(header[0:], whatsappPing)
	binary.BigEndian.PutUint16(header[2:], 0)
	binary.BigEndian.PutUint32(header[4:], stunMagicCookie)
	copy(header[8:], txid)
	return header
}

func IsStunPacket(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	return data[0]&0xc0 == 0
}

func IsRtpPacket(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	return data[0]&0xc0 == 0x80
}

type StunAttribute struct {
	Type     int
	TypeName string
	Length   int
	Data     []byte
}

type StunResponseInfo struct {
	RawType             int
	Method              string
	StunClass           string
	IsSuccess           bool
	IsError             bool
	ErrorCode           int
	ErrorReason         string
	StableRoutingConnID uint64
	TransactionID       string
	Length              int
	Attributes          []StunAttribute
}

var stunAttrNames = map[int]string{
	0x0001: "MAPPED-ADDRESS", 0x0006: "USERNAME", 0x0008: "MESSAGE-INTEGRITY",
	0x0009: "ERROR-CODE", 0x000a: "UNKNOWN-ATTRIBUTES", 0x0014: "REALM",
	0x0015: "NONCE", 0x0019: "REQUESTED-TRANSPORT", 0x0020: "XOR-MAPPED-ADDRESS",
	0x0024: "PRIORITY", 0x0025: "USE-CANDIDATE", 0x4000: "SENDER-SUBSCRIPTIONS",
	0x4001: "RECEIVER-SUBSCRIPTION", 0x4002: "SUBSCRIPTION-ACK", 0x8022: "SOFTWARE",
	0x8028: "FINGERPRINT", 0x8029: "ICE-CONTROLLED", 0x802a: "ICE-CONTROLLING",
	0x4033: "STABLE-ROUTING-CONN-ID",
}

func ParseStunResponse(data []byte) *StunResponseInfo {
	if len(data) < 20 {
		return nil
	}

	cookie := binary.BigEndian.Uint32(data[4:])
	if cookie != stunMagicCookie {
		msgType := int(binary.BigEndian.Uint16(data[0:]))
		if msgType == 0x0801 || msgType == 0x0802 {
			method := "wa-ping"
			if msgType == 0x0802 {
				method = "wa-pong"
			}
			return &StunResponseInfo{
				RawType:       msgType,
				Method:        method,
				StunClass:     "indication",
				TransactionID: hex.EncodeToString(data[8:20]),
				Length:        len(data),
			}
		}
		return nil
	}

	rawType := int(binary.BigEndian.Uint16(data[0:]))
	msgLength := int(binary.BigEndian.Uint16(data[2:]))
	transactionID := hex.EncodeToString(data[8:20])

	c0 := (rawType >> 4) & 0x1
	c1 := (rawType >> 8) & 0x1
	stunClassNum := (c1 << 1) | c0
	classes := []string{"request", "indication", "success", "error"}
	stunClass := "unknown"
	if stunClassNum < len(classes) {
		stunClass = classes[stunClassNum]
	}

	methodBits := ((rawType & 0x3e00) >> 2) | ((rawType & 0x00e0) >> 1) | (rawType & 0x000f)
	method := "unknown"
	switch methodBits {
	case 0x001:
		method = "binding"
	case 0x003:
		method = "allocate"
	case 0x004:
		method = "refresh"
	case 0x006:
		method = "send"
	case 0x007:
		method = "data"
	case 0x008:
		method = "create-permission"
	case 0x009:
		method = "channel-bind"
	}
	if rawType == 0x0801 {
		method = "wa-ping"
	}
	if rawType == 0x0802 {
		method = "wa-pong"
	}

	info := &StunResponseInfo{
		RawType:       rawType,
		Method:        method,
		StunClass:     stunClass,
		IsSuccess:     stunClass == "success",
		IsError:       stunClass == "error",
		TransactionID: transactionID,
		Length:        len(data),
	}

	offset := 20
	for offset+4 <= 20+msgLength && offset+4 <= len(data) {
		attrType := int(binary.BigEndian.Uint16(data[offset:]))
		attrLength := int(binary.BigEndian.Uint16(data[offset+2:]))
		attrEnd := offset + 4 + attrLength
		if attrEnd > len(data) {
			break
		}
		attrData := data[offset+4 : attrEnd]
		name := stunAttrNames[attrType]
		if name == "" {
			name = fmt.Sprintf("0x%04x", attrType)
		}
		info.Attributes = append(info.Attributes, StunAttribute{
			Type: attrType, TypeName: name, Length: attrLength, Data: attrData,
		})

		if attrType == 0x0009 && attrLength >= 4 {
			errorClass := int(attrData[2] & 0x07)
			errorNumber := int(attrData[3])
			info.ErrorCode = errorClass*100 + errorNumber
			if attrLength > 4 {
				info.ErrorReason = string(attrData[4:])
			}
		}
		if attrType == 0x4033 && stunClass == "success" && attrLength == 8 {
			info.StableRoutingConnID = binary.BigEndian.Uint64(attrData)
		}

		offset = attrEnd + ((4 - (attrLength % 4)) % 4)
	}

	return info
}

func FormatStunResponse(info *StunResponseInfo) string {
	result := fmt.Sprintf("STUN %s %s (0x%04x, %dB)", info.Method, info.StunClass, info.RawType, info.Length)
	if info.IsError && info.ErrorCode != 0 {
		result += fmt.Sprintf(" ERROR %d", info.ErrorCode)
		if info.ErrorReason != "" {
			result += ": " + info.ErrorReason
		}
	}
	if len(info.Attributes) > 0 {
		names := make([]string, len(info.Attributes))
		for i, a := range info.Attributes {
			names[i] = a.TypeName
		}
		result += " [" + strings.Join(names, ", ") + "]"
	}
	return result
}

func ClassifyPacket(data []byte) string {
	if len(data) < 2 {
		return fmt.Sprintf("tiny(%dB)", len(data))
	}
	twoBits := (data[0] & 0xc0) >> 6
	switch twoBits {
	case 0:
		if info := ParseStunResponse(data); info != nil {
			return FormatStunResponse(info)
		}
		msgType := (int(data[0]) << 8) | int(data[1])
		return fmt.Sprintf("STUN? 0x%x (%dB)", msgType, len(data))
	case 2:
		pt := data[1] & 0x7f
		marker := (data[1] >> 7) & 1
		seq := 0
		if len(data) >= 4 {
			seq = (int(data[2]) << 8) | int(data[3])
		}
		return fmt.Sprintf("RTP/SRTP PT=%d M=%d seq=%d (%dB)", pt, marker, seq, len(data))
	case 1:
		return fmt.Sprintf("DTLS? 0x%x (%dB)", data[0], len(data))
	}
	return fmt.Sprintf("unknown 0x%x (%dB)", data[0], len(data))
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
