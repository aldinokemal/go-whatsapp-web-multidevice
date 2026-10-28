package utils

import (
	"bytes"
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
	// Typical phone photos sit well under this; 8192 still admits a long edge
	// from a high-resolution camera without allowing multi-gigapixel headers.
	MaxImageEdge = 8192
	// MaxImagePixels is the decoded pixel budget, about 40 megapixels.
	// Width and height are also rejected when their product would overflow.
	MaxImagePixels = 40_000_000
	// ThumbnailMaxWidth is the target width for a normal send thumbnail.
	ThumbnailMaxWidth = 100
	// ThumbnailMaxHeight stops a very narrow source from being stretched into
	// a tall thumbnail when the width is fixed and the height is derived.
	ThumbnailMaxHeight = 512
)

// ErrImageTooLarge is returned when width, height, or pixel count is outside
// the decode budget.
var ErrImageTooLarge = errors.New("image exceeds dimension or pixel budget")

// ValidateImageDimensions rejects non-positive sizes, edges above MaxImageEdge,
// pixel counts above MaxImagePixels, and sizes whose product overflows.
func ValidateImageDimensions(width, height int) error {
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
	if w*h > MaxImagePixels {
		return fmt.Errorf("%w: dimensions %dx%d exceed maximum pixel count %d", ErrImageTooLarge, width, height, MaxImagePixels)
	}
	return nil
}

// ThumbnailSize fits srcW x srcH inside the thumbnail box without upscaling.
// The result never exceeds ThumbnailMaxWidth, ThumbnailMaxHeight, or their product.
func ThumbnailSize(srcW, srcH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return 0, 0
	}
	dstW := int64(srcW)
	dstH := int64(srcH)
	maxW := int64(ThumbnailMaxWidth)
	maxH := int64(ThumbnailMaxHeight)
	if dstW > maxW {
		dstH = dstH * maxW / dstW
		if dstH < 1 {
			dstH = 1
		}
		dstW = maxW
	}
	if dstH > maxH {
		dstW = dstW * maxH / dstH
		if dstW < 1 {
			dstW = 1
		}
		dstH = maxH
	}
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	maxPixels := maxW * maxH
	if dstH > 0 && dstW > maxPixels/dstH {
		dstW = maxPixels / dstH
		if dstW < 1 {
			dstW = 1
		}
	}
	return int(dstW), int(dstH)
}

// FitThumbnail scales src down into the thumbnail box. It does not enlarge a
// small image, and it does not derive an unbounded height from a fixed width.
func FitThumbnail(src image.Image) image.Image {
	if src == nil {
		return nil
	}
	bounds := src.Bounds()
	dstW, dstH := ThumbnailSize(bounds.Dx(), bounds.Dy())
	if dstW <= 0 || dstH <= 0 {
		return src
	}
	if dstW == bounds.Dx() && dstH == bounds.Dy() {
		return src
	}
	return imaging.Resize(src, dstW, dstH, imaging.Lanczos)
}

// DecodeImageWithinBudget reads image dimensions before allocating the raster.
// Formats that cannot report a header are rejected instead of fully decoded.
func DecodeImageWithinBudget(r io.Reader, opts ...imaging.DecodeOption) (image.Image, string, error) {
	seeker, err := imageReaderAtStart(r)
	if err != nil {
		return nil, "", err
	}
	cfg, format, err := image.DecodeConfig(seeker)
	if err != nil {
		return nil, "", err
	}
	if err := ValidateImageDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, format, err
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return nil, format, err
	}
	img, err := imaging.Decode(seeker, opts...)
	if err != nil {
		return nil, format, err
	}
	decoded := img.Bounds()
	if err := ValidateImageDimensions(decoded.Dx(), decoded.Dy()); err != nil {
		return nil, format, err
	}
	return img, format, nil
}

// DecodeImageFileWithinBudget opens path and decodes it only after the header
// dimensions pass ValidateImageDimensions.
func DecodeImageFileWithinBudget(path string, opts ...imaging.DecodeOption) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := DecodeImageWithinBudget(file, opts...)
	return img, err
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
