package usecase

import (
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
)

const (
	pdfThumbnailDPI = 150

	// waveformSampleRate is the PCM rate used to build a 64-point voice waveform.
	waveformSampleRate = 8000
	// waveformMaxSeconds is the longest note decoded at waveformSampleRate.
	// Longer notes are decoded at a lower rate within waveformMaxPCMBytes.
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

// pdfRasterWithinBudget reports whether a page of the given size in points
// stays inside the shared image budget when rasterized at pdfThumbnailDPI.
func pdfRasterWithinBudget(widthPts, heightPts float64) bool {
	widthPx := ceilPDFPixels(widthPts)
	heightPx := ceilPDFPixels(heightPts)
	if widthPx <= 0 || heightPx <= 0 {
		return false
	}
	return utils.ValidateImageDimensions(widthPx, heightPx) == nil
}

// ceilPDFPixels is ceil(points * 150 / 72) in integer arithmetic so a page
// that lands on a whole pixel is not rounded up by float error.
func ceilPDFPixels(pts float64) int {
	if math.IsNaN(pts) || math.IsInf(pts, 0) || pts <= 0 || pts > 1e12 {
		return 0
	}
	milli := int64(math.Round(pts * 1000))
	if milli <= 0 {
		return 0
	}
	den := int64(1000 * 72)
	num := milli * int64(pdfThumbnailDPI)
	if num/milli != int64(pdfThumbnailDPI) {
		return 0
	}
	px := (num + den - 1) / den
	if px > int64(math.MaxInt32) {
		return 0
	}
	return int(px)
}

// pdfApprovedRasterPixels returns the pixel size pdfinfo measured for page 1
// when that size fits the shared image budget. If pdfinfo is missing, errors,
// or does not report an in-budget size, ok is false and the caller must not
// rasterize.
func pdfApprovedRasterPixels(path string) (width, height int, ok bool) {
	infoW, infoH, infoOK := pdfInfoPagePoints(path)
	if !infoOK {
		return 0, 0, false
	}
	return pdfRasterPixels(infoW, infoH)
}

// pdfPopplerArgs renders a page to an exact pixel size already accepted by
// pdfApprovedRasterPixels. It does not use an uncapped DPI flag. -cropbox
// renders the CropBox that pdfinfo measured, so the pinned size keeps the
// page's aspect ratio.
func pdfPopplerArgs(pdfPath, outPrefix string, pxW, pxH int) []string {
	if pxW < 1 {
		pxW = 1
	}
	if pxH < 1 {
		pxH = 1
	}
	return []string{
		"-png",
		"-f", "1",
		"-l", "1",
		"-cropbox",
		"-scale-to-x", strconv.Itoa(pxW),
		"-scale-to-y", strconv.Itoa(pxH),
		"-singlefile",
		pdfPath,
		outPrefix,
	}
}

func pdfRasterPixels(widthPts, heightPts float64) (int, int, bool) {
	if !pdfRasterWithinBudget(widthPts, heightPts) {
		return 0, 0, false
	}
	return ceilPDFPixels(widthPts), ceilPDFPixels(heightPts), true
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

// waveformFFmpegArgs decodes mono s8 PCM for the waveform. A note longer than
// waveformMaxSeconds is decoded whole at a rate that fits waveformMaxPCMBytes,
// so the bars span the full note; a shorter or unknown duration keeps the
// waveformSampleRate prefix.
func waveformFFmpegArgs(audioPath string, durationSeconds uint32) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-i", audioPath, "-ac", "1"}
	if durationSeconds > waveformMaxSeconds {
		args = append(args, "-ar", strconv.Itoa(max(64, waveformMaxPCMBytes/int(durationSeconds))))
	} else {
		args = append(args, "-ar", strconv.Itoa(waveformSampleRate), "-t", strconv.Itoa(waveformMaxSeconds))
	}
	return append(args, "-f", "s8", "-acodec", "pcm_s8", "pipe:1")
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
