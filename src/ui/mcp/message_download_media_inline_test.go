package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tiny real PNG (1x1, transparent) - enough bytes for os.ReadFile and a real mime sniff by
// extension, without needing a large fixture.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

// downloadMediaStub lets each test case control exactly what DownloadMedia hands back, unlike
// stubMessageService's own fixed "/tmp/x.jpg" response.
type downloadMediaStub struct {
	domainMessage.IMessageUsecase
	resp domainMessage.DownloadMediaResponse
}

func (s *downloadMediaStub) DownloadMedia(_ context.Context, _ domainMessage.DownloadMediaRequest) (domainMessage.DownloadMediaResponse, error) {
	return s.resp, nil
}

func writeTempImage(t *testing.T, data []byte, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestDownloadMediaInlinesASmallImage(t *testing.T) {
	path := writeTempImage(t, onePixelPNG, "a.png")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "image", Filename: "a.png", FilePath: path, FileSize: int64(len(onePixelPNG)),
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	require.False(t, res.IsError)

	var found *mcpg.ImageContent
	for _, c := range res.Content {
		if img, ok := mcpg.AsImageContent(c); ok {
			found = img
		}
	}
	require.NotNil(t, found, "expected an inline image content block")
	assert.Equal(t, "image/png", found.MIMEType)
	assert.NotEmpty(t, found.Data)
}

func TestDownloadMediaSkipsInliningOverTheSizeCap(t *testing.T) {
	path := writeTempImage(t, onePixelPNG, "a.png")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "image", Filename: "a.png", FilePath: path,
		FileSize: maxInlineImageBytes + 1, // claims to be over the cap, even though the real file is tiny
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	require.False(t, res.IsError)

	for _, c := range res.Content {
		_, ok := mcpg.AsImageContent(c)
		assert.False(t, ok, "an over-cap response must not be inlined, regardless of the real file's size")
	}
}

func TestDownloadMediaSkipsInliningForNonImageMedia(t *testing.T) {
	path := writeTempImage(t, onePixelPNG, "a.ogg")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "audio", Filename: "a.ogg", FilePath: path, FileSize: int64(len(onePixelPNG)),
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	require.False(t, res.IsError)

	for _, c := range res.Content {
		_, ok := mcpg.AsImageContent(c)
		assert.False(t, ok, "a voice note must not be inlined as an image")
	}
}

func TestDownloadMediaFallsBackCleanlyWhenTheFileIsMissing(t *testing.T) {
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "image", Filename: "gone.png",
		FilePath: filepath.Join(t.TempDir(), "gone.png"), // never written
		FileSize: 123,
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError, "a read failure falls back to the structured response, not an error")
}
