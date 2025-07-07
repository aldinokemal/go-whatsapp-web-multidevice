package utils

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"

	"github.com/disintegration/imaging"
)

const (
	// WhatsApp group photo constraints
	MaxGroupPhotoSize      = 100 * 1024 // 100KB
	GroupPhotoQuality      = 80         // JPEG quality
	MaxGroupPhotoDimension = 640        // Max width/height in pixels
)

// ProcessGroupPhoto processes an image for WhatsApp group photo requirements:
// - Converts to JPEG format
// - Crops to 1:1 aspect ratio (square)
// - Resizes to fit within MaxGroupPhotoDimension
// - Compresses to stay under MaxGroupPhotoSize
func ProcessGroupPhoto(file *multipart.FileHeader) (*bytes.Buffer, error) {
	// Open the uploaded file
	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	// Decode the image
	img, format, err := image.Decode(src)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Handle different input formats
	switch format {
	case "jpeg", "png", "webp":
		// Supported formats
	default:
		return nil, fmt.Errorf("unsupported image format: %s (only JPEG, PNG, and WebP are supported)", format)
	}

	// Crop to 1:1 aspect ratio (square)
	img = cropToSquare(img)

	// Resize if too large
	if img.Bounds().Dx() > MaxGroupPhotoDimension || img.Bounds().Dy() > MaxGroupPhotoDimension {
		img = imaging.Resize(img, MaxGroupPhotoDimension, MaxGroupPhotoDimension, imaging.Lanczos)
	}

	// Convert to JPEG with compression
	return compressToJPEG(img, GroupPhotoQuality)
}

// cropToSquare crops an image to a 1:1 aspect ratio, keeping the center
func cropToSquare(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// If already square, return as-is
	if width == height {
		return img
	}

	// Determine the size of the square (minimum of width and height)
	size := width
	if height < width {
		size = height
	}

	// Calculate crop position to center the square
	x := (width - size) / 2
	y := (height - size) / 2

	// Crop to square
	cropRect := image.Rect(x, y, x+size, y+size)
	return imaging.Crop(img, cropRect)
}

// compressToJPEG converts an image to JPEG format with specified quality
// and tries to compress it to stay under the file size limit
func compressToJPEG(img image.Image, quality int) (*bytes.Buffer, error) {
	return compressToJPEGWithDepth(img, quality, 0)
}

func compressToJPEGWithDepth(img image.Image, quality int, depth int) (*bytes.Buffer, error) {
	const maxDepth = 10 // Prevent infinite recursion
	if depth > maxDepth {
		return nil, fmt.Errorf("exceeded maximum compression attempts")
	}

	var buf bytes.Buffer

	// Try with initial quality
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("failed to encode JPEG: %w", err)
	}

	// If file is too large, reduce quality and try again
	if buf.Len() > MaxGroupPhotoSize && quality > 10 {
		return compressToJPEGWithDepth(img, quality-10, depth+1)
	}

	// If still too large even at low quality, resize the image smaller
	if buf.Len() > MaxGroupPhotoSize {
		bounds := img.Bounds()
		newSize := int(float64(bounds.Dx()) * 0.8) // Reduce by 20%
		if newSize < 100 {
			return nil, fmt.Errorf("image cannot be compressed enough to meet WhatsApp requirements (max %d bytes)", MaxGroupPhotoSize)
		}

		resized := imaging.Resize(img, newSize, newSize, imaging.Lanczos)
		return compressToJPEGWithDepth(resized, quality, depth+1)
	}

	return &buf, nil
}

// ValidateGroupPhotoFormat checks if the uploaded file is a supported image format
func ValidateGroupPhotoFormat(file *multipart.FileHeader) error {
	if file == nil {
		return nil // Photo is optional (can be nil to remove photo)
	}

	// Check content type
	contentType := file.Header.Get("Content-Type")
	supportedTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/webp": true,
	}

	if contentType != "" && !supportedTypes[contentType] {
		return fmt.Errorf("unsupported image format: %s (supported: JPEG, PNG, WebP)", contentType)
	}

	// Check file size (before processing)
	if file.Size > 10*1024*1024 { // 10MB limit for input
		return fmt.Errorf("image file too large: %d bytes (max 10MB for processing)", file.Size)
	}

	// Try to decode the image to verify it's a valid image
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	// Read only the first few bytes to detect format
	header := make([]byte, 512)
	_, err = src.Read(header)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read file header: %w", err)
	}

	// Reset to beginning
	if _, err := src.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to reset file position: %w", err)
	}

	// Detect image format from header
	_, format, err := image.DecodeConfig(src)
	if err != nil {
		return fmt.Errorf("invalid image file: %w", err)
	}

	if format != "jpeg" && format != "png" && format != "webp" {
		return fmt.Errorf("unsupported image format detected: %s", format)
	}

	return nil
}
