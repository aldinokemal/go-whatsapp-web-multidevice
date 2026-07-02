package mlow

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type rcOp struct {
	Kind int    `json:"kind"`
	A    uint32 `json:"a"`
	B    uint32 `json:"b"`
}

type rcVectors struct {
	BytesHex    string     `json:"bytesHex"`
	CDFBytesHex string     `json:"cdfBytesHex"`
	CDFOps      []rcOp     `json:"cdfOps"`
	CDFTables   [][]uint16 `json:"cdfTables"`
	ICDF        []byte     `json:"icdf"`
	Ops         []rcOp     `json:"ops"`
}

func loadRCVectors(t *testing.T) rcVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/rc_vectors.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var v rcVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal vectors: %v", err)
	}
	return v
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	return b
}

// icdfTable is the fixed inverse-CDF table the script was encoded against (ftb=8),
// carried base64-encoded in the vector.
func icdfTable(t *testing.T, v rcVectors) []byte {
	t.Helper()
	// json unmarshals the base64 "icdf" string straight into the []byte field.
	if len(v.ICDF) != 6 {
		t.Fatalf("icdf: got %d bytes, want 6", len(v.ICDF))
	}
	return v.ICDF
}

// TestRangeDecoderMatchesVectors replays the mixed icdf/raw/bit_logp/uint script
// and requires identical decoded values.
func TestRangeDecoderMatchesVectors(t *testing.T) {
	v := loadRCVectors(t)
	icdf := icdfTable(t, v)
	d := NewRangeDecoder(mustHex(t, v.BytesHex))
	for i, op := range v.Ops {
		switch op.Kind {
		case 0:
			if got := d.DecodeICDF(icdf, 8); got != int32(op.A) {
				t.Errorf("op %d icdf: got %d want %d", i, got, op.A)
			}
		case 1:
			if got := d.BitsN(op.A); got != op.B {
				t.Errorf("op %d bits(%d): got %d want %d", i, op.A, got, op.B)
			}
		case 2:
			if got := uint32(d.BitLogp(op.A)); got != op.B {
				t.Errorf("op %d bit_logp(%d): got %d want %d", i, op.A, got, op.B)
			}
		case 3:
			if got := d.DecodeUint(op.A); got != op.B {
				t.Errorf("op %d uint(ft=%d): got %d want %d", i, op.A, got, op.B)
			}
		default:
			t.Fatalf("op %d: bad kind %d", i, op.Kind)
		}
	}
	if d.Err != 0 {
		t.Errorf("decode error: Err=%d", d.Err)
	}
}

// TestRangeDecoderCDFMatchesVectors exercises DecodeCDF against cumulative tables
// (including non-zero-base ones).
func TestRangeDecoderCDFMatchesVectors(t *testing.T) {
	v := loadRCVectors(t)
	d := NewRangeDecoder(mustHex(t, v.CDFBytesHex))
	for i, op := range v.CDFOps {
		ti := op.Kind
		if got := d.DecodeCDF(v.CDFTables[ti]); got != int32(op.A) {
			t.Errorf("cdf op %d table %d: got %d want %d", i, ti, got, op.A)
		}
	}
	if d.Err != 0 {
		t.Errorf("decode error: Err=%d", d.Err)
	}
}

// TestRangeEncoderMatchesBytes re-encodes the same script and requires
// byte-identical output.
func TestRangeEncoderMatchesBytes(t *testing.T) {
	v := loadRCVectors(t)
	icdf := icdfTable(t, v)
	want := mustHex(t, v.BytesHex)
	e := NewRangeEncoder(len(want))
	for _, op := range v.Ops {
		switch op.Kind {
		case 0:
			e.EncodeICDF(int32(op.A), icdf, 8)
		case 1:
			e.BitsN(op.B, op.A)
		case 2:
			e.BitLogp(int32(op.B), op.A)
		case 3:
			e.EncodeUint(op.B, op.A)
		default:
			t.Fatalf("bad kind %d", op.Kind)
		}
	}
	e.Done()
	if e.Err() != 0 {
		t.Errorf("encoder error: %d", e.Err())
	}
	if !bytes.Equal(e.Bytes(), want) {
		t.Errorf("encoder output differs from reference bytes")
	}
}

// TestRangeEncoderCDFMatchesBytes re-encodes the CDF script and requires
// byte-identical output.
func TestRangeEncoderCDFMatchesBytes(t *testing.T) {
	v := loadRCVectors(t)
	want := mustHex(t, v.CDFBytesHex)
	e := NewRangeEncoder(len(want))
	for _, op := range v.CDFOps {
		e.EncodeCDF(int32(op.A), v.CDFTables[op.Kind])
	}
	e.Done()
	if e.Err() != 0 {
		t.Errorf("encoder error: %d", e.Err())
	}
	if !bytes.Equal(e.Bytes(), want) {
		t.Errorf("cdf encoder output differs from reference bytes")
	}
}
