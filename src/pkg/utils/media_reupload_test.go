package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/util/cbcutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
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

func TestIsMediaHashMismatch(t *testing.T) {
	require.True(t, IsMediaHashMismatch(whatsmeow.ErrInvalidMediaEncSHA256))
	require.True(t, IsMediaHashMismatch(fmt.Errorf("last host: %w", whatsmeow.ErrInvalidMediaSHA256)))
	require.False(t, IsMediaHashMismatch(whatsmeow.ErrInvalidMediaHMAC))
	require.False(t, IsMediaHashMismatch(whatsmeow.ErrMediaDownloadFailedWith403))
}
