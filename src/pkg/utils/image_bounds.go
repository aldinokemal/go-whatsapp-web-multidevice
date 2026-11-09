package utils

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"

	"github.com/disintegration/imaging"
)

const (
	// MaxImageEdge is the longest side accepted before a full raster decode.
	// 32768 admits long screenshots and panoramas and still bounds imaging's
	// per-axis resize weight tables.
	MaxImageEdge = 32768
	// MaxImagePixels is the decoded pixel budget for every format but JPEG,
	// about 40 megapixels. Width and height are also rejected when their
	// product would overflow. Original mode SendImage still sends an image
	// past these budgets, without a thumbnail.
	MaxImagePixels = 40_000_000
	// ThumbnailMaxWidth is the target width for a normal send thumbnail.
	ThumbnailMaxWidth = 100
	// ThumbnailMaxHeight stops a very narrow source from being stretched into
	// a tall thumbnail when the width is fixed and the height is derived.
	ThumbnailMaxHeight = 512
	// maxJPEGPixels replaces MaxImagePixels for JPEG, so 48, 50 and 64 MP
	// phone photos decode. The NRGBA copy made to rotate or crop the largest
	// one still fits in maxJPEGDecodeBytes.
	maxJPEGPixels = 80_000_000
	// maxJPEGDecodeBytes bounds what image/jpeg allocates for one decode. It
	// admits a 40 MP progressive or an 80 MP baseline 4:2:0 photo, and refuses
	// a 40 MP progressive CMYK JPEG, which needs about 900 MiB.
	maxJPEGDecodeBytes = 320 << 20
)

// ErrImageTooLarge is returned when width, height, or pixel count is outside
// the decode budget.
var ErrImageTooLarge = errors.New("image exceeds dimension or pixel budget")

// ValidateImageDimensions rejects non-positive sizes, edges above MaxImageEdge,
// pixel counts above MaxImagePixels, and sizes whose product overflows.
func ValidateImageDimensions(width, height int) error {
	return validateImageDimensions("", width, height)
}

// validateImageDimensions is ValidateImageDimensions for a decoded format. A
// JPEG is held to maxJPEGPixels instead of MaxImagePixels.
func validateImageDimensions(format string, width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%w: non-positive dimensions %dx%d", ErrImageTooLarge, width, height)
	}
	w := int64(width)
	h := int64(height)
	if w > math.MaxInt64/h {
		return fmt.Errorf("%w: dimensions %dx%d overflow pixel count", ErrImageTooLarge, width, height)
	}
	if w > MaxImageEdge || h > MaxImageEdge {
		return fmt.Errorf("%w: dimensions %dx%d exceed maximum edge %d", ErrImageTooLarge, width, height, MaxImageEdge)
	}
	limit := int64(MaxImagePixels)
	if format == "jpeg" {
		limit = maxJPEGPixels
	}
	if w*h > limit {
		return fmt.Errorf("%w: dimensions %dx%d exceed maximum pixel count %d", ErrImageTooLarge, width, height, limit)
	}
	return nil
}

// ThumbnailSize fits srcW x srcH inside the thumbnail box without upscaling.
// The result never exceeds ThumbnailMaxWidth, ThumbnailMaxHeight, or their product.
func ThumbnailSize(srcW, srcH int) (int, int) {
	return FitBoxSize(srcW, srcH, ThumbnailMaxWidth, ThumbnailMaxHeight)
}

// FitBoxSize fits srcW x srcH inside maxW x maxH without upscaling.
// Neither returned edge exceeds its cap, and the product stays within maxW*maxH.
// A zero requested dimension is not used to derive an unbounded edge.
func FitBoxSize(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}
	dstW := int64(srcW)
	dstH := int64(srcH)
	limitW := int64(maxW)
	limitH := int64(maxH)
	if dstW > limitW {
		dstH = dstH * limitW / dstW
		if dstH < 1 {
			dstH = 1
		}
		dstW = limitW
	}
	if dstH > limitH {
		dstW = dstW * limitH / dstH
		if dstW < 1 {
			dstW = 1
		}
		dstH = limitH
	}
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	maxPixels := limitW * limitH
	if dstH > 0 && dstW > maxPixels/dstH {
		dstW = maxPixels / dstH
		if dstW < 1 {
			dstW = 1
		}
	}
	return int(dstW), int(dstH)
}

// FitBox scales src down so both edges stay inside the box. It does not
// enlarge a smaller image and does not derive an unbounded height from a
// fixed width.
func FitBox(src image.Image, maxW, maxH int) image.Image {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	dstW, dstH := FitBoxSize(bounds.Dx(), bounds.Dy(), maxW, maxH)
	if dstW <= 0 || dstH <= 0 {
		return src
	}
	if dstW == bounds.Dx() && dstH == bounds.Dy() {
		return src
	}
	return imaging.Resize(src, dstW, dstH, imaging.Lanczos)
}

// FitThumbnail scales src down into the thumbnail box. It does not enlarge a
// small image, and it does not derive an unbounded height from a fixed width.
func FitThumbnail(src image.Image) image.Image {
	return FitBox(src, ThumbnailMaxWidth, ThumbnailMaxHeight)
}

// DecodeImageWithinBudget reads image dimensions before allocating the raster.
// Formats that cannot report a header are rejected instead of fully decoded.
// A JPEG is also rejected when its estimated decoder memory exceeds
// maxJPEGDecodeBytes.
func DecodeImageWithinBudget(r io.Reader, opts ...imaging.DecodeOption) (image.Image, string, error) {
	seeker, err := imageReaderAtStart(r)
	if err != nil {
		return nil, "", err
	}
	cfg, format, err := image.DecodeConfig(seeker)
	if err != nil {
		return nil, "", err
	}
	if err := validateImageDimensions(format, cfg.Width, cfg.Height); err != nil {
		return nil, format, err
	}
	if format == "jpeg" {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return nil, format, err
		}
		need, err := jpegDecodeBytes(seeker)
		if err != nil {
			return nil, format, err
		}
		if need > maxJPEGDecodeBytes {
			return nil, format, fmt.Errorf("%w: JPEG decode needs about %d bytes, budget %d", ErrImageTooLarge, need, maxJPEGDecodeBytes)
		}
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return nil, format, err
	}
	img, err := imaging.Decode(seeker, opts...)
	if err != nil {
		return nil, format, err
	}
	decoded := img.Bounds()
	if err := validateImageDimensions(format, decoded.Dx(), decoded.Dy()); err != nil {
		return nil, format, err
	}
	return img, format, nil
}

// DecodeImageFileWithinBudget opens path and decodes it only after the header
// passes the DecodeImageWithinBudget checks.
func DecodeImageFileWithinBudget(path string, opts ...imaging.DecodeOption) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := DecodeImageWithinBudget(file, opts...)
	return img, err
}

// jpegDecodeBytes estimates what image/jpeg allocates from the first SOF
// segment: one byte per component sample for the output planes, four more per
// sample for progressive coefficient buffers, and four bytes per pixel to
// convert a 4-component image to CMYK or an RGB-tagged 3-component image to
// RGBA. image/jpeg reads the RGB tag from APP0 and APP14 segments anywhere
// before EOI, so the whole stream is scanned.
func jpegDecodeBytes(r io.Reader) (int64, error) {
	br := bufio.NewReader(r)
	if _, err := br.Discard(2); err != nil { // SOI
		return 0, err
	}
	need := int64(-1) // set by the first SOF
	var pixels int64
	var comps int
	var rgbIDs, jfif, adobeRGB bool
	var err error
scan:
	for {
		// A marker is 0xff plus optional 0xff fill bytes. Other bytes between
		// segments, entropy-coded data included, are skipped, as image/jpeg does.
		var b byte
		if b, err = br.ReadByte(); err != nil {
			break
		}
		if b != 0xff {
			continue
		}
		marker := b
		for marker == 0xff && err == nil {
			marker, err = br.ReadByte()
		}
		switch {
		case err != nil:
			break scan
		case marker == 0x00 || (marker >= 0xd0 && marker <= 0xd7):
			continue // stuffed byte or RSTn, neither has a length
		case marker == 0xd9: // EOI
			break scan
		}
		var size [2]byte
		if _, err = io.ReadFull(br, size[:]); err != nil {
			break
		}
		n := int(binary.BigEndian.Uint16(size[:])) - 2
		switch {
		case marker == 0xe0 && n >= 5: // APP0; the last one decides JFIF
			var id [5]byte
			if _, err = io.ReadFull(br, id[:]); err != nil {
				break scan
			}
			n -= 5
			jfif = string(id[:]) == "JFIF\x00"
		case marker == 0xee && n >= 12: // APP14
			var app [12]byte
			if _, err = io.ReadFull(br, app[:]); err != nil {
				break scan
			}
			n -= 12
			if string(app[:5]) == "Adobe" {
				adobeRGB = app[11] == 0 // transform 0 is RGB
			}
		case need < 0 && marker&0xf0 == 0xc0 && marker != 0xc4 && marker != 0xc8 && marker != 0xcc:
			// SOF: precision, height, width, component count, then id, HiVi,
			// and Tq for each of at most 4 components.
			var sof [6 + 3*4]byte
			if n < 6+3 || n > len(sof) {
				return 0, fmt.Errorf("unsupported JPEG SOF length %d", n)
			}
			if _, err = io.ReadFull(br, sof[:n]); err != nil {
				break scan
			}
			height := int64(binary.BigEndian.Uint16(sof[1:3]))
			width := int64(binary.BigEndian.Uint16(sof[3:5]))
			comps = (n - 6) / 3
			// Component i holds width*height*h_i*v_i/(hMax*vMax) samples.
			var sumHV int64
			hMax, vMax := int64(1), int64(1)
			for i := 0; i < comps; i++ {
				h, v := int64(sof[7+3*i]>>4), int64(sof[7+3*i]&0x0f)
				sumHV += h * v
				hMax, vMax = max(hMax, h), max(vMax, v)
			}
			samples := width * height * sumHV / (hMax * vMax)
			need = samples
			if marker&0x03 == 2 { // SOF2, SOF6, SOF10, and SOF14 are progressive
				need += 4 * samples
			}
			pixels = width * height
			rgbIDs = comps == 3 && sof[6] == 'R' && sof[9] == 'G' && sof[12] == 'B'
			n = 0
		}
		if _, err = br.Discard(n); err != nil {
			break
		}
	}
	// Once the frame header is known, a stream may end without EOI.
	if err != nil && (need < 0 || !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF)) {
		return 0, err
	}
	if need < 0 {
		return 0, errors.New("JPEG has no SOF segment")
	}
	if comps == 4 || comps == 3 && !jfif && (adobeRGB || rgbIDs) {
		need += 4 * pixels
	}
	return need, nil
}

// imageReaderAtStart returns a seeker positioned for DecodeConfig.
// Callers that already hold a size-capped byte slice pass a *bytes.Reader.
// A non-seeker is buffered; those callers must already have capped the
// compressed length.
func imageReaderAtStart(r io.Reader) (io.ReadSeeker, error) {
	if seeker, ok := r.(io.ReadSeeker); ok {
		return seeker, nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}
