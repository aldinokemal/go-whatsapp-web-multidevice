package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"time"
)

// Sender sends WhatsApp messages through GOWA.
type Sender interface {
	SendText(ctx context.Context, phone, text string) (messageID string, err error)
	SendImage(ctx context.Context, phone, caption, path, contentType string) (messageID string, err error)
	Typing(ctx context.Context, phone string, on bool)
}

// Agent asks the AI for a reply within a per-chat session.
type Agent interface {
	Reply(ctx context.Context, sessionID, systemPrompt, userText string) (string, error)
}

// ---- GOWA ---------------------------------------------------------------------

type GowaClient struct {
	baseURL, user, pass, deviceID string
	http                          *http.Client
}

func NewGowaClient(c Config) *GowaClient {
	return &GowaClient{baseURL: c.GowaURL, user: c.GowaUser, pass: c.GowaPass, deviceID: c.GowaDeviceID,
		http: &http.Client{Timeout: 30 * time.Second}}
}

func (g *GowaClient) post(ctx context.Context, path string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return g.do(ctx, path, "application/json", bytes.NewReader(b))
}

func (g *GowaClient) do(ctx context.Context, path, contentType string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(g.user, g.pass)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Device-Id", g.deviceID)
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("gowa %s: HTTP %d: %s", path, resp.StatusCode, truncate(string(out), 300))
	}
	return out, nil
}

func (g *GowaClient) SendText(ctx context.Context, phone, text string) (string, error) {
	out, err := g.post(ctx, "/send/message", map[string]string{"phone": phone, "message": text})
	if err != nil {
		return "", err
	}
	return messageID(out), nil
}

func messageID(out []byte) string {
	var r struct {
		Results struct {
			MessageID string `json:"message_id"`
		} `json:"results"`
	}
	_ = json.Unmarshal(out, &r)
	return r.Results.MessageID
}

// SendImage uploads a file to GOWA's /send/image as multipart form data.
func (g *GowaClient) SendImage(ctx context.Context, phone, caption, path, contentType string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("phone", phone)
	if caption != "" {
		_ = mw.WriteField("caption", caption)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename=%q`, filepath.Base(path)))
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	out, err := g.do(ctx, "/send/image", mw.FormDataContentType(), &buf)
	if err != nil {
		return "", err
	}
	return messageID(out), nil
}

func (g *GowaClient) Typing(ctx context.Context, phone string, on bool) {
	action := "stop"
	if on {
		action = "start"
	}
	_, _ = g.post(ctx, "/send/chat-presence", map[string]string{"phone": phone, "action": action})
}

// ---- Hermes -------------------------------------------------------------------

type HermesClient struct {
	baseURL, apiKey, model string
	http                   *http.Client
}

func NewHermesClient(c Config) *HermesClient {
	return &HermesClient{baseURL: c.HermesURL, apiKey: c.HermesAPIKey, model: c.HermesModel,
		http: &http.Client{Timeout: c.HermesTimeout}}
}

// Reply calls the OpenAI-compatible Chat Completions endpoint of the Hermes
// API server. X-Hermes-Session-Id makes Hermes load and extend this chat's own
// history, so only the newest user text is sent.
func (h *HermesClient) Reply(ctx context.Context, sessionID, systemPrompt, userText string) (string, error) {
	body := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userText},
		},
		"stream": false,
	}
	if h.model != "" {
		body["model"] = h.model
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hermes-Session-Id", sessionID)

	resp, err := h.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hermes: HTTP %d: %s", resp.StatusCode, truncate(string(out), 300))
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return "", fmt.Errorf("hermes: invalid JSON: %w", err)
	}
	if len(r.Choices) == 0 {
		return "", errors.New("hermes: empty choices")
	}
	ch := r.Choices[0]
	switch ch.FinishReason {
	case "error", "cancelled":
		return "", fmt.Errorf("hermes: turn ended with finish_reason=%s", ch.FinishReason)
	}
	return ch.Message.Content, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
