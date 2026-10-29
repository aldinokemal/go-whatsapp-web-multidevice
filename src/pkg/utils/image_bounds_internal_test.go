package utils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJPEGDecodeBytesCountsRGBConversion(t *testing.T) {
	// 7500x8000 4:4:4 needs 180,000,000 bytes of planes. image/jpeg converts an
	// RGB-tagged one to RGBA as well, 240,000,000 bytes more.
	soi := []byte{0xff, 0xd8}
	sof := func(ids ...byte) []byte {
		out := []byte{0xff, 0xc0, 0, 17, 8, 0x1f, 0x40, 0x1d, 0x4c, 3}
		for _, id := range ids {
			out = append(out, id, 0x11, 0)
		}
		return out
	}
	sos := []byte{0xff, 0xda, 0, 8, 1, 1, 0, 0, 63, 0}
	scanData := []byte{0x12, 0x34}
	jfif := []byte{0xff, 0xe0, 0, 7, 'J', 'F', 'I', 'F', 0}
	adobeRGB := []byte{0xff, 0xee, 0, 14, 'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 0}
	for _, tt := range []struct {
		name  string
		parts [][]byte
		want  int64
	}{
		{"YCbCr component ids", [][]byte{soi, sof(1, 2, 3), sos, scanData}, 180_000_000},
		{"RGB component ids", [][]byte{soi, sof('R', 'G', 'B'), sos, scanData}, 420_000_000},
		{"Adobe RGB after the first scan", [][]byte{soi, sof(1, 2, 3), sos, scanData, adobeRGB}, 420_000_000},
		{"JFIF keeps YCbCr", [][]byte{soi, jfif, sof(1, 2, 3), sos, scanData, adobeRGB}, 180_000_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jpegDecodeBytes(bytes.NewReader(bytes.Join(tt.parts, nil)))
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	// An RGB-tagged 60 MP JPEG fits the pixel cap but not the decode budget.
	rgb := bytes.Join([][]byte{soi, sof('R', 'G', 'B'), sos}, nil)
	_, _, err := DecodeImageWithinBudget(bytes.NewReader(rgb))
	require.ErrorIs(t, err, ErrImageTooLarge)
}
