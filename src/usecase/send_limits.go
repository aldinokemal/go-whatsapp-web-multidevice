package usecase

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
)

const (
	pdfThumbnailDPI = 150.0

	// waveformSampleRate is the PCM rate used to build a 64-point voice waveform.
	waveformSampleRate = 8000
	// waveformMaxSeconds bounds how much audio ffmpeg may decode for that waveform.
	waveformMaxSeconds = 120
	// waveformMaxPCMBytes is the decoded s8 mono prefix kept in memory.
	waveformMaxPCMBytes = waveformSampleRate * waveformMaxSeconds
)

// runExternal and runExternalOutput are replaceable so tests can prove an
// oversized PDF never reaches a rasterizer.
var runExternal = func(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

var runExternalOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var pdfInfoPageSizePattern = regexp.MustCompile(`Page(?:\s+\d+)?\s+size:\s+([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s+x\s+([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s+pts`)

var pdfMediaBoxPattern = regexp.MustCompile(`/MediaBox\s*\[\s*([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s+([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s+([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s+([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s*\]`)

// pdfRasterWithinBudget reports whether a page of the given size in points
// stays inside the shared image budget when rasterized at pdfThumbnailDPI.
func pdfRasterWithinBudget(widthPts, heightPts float64) bool {
	if math.IsNaN(widthPts) || math.IsNaN(heightPts) || math.IsInf(widthPts, 0) || math.IsInf(heightPts, 0) {
		return false
	}
	if widthPts <= 0 || heightPts <= 0 {
		return false
	}
	scale := pdfThumbnailDPI / 72.0
	widthPx := widthPts * scale
	heightPx := heightPts * scale
	if math.IsNaN(widthPx) || math.IsNaN(heightPx) || math.IsInf(widthPx, 0) || math.IsInf(heightPx, 0) {
		return false
	}
	if widthPx > float64(math.MaxInt32) || heightPx > float64(math.MaxInt32) {
		return false
	}
	return utils.ValidateImageDimensions(int(math.Ceil(widthPx)), int(math.Ceil(heightPx))) == nil
}

// pdfFileWithinRenderBudget accepts a PDF only when every discovered page box
// fits in the raster budget. Unknown sizes are rejected so a rasterizer is
// not asked to allocate an unbounded page.
func pdfFileWithinRenderBudget(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	foundBox, boxOK := mediaBoxesWithinBudget(data)
	if foundBox && !boxOK {
		return false
	}
	infoW, infoH, infoOK := pdfInfoPagePoints(path)
	if infoOK && !pdfRasterWithinBudget(infoW, infoH) {
		return false
	}
	return foundBox || infoOK
}

func mediaBoxesWithinBudget(data []byte) (found bool, ok bool) {
	matches := pdfMediaBoxPattern.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return false, false
	}
	for _, match := range matches {
		width, height, parsed := mediaBoxPoints(match)
		if !parsed || !pdfRasterWithinBudget(width, height) {
			return true, false
		}
	}
	return true, true
}

func mediaBoxPoints(match [][]byte) (float64, float64, bool) {
	if len(match) < 5 {
		return 0, 0, false
	}
	llx, err1 := strconv.ParseFloat(string(match[1]), 64)
	lly, err2 := strconv.ParseFloat(string(match[2]), 64)
	urx, err3 := strconv.ParseFloat(string(match[3]), 64)
	ury, err4 := strconv.ParseFloat(string(match[4]), 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return 0, 0, false
	}
	width := math.Abs(urx - llx)
	height := math.Abs(ury - lly)
	return width, height, true
}

func pdfInfoPagePoints(path string) (float64, float64, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := runExternalOutput(ctx, "pdfinfo", "-f", "1", "-l", "1", path)
	if err != nil {
		return 0, 0, false
	}
	return parsePDFInfoPageSize(out)
}

func parsePDFInfoPageSize(output []byte) (float64, float64, bool) {
	match := pdfInfoPageSizePattern.FindSubmatch(output)
	if len(match) < 3 {
		return 0, 0, false
	}
	width, errW := strconv.ParseFloat(string(match[1]), 64)
	height, errH := strconv.ParseFloat(string(match[2]), 64)
	if errW != nil || errH != nil {
		return 0, 0, false
	}
	return width, height, true
}

func waveformFFmpegArgs(audioPath string) []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", audioPath,
		"-ac", "1",
		"-ar", strconv.Itoa(waveformSampleRate),
		"-t", strconv.Itoa(waveformMaxSeconds),
		"-f", "s8",
		"-acodec", "pcm_s8",
		"pipe:1",
	}
}

// readWaveformPCM keeps at most max decoded PCM bytes. A longer stream is
// truncated to that prefix so the waveform can be built without buffering it.
func readWaveformPCM(r io.Reader, max int) ([]byte, error) {
	if max <= 0 {
		return nil, fmt.Errorf("invalid pcm cap")
	}
	buf := make([]byte, max)
	n, err := io.ReadFull(r, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return buf[:n], nil
	}
	if err != nil {
		if n > 0 {
			return buf[:n], nil
		}
		return nil, err
	}
	return buf[:n], nil
}
