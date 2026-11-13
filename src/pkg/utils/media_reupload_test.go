package utils

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/util/cbcutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	"google.golang.org/protobuf/proto"
)

// encryptTestMedia encrypts a file the way WhatsApp clients do: AES-CBC with
// keys derived from the media key, followed by a 10-byte HMAC.
func encryptTestMedia(t *testing.T, plaintext, mediaKey []byte, mediaType whatsmeow.MediaType) []byte {
	t.Helper()
	expanded := hkdfutil.SHA256(mediaKey, nil, []byte(mediaType), 112)
	iv, cipherKey, macKey := expanded[:16], expanded[16:48], expanded[48:80]
	ciphertext, err := cbcutil.Encrypt(cipherKey, iv, plaintext)
	require.NoError(t, err)
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	return append(ciphertext, h.Sum(nil)[:mediaHMACLength]...)
}

func TestDecryptAuthenticatedMediaAcceptsValidHMAC(t *testing.T) {
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	plaintext := []byte("a copy of the image the phone kept")
	encrypted := encryptTestMedia(t, plaintext, mediaKey, whatsmeow.MediaImage)

	data, err := decryptAuthenticatedMedia(encrypted, mediaKey, whatsmeow.MediaImage)

	require.NoError(t, err)
	require.Equal(t, plaintext, data)
}

func TestDecryptAuthenticatedMediaRejectsOtherKey(t *testing.T) {
	encrypted := encryptTestMedia(t, []byte("payload"), []byte("0123456789abcdef0123456789abcdef"), whatsmeow.MediaImage)

	_, err := decryptAuthenticatedMedia(encrypted, []byte("fedcba9876543210fedcba9876543210"), whatsmeow.MediaImage)

	require.ErrorIs(t, err, ErrInvalidReuploadedMediaHMAC)
}

func TestDecryptAuthenticatedMediaRejectsTamperedFile(t *testing.T) {
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	encrypted := encryptTestMedia(t, []byte("payload that will be tampered"), mediaKey, whatsmeow.MediaImage)
	encrypted[0] ^= 0xFF

	_, err := decryptAuthenticatedMedia(encrypted, mediaKey, whatsmeow.MediaImage)

	require.ErrorIs(t, err, ErrInvalidReuploadedMediaHMAC)
}

func TestDecryptAuthenticatedMediaRejectsWrongMediaType(t *testing.T) {
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	encrypted := encryptTestMedia(t, []byte("payload"), mediaKey, whatsmeow.MediaImage)

	_, err := decryptAuthenticatedMedia(encrypted, mediaKey, whatsmeow.MediaVideo)

	require.ErrorIs(t, err, ErrInvalidReuploadedMediaHMAC)
}

func TestDecryptAuthenticatedMediaRejectsShortFile(t *testing.T) {
	_, err := decryptAuthenticatedMedia([]byte("short"), []byte("0123456789abcdef0123456789abcdef"), whatsmeow.MediaImage)

	require.ErrorIs(t, err, whatsmeow.ErrTooShortFile)
}

// fakeMediaDownloader plays whatsmeow's media hosts.
type fakeMediaDownloader struct {
	hosts     []string
	responses map[string]func() (*http.Response, error)
	requested []string
}

func (f *fakeMediaDownloader) RefreshMediaConn(context.Context, bool) (*whatsmeow.MediaConn, error) {
	conn := &whatsmeow.MediaConn{}
	for _, host := range f.hosts {
		conn.Hosts = append(conn.Hosts, whatsmeow.MediaConnHost{Hostname: host})
	}
	return conn, nil
}

func (f *fakeMediaDownloader) DoMediaDownloadRequest(_ context.Context, mediaURL string) (*http.Response, error) {
	f.requested = append(f.requested, mediaURL)
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return nil, err
	}
	if respond, ok := f.responses[parsed.Host]; ok {
		return respond()
	}
	return nil, errors.New("connection refused")
}

func okResponse(body []byte) func() (*http.Response, error) {
	return func() (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
	}
}

func TestDownloadReuploadedMediaReturnsHashesOfChangedCopy(t *testing.T) {
	mediaKey := []byte("0123456789abcdef0123456789abcdef")
	plaintext := []byte("\xff\xd8\xff\xe0 the copy the phone kept")
	encrypted := encryptTestMedia(t, plaintext, mediaKey, whatsmeow.MediaImage)
	wantEnc := sha256.Sum256(encrypted)
	stale := sha256.Sum256([]byte("original file"))

	downloader := &fakeMediaDownloader{
		hosts:     []string{"down.example", "mmg.example"},
		responses: map[string]func() (*http.Response, error){"mmg.example": okResponse(append([]byte(nil), encrypted...))},
	}
	image := &waE2E.ImageMessage{
		Mimetype:      proto.String("image/jpeg"),
		MediaKey:      mediaKey,
		FileSHA256:    stale[:],
		FileEncSHA256: stale[:],
	}

	media, err := downloadReuploadedMedia(context.Background(), downloader, "/v/t62.7118-24/fresh.enc?oh=x", image, t.TempDir())

	require.NoError(t, err)
	wantPlain := sha256.Sum256(plaintext)
	require.Equal(t, wantPlain[:], media.FileSHA256)
	require.Equal(t, wantEnc[:], media.FileEncSHA256)
	require.Equal(t, uint64(len(plaintext)), media.FileLength)
	stored, err := os.ReadFile(media.MediaPath)
	require.NoError(t, err)
	require.Equal(t, plaintext, stored)

	require.Len(t, downloader.requested, 2, "a network error on one host moves on to the next")
	last, err := url.Parse(downloader.requested[1])
	require.NoError(t, err)
	require.Equal(t, "/v/t62.7118-24/fresh.enc", last.Path)
	require.Equal(t, "x", last.Query().Get("oh"))
	require.Equal(t, "image", last.Query().Get("mms-type"))
}

func TestDownloadReuploadedMediaStopsOnGoneFile(t *testing.T) {
	gone := func() (*http.Response, error) { return nil, whatsmeow.ErrMediaDownloadFailedWith410 }
	downloader := &fakeMediaDownloader{
		hosts:     []string{"a.example", "b.example"},
		responses: map[string]func() (*http.Response, error){"a.example": gone, "b.example": gone},
	}

	_, err := downloadReuploadedMedia(context.Background(), downloader, "/v/gone.enc", &waE2E.ImageMessage{MediaKey: []byte("k")}, t.TempDir())

	require.ErrorIs(t, err, whatsmeow.ErrMediaDownloadFailedWith410)
	require.Len(t, downloader.requested, 1, "another host cannot serve a file that is gone")
}

func TestDownloadReuploadedMediaRejectsInvalidPaths(t *testing.T) {
	for _, path := range []string{"", "v/no-slash", "//evil.example/x"} {
		_, err := downloadReuploadedMedia(context.Background(), &fakeMediaDownloader{}, path, &waE2E.ImageMessage{}, t.TempDir())
		require.ErrorContains(t, err, "invalid media direct path", "path %q", path)
	}
}

func TestIsExpiredMediaError(t *testing.T) {
	for _, err := range []error{
		whatsmeow.ErrMediaDownloadFailedWith403,
		whatsmeow.ErrMediaDownloadFailedWith404,
		whatsmeow.ErrMediaDownloadFailedWith410,
		fmt.Errorf("wrapped: %w", whatsmeow.ErrMediaDownloadFailedWith410),
	} {
		require.True(t, IsExpiredMediaError(err), err.Error())
	}
	require.False(t, IsExpiredMediaError(whatsmeow.ErrInvalidMediaSHA256))
	require.False(t, IsExpiredMediaError(errors.New("other")))
}

func TestReuploadedMediaQueryMatchesWhatsmeow(t *testing.T) {
	hash := []byte{0xfb, 0xff}
	require.Equal(t, "&hash=-_8=&mms-type=image&__wa-mms=", reuploadedMediaQuery("/p?oh=1", hash, whatsmeow.MediaImage))
	require.Equal(t, "?hash=-_8=&mms-type=video&__wa-mms=", reuploadedMediaQuery("/p", hash, whatsmeow.MediaVideo))
}
