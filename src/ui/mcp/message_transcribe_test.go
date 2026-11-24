package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/transcribe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTranscriber lets each test control what the transcription call returns
// without a real network client, and records whether it was called at all.
type stubTranscriber struct {
	text    string
	err     error
	called  bool
	lastExt string
}

func (s *stubTranscriber) Transcribe(_ context.Context, _ []byte, filename string) (string, error) {
	s.called = true
	s.lastExt = filename
	return s.text, s.err
}

func writeTempAudio(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte("fake audio bytes"), 0o644))
	return path
}

func TestDownloadMediaTranscribesAVoiceNote(t *testing.T) {
	path := writeTempAudio(t, "voice.ogg")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt", Filename: "voice.ogg", FilePath: path, FileSize: 17,
	}}
	stub := &stubTranscriber{text: "I'd like to apply for the bookkeeper role"}
	h := &MessageHandler{messageService: svc, resolver: &stubResolver{}, transcriber: stub}

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.True(t, stub.called)
	assert.Equal(t, "voice.ogg", stub.lastExt)
	assert.Contains(t, resultText(t, res), "Transcript: I'd like to apply for the bookkeeper role")
}

func TestDownloadMediaTranscribesAnAudioFile(t *testing.T) {
	path := writeTempAudio(t, "clip.mp3")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "audio", Filename: "clip.mp3", FilePath: path, FileSize: 17,
	}}
	stub := &stubTranscriber{text: "hello"}
	h := &MessageHandler{messageService: svc, resolver: &stubResolver{}, transcriber: stub}

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.True(t, stub.called)
	assert.Contains(t, resultText(t, res), "Transcript: hello")
}

func TestDownloadMediaSkipsTranscriptionWhenNotConfigured(t *testing.T) {
	path := writeTempAudio(t, "voice.ogg")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt", Filename: "voice.ogg", FilePath: path, FileSize: 17,
	}}
	stub := &stubTranscriber{err: transcribe.ErrNotConfigured}
	h := &MessageHandler{messageService: svc, resolver: &stubResolver{}, transcriber: stub}

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.NotContains(t, resultText(t, res), "Transcript:")
}

func TestDownloadMediaReportsATranscriptionFailure(t *testing.T) {
	path := writeTempAudio(t, "voice.ogg")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt", Filename: "voice.ogg", FilePath: path, FileSize: 17,
	}}
	stub := &stubTranscriber{err: errors.New("network unreachable")}
	h := &MessageHandler{messageService: svc, resolver: &stubResolver{}, transcriber: stub}

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError, "the media still downloaded fine; only the transcript failed")
	assert.Contains(t, resultText(t, res), "transcription failed")
	assert.Contains(t, resultText(t, res), "network unreachable")
}

func TestDownloadMediaSkipsTranscriptionOverTheSizeCap(t *testing.T) {
	path := writeTempAudio(t, "voice.ogg")
	svc := &downloadMediaStub{resp: domainMessage.DownloadMediaResponse{
		MessageID: "M1", MediaType: "ptt", Filename: "voice.ogg", FilePath: path,
		FileSize: maxTranscribeAudioBytes + 1, // claims to be over the cap, even though the real file is tiny
	}}
	stub := &stubTranscriber{text: "should never be read"}
	h := &MessageHandler{messageService: svc, resolver: &stubResolver{}, transcriber: stub}

	res, err := h.handleMessage(deviceCtx(), callReq(map[string]any{
		"action": "download_media", "phone": "628", "message_id": "M1",
	}))
	require.NoError(t, err)
	assert.False(t, res.IsError)
	assert.False(t, stub.called, "an over-cap voice note must not be sent off for transcription")
	assert.NotContains(t, resultText(t, res), "Transcript:")
}
