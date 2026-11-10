package utils

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/util/cbcutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
)

// reuploadedMediaHost is the WhatsApp media CDN host used to fetch a file the
// phone re-uploaded. whatsmeow keeps its host list private.
const reuploadedMediaHost = "https://mmg.whatsapp.net"

// mediaHMACLength matches whatsmeow: encrypted media ends with a 10-byte HMAC.
const mediaHMACLength = 10

// ErrInvalidReuploadedMediaHMAC means the re-uploaded file was not encrypted
// with the message's media key, so it cannot be trusted.
var ErrInvalidReuploadedMediaHMAC = errors.New("re-uploaded media failed HMAC validation")

// IsMediaHashMismatch reports whether a download failed only because the
// file's hashes differ from the ones recorded in the message.
func IsMediaHashMismatch(err error) bool {
	return errors.Is(err, whatsmeow.ErrInvalidMediaEncSHA256) || errors.Is(err, whatsmeow.ErrInvalidMediaSHA256)
}

// DownloadReuploadedMedia fetches media the phone re-uploaded whose bytes no
// longer match the hashes in the original message. Phones sometimes keep a
// slightly different copy of a file than the one first sent, and whatsmeow
// rejects any hash mismatch. Authenticity is still enforced: the file must
// carry a valid HMAC derived from the message's media key, which only the
// participants of the message hold.
func DownloadReuploadedMedia(ctx context.Context, directPath string, mediaFile whatsmeow.DownloadableMessage, storageLocation string) (ExtractedMedia, error) {
	if mediaFile == nil {
		return ExtractedMedia{}, errors.New("media message is missing")
	}
	if !strings.HasPrefix(directPath, "/") {
		return ExtractedMedia{}, fmt.Errorf("media download path does not start with slash: %s", directPath)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reuploadedMediaHost+directPath, nil)
	if err != nil {
		return ExtractedMedia{}, fmt.Errorf("failed to prepare request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ExtractedMedia{}, fmt.Errorf("failed to fetch re-uploaded media: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ExtractedMedia{}, fmt.Errorf("failed to fetch re-uploaded media: status code %d", response.StatusCode)
	}

	maxEncryptedSize := config.WhatsappSettingMaxDownloadSize + mediaHMACLength + 16
	encrypted, err := io.ReadAll(io.LimitReader(response.Body, maxEncryptedSize+1))
	if err != nil {
		return ExtractedMedia{}, fmt.Errorf("failed to read re-uploaded media: %w", err)
	}
	if int64(len(encrypted)) > maxEncryptedSize {
		return ExtractedMedia{}, fmt.Errorf("file size exceeds the maximum limit of %d bytes", config.WhatsappSettingMaxDownloadSize)
	}

	data, err := decryptAuthenticatedMedia(encrypted, mediaFile.GetMediaKey(), whatsmeow.GetMediaType(mediaFile))
	if err != nil {
		return ExtractedMedia{}, err
	}
	return storeExtractedMedia(storageLocation, mediaFile, data)
}

// decryptAuthenticatedMedia validates the trailing HMAC and decrypts the file,
// without comparing it to the hashes recorded in the original message.
func decryptAuthenticatedMedia(encrypted, mediaKey []byte, mediaType whatsmeow.MediaType) ([]byte, error) {
	if len(mediaKey) == 0 {
		return nil, errors.New("media key is missing")
	}
	if len(encrypted) <= mediaHMACLength {
		return nil, whatsmeow.ErrTooShortFile
	}
	expanded := hkdfutil.SHA256(mediaKey, nil, []byte(mediaType), 112)
	iv, cipherKey, macKey := expanded[:16], expanded[16:48], expanded[48:80]

	ciphertext, mac := encrypted[:len(encrypted)-mediaHMACLength], encrypted[len(encrypted)-mediaHMACLength:]
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ciphertext)
	if !hmac.Equal(h.Sum(nil)[:mediaHMACLength], mac) {
		return nil, ErrInvalidReuploadedMediaHMAC
	}

	data, err := cbcutil.Decrypt(cipherKey, iv, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt re-uploaded media: %w", err)
	}
	return data, nil
}
