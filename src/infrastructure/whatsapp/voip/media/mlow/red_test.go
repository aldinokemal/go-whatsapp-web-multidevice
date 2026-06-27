package mlow

import (
	"bytes"
	"errors"
	"testing"
)

// TestDepackSplitRed is the SplitRed depacketization KAT (the reference's inline
// unit tests): one redundant + main, header-only + main, empty packet, and the
// rejection of a bare high-bit-set frame.
func TestDepackSplitRed(t *testing.T) {
	// N=1: hdr [0x85,0x03] [main_marker 0x00] | red [AA BB CC] | main [50 11 22 33].
	frames, err := DepackSplitRed([]byte{0x85, 0x03, 0x00, 0xAA, 0xBB, 0xCC, 0x50, 0x11, 0x22, 0x33})
	if err != nil {
		t.Fatalf("one+main: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("one+main: got %d frames", len(frames))
	}
	if !bytes.Equal(frames[0].Data, []byte{0xAA, 0xBB, 0xCC}) || frames[0].TimeCode != 5 || frames[0].IsMain {
		t.Errorf("red frame: %+v", frames[0])
	}
	if !bytes.Equal(frames[1].Data, []byte{0x50, 0x11, 0x22, 0x33}) || frames[1].TimeCode != 0 || !frames[1].IsMain {
		t.Errorf("main frame: %+v", frames[1])
	}

	// header is just the main marker (0x00), then the main payload.
	frames, err = DepackSplitRed([]byte{0x00, 0x50, 0x11, 0x22})
	if err != nil {
		t.Fatalf("no-redundant: %v", err)
	}
	if len(frames) != 1 || !frames[0].IsMain || !bytes.Equal(frames[0].Data, []byte{0x50, 0x11, 0x22}) {
		t.Errorf("no-redundant frame: %+v", frames)
	}

	// empty packet.
	if _, err := DepackSplitRed(nil); !errors.Is(err, ErrPktSizeZero) {
		t.Errorf("empty: got %v want ErrPktSizeZero", err)
	}

	// a bare MLow frame (high-bit-set TOC like 0x90) must NOT parse as SplitRed.
	if _, err := DepackSplitRed([]byte{0x90, 0x01, 0x02}); err == nil {
		t.Errorf("bare frame should be rejected")
	}
}
