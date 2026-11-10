package utils

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

const (
	// mediaHMACLength matches whatsmeow: encrypted media ends with a 10-byte HMAC.
	mediaHMACLength = 10
	// aesBlockSize is the most PKCS#7 padding can add to the plaintext.
	aesBlockSize = 16
)

// ErrInvalidReuploadedMediaHMAC means the re-uploaded file was not encrypted
// with the message's media key, so it cannot be trusted.
var ErrInvalidReuploadedMediaHMAC = errors.New("re-uploaded media failed HMAC validation")

var errFileTooLarge = errors.New("file size exceeds the maximum download limit")

// ReuploadedMedia is a stored re-uploaded file plus the integrity metadata of
// the copy the phone actually uploaded, which can differ from the original.
type ReuploadedMedia struct {
	ExtractedMedia
	FileSHA256    []byte
	FileEncSHA256 []byte
	FileLength    uint64
}

// mediaDownloader is the part of whatsmeow's internals used to fetch media
// with the client's own hosts, HTTP client, proxy and headers.
type mediaDownloader interface {
	RefreshMediaConn(ctx context.Context, force bool) (*whatsmeow.MediaConn, error)
	DoMediaDownloadRequest(ctx context.Context, url string) (*http.Response, error)
}

// DownloadReuploadedMedia downloads media from the direct path the phone
// returned after a re-upload. The phone can re-upload a slightly different
// copy than the file first sent, which whatsmeow's Download rejects because
// the hashes no longer match the message. The file is instead authenticated
// with the HMAC derived from the message's media key, which only the message
// participants hold, and its hashes are computed from the downloaded copy.
// The request goes through whatsmeow's media hosts and HTTP client, so
// WHATSAPP_PROXY and whatsmeow's request headers apply.
func DownloadReuploadedMedia(ctx context.Context, client *whatsmeow.Client, directPath string, mediaFile whatsmeow.DownloadableMessage, storageLocation string) (ReuploadedMedia, error) {
	if client == nil {
		return ReuploadedMedia{}, whatsmeow.ErrClientIsNil
	}
	return downloadReuploadedMedia(ctx, client.DangerousInternals(), directPath, mediaFile, storageLocation)
}

func downloadReuploadedMedia(ctx context.Context, downloader mediaDownloader, directPath string, mediaFile whatsmeow.DownloadableMessage, storageLocation string) (ReuploadedMedia, error) {
	if mediaFile == nil {
		return ReuploadedMedia{}, errors.New("media message is missing")
	}
	if !strings.HasPrefix(directPath, "/") || strings.HasPrefix(directPath, "//") {
		return ReuploadedMedia{}, fmt.Errorf("invalid media direct path: %q", directPath)
	}

	mediaType := whatsmeow.GetMediaType(mediaFile)
	encrypted, err := fetchEncryptedMedia(ctx, downloader, directPath, mediaFile.GetFileEncSHA256(), mediaType)
	if err != nil {
		return ReuploadedMedia{}, err
	}
	// Hash before decrypting: cbcutil.Decrypt works in place on this buffer.
	fileEncSHA256 := sha256.Sum256(encrypted)

	data, err := decryptAuthenticatedMedia(encrypted, mediaFile.GetMediaKey(), mediaType)
	if err != nil {
		return ReuploadedMedia{}, err
	}
	extracted, err := storeExtractedMedia(storageLocation, mediaFile, data)
	if err != nil {
		return ReuploadedMedia{}, err
	}

	fileSHA256 := sha256.Sum256(data)
	return ReuploadedMedia{
		ExtractedMedia: extracted,
		FileSHA256:     fileSHA256[:],
		FileEncSHA256:  fileEncSHA256[:],
		FileLength:     uint64(len(data)),
	}, nil
}

// fetchEncryptedMedia tries each host of the media connection, like
// whatsmeow's DownloadMediaWithPath, and returns the encrypted file.
func fetchEncryptedMedia(ctx context.Context, downloader mediaDownloader, directPath string, fileEncSHA256 []byte, mediaType whatsmeow.MediaType) ([]byte, error) {
	mediaConn, err := downloader.RefreshMediaConn(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh media connections: %w", err)
	}
	if mediaConn == nil || len(mediaConn.Hosts) == 0 {
		return nil, errors.New("no media hosts available")
	}

	query := reuploadedMediaQuery(directPath, fileEncSHA256, mediaType)
	var lastErr error
	for _, host := range mediaConn.Hosts {
		encrypted, err := fetchFromMediaHost(ctx, downloader, "https://"+host.Hostname+directPath+query)
		if err == nil {
			return encrypted, nil
		}
		lastErr = err
		if ctx.Err() != nil || isFinalMediaFetchError(err) {
			break
		}
	}
	return nil, fmt.Errorf("failed to fetch re-uploaded media: %w", lastErr)
}

func fetchFromMediaHost(ctx context.Context, downloader mediaDownloader, mediaURL string) ([]byte, error) {
	response, err := downloader.DoMediaDownloadRequest(ctx, mediaURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	maxEncryptedSize := config.WhatsappSettingMaxDownloadSize + aesBlockSize + mediaHMACLength
	encrypted, err := io.ReadAll(io.LimitReader(response.Body, maxEncryptedSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(encrypted)) > maxEncryptedSize {
		return nil, errFileTooLarge
	}
	return encrypted, nil
}

// isFinalMediaFetchError reports errors another host would not fix: the file
// is gone (403/404/410) or too large.
func isFinalMediaFetchError(err error) bool {
	return errors.Is(err, errFileTooLarge) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410)
}

// reuploadedMediaQuery appends the parameters whatsmeow adds to media
// download URLs: the stored ciphertext hash, the MMS type and the marker.
func reuploadedMediaQuery(directPath string, fileEncSHA256 []byte, mediaType whatsmeow.MediaType) string {
	separator := "&"
	if !strings.Contains(directPath, "?") {
		separator = "?"
	}
	return fmt.Sprintf("%shash=%s&mms-type=%s&__wa-mms=", separator,
		base64.URLEncoding.EncodeToString(fileEncSHA256), reuploadedMediaMMSType[mediaType])
}

// reuploadedMediaMMSType mirrors whatsmeow's media type to MMS type mapping
// for the media kinds GOWA downloads.
var reuploadedMediaMMSType = map[whatsmeow.MediaType]string{
	whatsmeow.MediaImage:    "image",
	whatsmeow.MediaAudio:    "audio",
	whatsmeow.MediaVideo:    "video",
	whatsmeow.MediaDocument: "document",
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
	// mediaType is whatsmeow's HKDF info label, e.g. "WhatsApp Image Keys".
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
