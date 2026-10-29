package utils_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"math"
	"mime/multipart"
	"net/textproto"
	"testing"

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

type seekCountReader struct {
	*bytes.Reader
	seeks int
}

func (s *seekCountReader) Seek(offset int64, whence int) (int64, error) {
	s.seeks++
	return s.Reader.Seek(offset, whence)
}

func TestValidateImageDimensions(t *testing.T) {
	require.NoError(t, utils.ValidateImageDimensions(8000, 5000))
	require.NoError(t, utils.ValidateImageDimensions(utils.MaxImageEdge, 1220))
	require.ErrorIs(t, utils.ValidateImageDimensions(utils.MaxImageEdge+1, 1), utils.ErrImageTooLarge)
	require.ErrorIs(t, utils.ValidateImageDimensions(1, utils.MaxImageEdge+1), utils.ErrImageTooLarge)
	require.ErrorIs(t, utils.ValidateImageDimensions(8000, 5001), utils.ErrImageTooLarge)
	require.ErrorIs(t, utils.ValidateImageDimensions(utils.MaxImageEdge, utils.MaxImageEdge), utils.ErrImageTooLarge)
	require.ErrorIs(t, utils.ValidateImageDimensions(0, 10), utils.ErrImageTooLarge)
	require.ErrorIs(t, utils.ValidateImageDimensions(math.MaxInt, 2), utils.ErrImageTooLarge)
	require.ErrorIs(t, utils.ValidateImageDimensions(math.MaxInt, math.MaxInt), utils.ErrImageTooLarge)
}

func TestDecodeImageWithinBudgetRejectsOversizedHeaderBeforeDecode(t *testing.T) {
	header := pngWithIHDR(20000, 20000)
	reader := &seekCountReader{Reader: bytes.NewReader(header)}
	img, _, err := utils.DecodeImageWithinBudget(reader)
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
	require.Nil(t, img)
	require.Zero(t, reader.seeks, "full decode seeks back to the start; rejection must happen first")
}

// jpegHeader is SOI, a JFIF APP0 (so DecodeConfig stops at SOF), and one SOF
// segment with the given sampling factor byte per component. It carries no
// scan data.
func jpegHeader(sofMarker byte, width, height uint16, sampling ...byte) []byte {
	out := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 1, 1, 0, 0, 1, 0, 1, 0, 0}
	sof := []byte{8, byte(height >> 8), byte(height), byte(width >> 8), byte(width), byte(len(sampling))}
	for i, hv := range sampling {
		sof = append(sof, byte(i+1), hv, 0)
	}
	out = append(out, 0xff, sofMarker, 0, byte(len(sof)+2))
	return append(out, sof...)
}

func TestDecodeImageWithinBudgetRejectsJPEGOverDecoderMemoryBudget(t *testing.T) {
	// 6000x6600 is under the pixel budget, but a progressive CMYK decode
	// allocates about 900 MiB.
	cmyk := jpegHeader(0xc2, 6000, 6600, 0x11, 0x11, 0x11, 0x11)
	img, _, err := utils.DecodeImageWithinBudget(bytes.NewReader(cmyk))
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
	require.Nil(t, img)

	// A 40 MP progressive 4:2:0 photo stays inside the budget; the header-only
	// fixture then fails in the decoder for lack of scan data.
	photo := jpegHeader(0xc2, 8000, 5000, 0x22, 0x11, 0x11)
	_, _, err = utils.DecodeImageWithinBudget(bytes.NewReader(photo))
	require.Error(t, err)
	require.NotErrorIs(t, err, utils.ErrImageTooLarge)
}

func TestDecodeImageWithinBudgetGivesJPEGItsOwnPixelCap(t *testing.T) {
	// A 48 MP baseline 4:2:0 phone photo is past MaxImagePixels but needs about
	// 70 MiB to decode, so it passes the header checks.
	phone := jpegHeader(0xc0, 8064, 6048, 0x22, 0x11, 0x11)
	_, _, err := utils.DecodeImageWithinBudget(bytes.NewReader(phone))
	require.Error(t, err)
	require.NotErrorIs(t, err, utils.ErrImageTooLarge)
	require.NoError(t, utils.ValidateGroupPhotoFormat(multipartImage(t, "phone.jpg", "image/jpeg", phone)))

	// A 96 MP grayscale JPEG decodes in about 92 MiB, but rotating or cropping
	// it would copy 384 MB, so the JPEG pixel cap refuses it.
	_, _, err = utils.DecodeImageWithinBudget(bytes.NewReader(jpegHeader(0xc0, 16000, 6000, 0x11)))
	require.ErrorIs(t, err, utils.ErrImageTooLarge)

	// Other formats keep the pixel cap; a long screenshot fits the edge cap.
	_, _, err = utils.DecodeImageWithinBudget(bytes.NewReader(pngWithIHDR(8064, 6048)))
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
	_, _, err = utils.DecodeImageWithinBudget(bytes.NewReader(pngWithIHDR(1080, 16000)))
	require.Error(t, err)
	require.NotErrorIs(t, err, utils.ErrImageTooLarge)
}

func TestDecodeImageWithinBudgetDecodesAcceptedImage(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 3, 2))))
	img, format, err := utils.DecodeImageWithinBudget(bytes.NewReader(encoded.Bytes()))
	require.NoError(t, err)
	require.Equal(t, "png", format)
	require.Equal(t, 3, img.Bounds().Dx())
	require.Equal(t, 2, img.Bounds().Dy())
}

func TestThumbnailSizeDoesNotUpscaleOrExplode(t *testing.T) {
	tests := []struct {
		name       string
		srcW, srcH int
		maxW, maxH int
	}{
		{name: "landscape photo", srcW: 4000, srcH: 3000, maxW: 100, maxH: 75},
		{name: "portrait photo", srcW: 3024, srcH: 4032, maxW: 100, maxH: 133},
		{name: "small image is not upscaled", srcW: 20, srcH: 20, maxW: 20, maxH: 20},
		{name: "narrow source keeps a short height", srcW: 1, srcH: 8192, maxW: utils.ThumbnailMaxWidth, maxH: utils.ThumbnailMaxHeight},
		{name: "wide source keeps a short height", srcW: 8192, srcH: 1, maxW: utils.ThumbnailMaxWidth, maxH: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH := utils.ThumbnailSize(tt.srcW, tt.srcH)
			require.LessOrEqual(t, gotW, tt.maxW)
			require.LessOrEqual(t, gotH, tt.maxH)
			require.LessOrEqual(t, gotW, utils.ThumbnailMaxWidth)
			require.LessOrEqual(t, gotH, utils.ThumbnailMaxHeight)
			require.LessOrEqual(t, int64(gotW)*int64(gotH), int64(utils.ThumbnailMaxWidth)*int64(utils.ThumbnailMaxHeight))
			require.Greater(t, gotW, 0)
			require.Greater(t, gotH, 0)
		})
	}
}

func TestFitThumbnailCapsNarrowImage(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 8192))
	got := utils.FitThumbnail(src)
	require.LessOrEqual(t, got.Bounds().Dx(), utils.ThumbnailMaxWidth)
	require.LessOrEqual(t, got.Bounds().Dy(), utils.ThumbnailMaxHeight)
	require.Less(t, got.Bounds().Dy(), 1000)
}

func multipartImage(t *testing.T, filename, contentType string, data []byte) *multipart.FileHeader {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="photo"; filename="%s"`, filename))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	form, err := multipart.NewReader(body, writer.Boundary()).ReadForm(int64(len(data)) + 4096)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })
	files := form.File["photo"]
	require.NotEmpty(t, files)
	return files[0]
}

func TestProcessGroupPhotoRejectsOversizedHeader(t *testing.T) {
	file := multipartImage(t, "big.png", "image/png", pngWithIHDR(20000, 20000))
	require.ErrorIs(t, utils.ValidateGroupPhotoFormat(file), utils.ErrImageTooLarge)
	_, err := utils.ProcessGroupPhoto(file)
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
}

func TestProcessGroupPhotoAcceptsSmallPNG(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 16, 8))))
	file := multipartImage(t, "ok.png", "image/png", encoded.Bytes())
	require.NoError(t, utils.ValidateGroupPhotoFormat(file))
	buf, err := utils.ProcessGroupPhoto(file)
	require.NoError(t, err)
	require.GreaterOrEqual(t, buf.Len(), 3)
	require.Equal(t, []byte{0xff, 0xd8, 0xff}, buf.Bytes()[:3])
}
