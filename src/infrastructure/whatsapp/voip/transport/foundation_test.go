package transport

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestVarintEncoding(t *testing.T) {
	cases := []struct {
		in  uint64
		out []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xac, 0x02}},
	}
	for _, c := range cases {
		if got := encodeVarint(c.in); !bytes.Equal(got, c.out) {
			t.Errorf("varint(%d)=%x want %x", c.in, got, c.out)
		}
	}
}

func TestSenderSubscriptions(t *testing.T) {

	subs := BuildSenderSubscriptions(0x10)

	inner := []byte{0x18, 0x10, 0x28, 0x00, 0x30, 0x00}

	want := append([]byte{0x0a, byte(len(inner))}, inner...)
	if !bytes.Equal(subs, want) {
		t.Fatalf("sender subscriptions mismatch:\n got=%x\nwant=%x", subs, want)
	}
}

func TestStunPacketDetection(t *testing.T) {
	ping := BuildWhatsAppPing()
	if !IsStunPacket(ping) {
		t.Error("ping should be classified as STUN")
	}
	if IsRtpPacket(ping) {
		t.Error("ping should not be RTP")
	}

	rtp := []byte{0x80, 120, 0, 0}
	if !IsRtpPacket(rtp) {
		t.Error("0x80 first byte should be RTP")
	}
	if IsStunPacket(rtp) {
		t.Error("RTP should not be STUN")
	}
}

func TestStunBindingFingerprint(t *testing.T) {
	subs := BuildSenderSubscriptions(0x12345678)
	msg := BuildBindingRequestWithSubs(nil, nil, subs, true, true)

	if binary.BigEndian.Uint32(msg[4:]) != stunMagicCookie {
		t.Fatal("missing STUN magic cookie")
	}
	info := ParseStunResponse(msg)
	if info == nil {
		t.Fatal("could not parse the binding request we built")
	}
	if info.Method != "binding" {
		t.Fatalf("expected method binding, got %s", info.Method)
	}

	last := info.Attributes[len(info.Attributes)-1]
	if last.TypeName != "FINGERPRINT" {
		t.Fatalf("expected FINGERPRINT last, got %s", last.TypeName)
	}
	fpStart := len(msg) - 8
	want := crc32stun(msg[:fpStart]) ^ stunFingerprintXor
	got := binary.BigEndian.Uint32(msg[len(msg)-4:])
	if got != want {
		t.Fatalf("fingerprint mismatch: got %08x want %08x", got, want)
	}
}
