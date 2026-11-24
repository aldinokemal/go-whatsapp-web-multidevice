// Package transcribe turns a WhatsApp voice note or audio file into text,
// the audio equivalent of inlining a downloaded image: an MCP client has no
// ears, the same way it has no eyes for a file:// path.
package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.openai.com/v1/audio/transcriptions"

// defaultModel is OpenAI's cheap, current transcription model. Whisper's own
// model ("whisper-1") still works against the same endpoint if this one is
// ever retired.
const defaultModel = "gpt-4o-mini-transcribe"

// ErrNotConfigured is returned when no API key is set, so a caller can
// degrade (e.g. fall back to "media saved, no transcript") rather than
// surface a raw network or auth error for a feature nobody turned on.
var ErrNotConfigured = errors.New("transcription is not configured")

// Transcriber calls OpenAI's transcription API. BaseURL and Client are
// overridable so a test can point this at an httptest.Server instead of the
// real network; left zero, Transcribe fills in the real defaults.
type Transcriber struct {
	APIKey  string
	Model   string
	BaseURL string
	Client  *http.Client
}

// Transcribe uploads audio (its bytes and a filename whose extension names
// the format, e.g. "voice.ogg") and returns the spoken words as plain text.
func (t Transcriber) Transcribe(ctx context.Context, audio []byte, filename string) (string, error) {
	if t.APIKey == "" {
		return "", ErrNotConfigured
	}
	model := t.Model
	if model == "" {
		model = defaultModel
	}
	baseURL := t.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if filename == "" {
		filename = "audio.ogg"
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("building transcription request: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return "", fmt.Errorf("building transcription request: %w", err)
	}
	if err := writer.WriteField("model", model); err != nil {
		return "", fmt.Errorf("building transcription request: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("building transcription request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, &body)
	if err != nil {
		return "", fmt.Errorf("building transcription request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+t.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling transcription API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading transcription response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("transcription API returned %s: %s", resp.Status, truncate(string(respBody), 500))
	}

	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parsing transcription response: %w", err)
	}
	return parsed.Text, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
