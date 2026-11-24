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
	assert.Equal(t, "application/octet-stream", audioMimeTypeFor("audio", "notes.txt"))
	assert.Equal(t, "application/octet-stream", audioMimeTypeFor("audio", ""))
	assert.Equal(t, "audio/ogg", audioMimeTypeFor("audio", "voice.OGG"), "extension match is case-insensitive")
}

// A real voice note from Geathe Langridge (27765925492), downloaded through the live
// download_media path on 2026-11-24, came back with a generated filename carrying no
// extension at all ("1795553037-e5afd729-92b1-43c8-8dc6-2f96168b89c3") - WhatsApp's own
// AudioMessage.Mimetype was apparently empty for that sender, so ExtractMedia's own
// extension-detection (pkg/utils/whatsapp.go's determineMediaExtension) had nothing to
// fall back to either. audioMimeTypeFor then silently mislabelled the inline block
// application/octet-stream instead of audio/ogg. A WhatsApp voice note ("ptt") is always
// Opus-in-Ogg regardless of what any particular sender's client reports, so that is a safe
// fallback when the filename carries no recognisable extension.
func TestAudioMimeTypeFallsBackToOggForAVoiceNoteWithNoFileExtension(t *testing.T) {
	assert.Equal(t, "audio/ogg", audioMimeTypeFor("ptt", "1795553037-e5afd729-92b1-43c8-8dc6-2f96168b89c3"))
}

func TestAudioMimeTypeForGenericAudioWithNoExtensionStaysUnknown(t *testing.T) {
	// Unlike "ptt", a plain "audio" message isn't guaranteed to be Ogg/Opus (could be an
	// mp3, m4a, etc. shared as a file), so with no extension to go on this stays the
	// generic fallback rather than guessing a specific codec.
	assert.Equal(t, "application/octet-stream", audioMimeTypeFor("audio", "1795553037-e5afd729-92b1-43c8-8dc6-2f96168b89c3"))
}

func TestDownloadMediaInlinesAVoiceNoteWithNoFileExtension(t *testing.T) {
	path := writeTempImage(t, fakeAudioBytes, "1795553037-e5afd729-92b1-43c8-8dc6-2f96168b89c3")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt",
		Filename: "1795553037-e5afd729-92b1-43c8-8dc6-2f96168b89c3", // matches a real download's generated name
		FilePath: path, FileSize: int64(len(fakeAudioBytes)),
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
	assert.Equal(t, "audio/ogg", found.MIMEType, "a voice note must not be mislabelled application/octet-stream just because its downloaded filename lost its extension")
}
