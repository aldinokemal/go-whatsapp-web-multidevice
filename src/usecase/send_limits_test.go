package usecase

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
)

func pngWithIHDR(width, height uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 2
	payload := append([]byte("IHDR"), ihdr...)
	var chunk bytes.Buffer
	_ = binary.Write(&chunk, binary.BigEndian, uint32(len(ihdr)))
	chunk.Write(payload)
	_ = binary.Write(&chunk, binary.BigEndian, crc32.ChecksumIEEE(payload))
	out := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	return append(out, chunk.Bytes()...)
}

func pdfWithMediaBox(width, height int) []byte {
	return []byte(fmt.Sprintf("%%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Count 1/Kids[3 0 R]>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 %d %d]>>endobj\ntrailer<</Root 1 0 R>>\n%%%%EOF\n", width, height))
}

func withSendItemsDir(t *testing.T) {
	t.Helper()
	orig := config.PathSendItems
	config.PathSendItems = t.TempDir()
	t.Cleanup(func() { config.PathSendItems = orig })
}

func TestOpenImageForSendRejectsOversizedDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.png")
	require.NoError(t, os.WriteFile(path, pngWithIHDR(20000, 20000), 0600))
	img, err := openImageForSend(path, false)
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
	require.Nil(t, img)
}

func TestOpenImageForSendStillDecodesSmallImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small.png")
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 4, 5))))
	require.NoError(t, os.WriteFile(path, encoded.Bytes(), 0600))
	img, err := openImageForSend(path, false)
	require.NoError(t, err)
	require.Equal(t, 4, img.Bounds().Dx())
	require.Equal(t, 5, img.Bounds().Dy())
}

func TestGenerateImageDocThumbnailRejectsOversizedImage(t *testing.T) {
	withSendItemsDir(t)
	got := generateImageDocThumbnail(pngWithIHDR(9000, 9000), "big.png", "oversize")
	require.Nil(t, got)
}

func TestGenerateImageDocThumbnailKeepsSmallImage(t *testing.T) {
	withSendItemsDir(t)
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 32, 16)), &jpeg.Options{Quality: 80}))
	got := generateImageDocThumbnail(encoded.Bytes(), "photo.jpg", "small")
	require.NotEmpty(t, got)
	require.Equal(t, "image/jpeg", httpContentType(got))
}

func httpContentType(data []byte) string {
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	return ""
}

func TestPDFRasterBudget(t *testing.T) {
	require.True(t, pdfRasterWithinBudget(612, 792))
	require.False(t, pdfRasterWithinBudget(200000, 200000))
	require.False(t, pdfRasterWithinBudget(1, 200000))
	require.False(t, pdfRasterWithinBudget(math.Inf(1), 10))
}

func TestParsePDFInfoPageSize(t *testing.T) {
	width, height, ok := parsePDFInfoPageSize([]byte("Page    1 size:  612 x 792 pts (letter)\n"))
	require.True(t, ok)
	require.Equal(t, 612.0, width)
	require.Equal(t, 792.0, height)
}

func TestGeneratePDFThumbnailDoesNotRenderOversizedPage(t *testing.T) {
	withSendItemsDir(t)
	var rendered []string
	origRun := runExternal
	origOut := runExternalOutput
	runExternal = func(ctx context.Context, name string, args ...string) error {
		rendered = append(rendered, name)
		return fmt.Errorf("render should not be called")
	}
	runExternalOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "pdfinfo" {
			return []byte("Page 1 size: 200000 x 200000 pts\n"), nil
		}
		return nil, fmt.Errorf("unexpected %s", name)
	}
	t.Cleanup(func() {
		runExternal = origRun
		runExternalOutput = origOut
	})

	got := generatePDFThumbnail(pdfWithMediaBox(200000, 200000), "huge")
	require.Nil(t, got)
	require.NotContains(t, rendered, "pdftoppm")
	require.NotContains(t, rendered, "convert")
}

func TestGeneratePDFThumbnailRejectsWhenPDFInfoDisagrees(t *testing.T) {
	withSendItemsDir(t)
	var rendered []string
	origRun := runExternal
	origOut := runExternalOutput
	runExternal = func(ctx context.Context, name string, args ...string) error {
		rendered = append(rendered, name)
		return nil
	}
	runExternalOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Page    1 size:  200000 x 200000 pts\n"), nil
	}
	t.Cleanup(func() {
		runExternal = origRun
		runExternalOutput = origOut
	})

	got := generatePDFThumbnail(pdfWithMediaBox(612, 792), "lie")
	require.Nil(t, got)
	require.NotContains(t, rendered, "pdftoppm")
	require.NotContains(t, rendered, "convert")
}

func TestGeneratePDFThumbnailAttemptsRenderForLetterPage(t *testing.T) {
	withSendItemsDir(t)
	var rendered []string
	origRun := runExternal
	origOut := runExternalOutput
	runExternal = func(ctx context.Context, name string, args ...string) error {
		rendered = append(rendered, name)
		rendered = append(rendered, args...)
		return fmt.Errorf("stub")
	}
	runExternalOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Page    1 size:  612 x 792 pts\n"), nil
	}
	t.Cleanup(func() {
		runExternal = origRun
		runExternalOutput = origOut
	})

	// Only page 1 is measured, so a later oversized page does not block it.
	pdf := append(pdfWithMediaBox(612, 792), "4 0 obj<</Type/Page/MediaBox[0 0 14400 14400]>>endobj\n"...)
	got := generatePDFThumbnail(pdf, "letter")
	require.Nil(t, got)
	require.Contains(t, rendered, "pdftoppm")
	require.Contains(t, rendered, "-cropbox")
	require.Contains(t, rendered, "-scale-to-x")
	require.Contains(t, rendered, "-scale-to-y")
	require.Contains(t, rendered, "1275")
	require.Contains(t, rendered, "1650")
	require.NotContains(t, rendered, "-r")
}

func TestGeneratePDFThumbnailDoesNotRasterizeWhenPDFInfoFails(t *testing.T) {
	withSendItemsDir(t)
	var rendered []string
	origRun := runExternal
	origOut := runExternalOutput
	runExternal = func(ctx context.Context, name string, args ...string) error {
		rendered = append(rendered, name)
		return nil
	}
	runExternalOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("pdfinfo failed")
	}
	t.Cleanup(func() {
		runExternal = origRun
		runExternalOutput = origOut
	})

	// A decoy small MediaBox must not authorize pdftoppm when pdfinfo errors.
	got := generatePDFThumbnail(pdfWithMediaBox(612, 792), "decoy")
	require.Nil(t, got)
	require.NotContains(t, rendered, "pdftoppm")
	require.NotContains(t, rendered, "convert")
}

func TestOpenVideoThumbnailRejectsExtremeFrameBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frame.png")
	require.NoError(t, os.WriteFile(path, pngWithIHDR(2, 65535), 0600))
	img, err := openVideoThumbnail(path)
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
	require.Nil(t, img)
}

func TestOpenStickerImageRejectsHugeHeaderBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sticker.png")
	require.NoError(t, os.WriteFile(path, pngWithIHDR(30000, 30000), 0600))
	img, err := openStickerImage(path)
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
	require.Nil(t, img)
}

type infiniteByteReader struct{ b byte }

func (r infiniteByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

func TestReadWaveformPCMCapsDecodedBytes(t *testing.T) {
	const max = 4096
	samples, err := readWaveformPCM(infiniteByteReader{b: 80}, max)
	require.NoError(t, err)
	require.Len(t, samples, max)
	require.Equal(t, waveformSampleRate*waveformMaxSeconds, waveformMaxPCMBytes)

	waveform := downsampleToWaveform(samples, 64)
	require.Len(t, waveform, 64)
	for _, value := range waveform {
		require.LessOrEqual(t, value, byte(100))
	}
}

func TestWaveformFFmpegArgsSpanWholeNote(t *testing.T) {
	flagValue := func(args []string, flag string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag {
				return args[i+1]
			}
		}
		return ""
	}

	// Short or unknown durations keep the 8 kHz prefix capped by -t.
	for _, seconds := range []uint32{0, 30, waveformMaxSeconds} {
		args := waveformFFmpegArgs("note.ogg", seconds)
		require.Equal(t, "8000", flagValue(args, "-ar"))
		require.Equal(t, "120", flagValue(args, "-t"))
		require.Equal(t, "pipe:1", args[len(args)-1])
	}

	// Longer notes are decoded whole at a rate that fits waveformMaxPCMBytes.
	args := waveformFFmpegArgs("note.ogg", 300)
	require.NotContains(t, args, "-t")
	require.Equal(t, "3200", flagValue(args, "-ar"))
	require.Equal(t, "64", flagValue(waveformFFmpegArgs("note.ogg", 100000), "-ar"))
}
