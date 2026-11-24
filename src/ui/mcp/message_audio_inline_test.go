package mcp

import (
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fakeAudioBytes = []byte("not real audio, just needs to be non-empty bytes")

func TestDownloadMediaInlinesAVoiceNote(t *testing.T) {
	path := writeTempImage(t, fakeAudioBytes, "voice.ogg")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt", Filename: "voice.ogg", FilePath: path, FileSize: int64(len(fakeAudioBytes)),
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	require.False(t, res.IsError)

	var found *mcpg.AudioContent
	for _, c := range res.Content {
		if audio, ok := mcpg.AsAudioContent(c); ok {
			found = audio
		}
	}
	require.NotNil(t, found, "expected an inline audio content block")
	assert.Equal(t, "audio/ogg", found.MIMEType)
	assert.NotEmpty(t, found.Data)
	assert.Contains(t, resultText(t, res), "Nothing transcribed it")
}

func TestDownloadMediaInlinesAnAudioFileByExtension(t *testing.T) {
	path := writeTempImage(t, fakeAudioBytes, "clip.mp3")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "audio", Filename: "clip.mp3", FilePath: path, FileSize: int64(len(fakeAudioBytes)),
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	require.False(t, res.IsError)

	var found *mcpg.AudioContent
	for _, c := range res.Content {
		if audio, ok := mcpg.AsAudioContent(c); ok {
			found = audio
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "audio/mpeg", found.MIMEType)
}

func TestDownloadMediaSkipsInliningAudioOverTheSizeCap(t *testing.T) {
	path := writeTempImage(t, fakeAudioBytes, "voice.ogg")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt", Filename: "voice.ogg", FilePath: path,
		FileSize: maxInlineAudioBytes + 1, // claims to be over the cap, even though the real file is tiny
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)

	for _, c := range res.Content {
		_, ok := mcpg.AsAudioContent(c)
		assert.False(t, ok, "an over-cap voice note must not be inlined")
	}
}

func TestDownloadMediaFallsBackCleanlyWhenTheAudioFileIsMissing(t *testing.T) {
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt", Filename: "gone.ogg",
		FilePath: "/nonexistent/gone.ogg", // never written
		FileSize: 123,
	}}
	h := InitMcpMessage(svc, &stubResolver{})

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError, "a read failure falls back to the structured response, not an error")
}

func TestAudioMimeTypeForUnknownExtensionFallsBack(t *testing.T) {
	assert.Equal(t, "application/octet-stream", audioMimeTypeFor("notes.txt"))
	assert.Equal(t, "application/octet-stream", audioMimeTypeFor(""))
	assert.Equal(t, "audio/ogg", audioMimeTypeFor("voice.OGG"), "extension match is case-insensitive")
}
