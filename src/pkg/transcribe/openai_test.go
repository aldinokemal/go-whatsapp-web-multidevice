package transcribe

import (
	"context"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranscribeSendsTheAudioAndReturnsTheText(t *testing.T) {
	var gotModel, gotFilename, gotAuth, gotFileContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		require.NoError(t, r.ParseMultipartForm(1<<20))
		gotModel = r.FormValue("model")
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		gotFilename = header.Filename
		buf := make([]byte, 1024)
		n, _ := file.Read(buf)
		gotFileContent = string(buf[:n])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello there, this is a test"}`))
	}))
	defer server.Close()

	tr := Transcriber{APIKey: "sk-test", BaseURL: server.URL}
	text, err := tr.Transcribe(context.Background(), []byte("fake ogg bytes"), "voice.ogg")

	require.NoError(t, err)
	assert.Equal(t, "hello there, this is a test", text)
	assert.Equal(t, "Bearer sk-test", gotAuth)
	assert.Equal(t, defaultModel, gotModel)
	assert.Equal(t, "voice.ogg", gotFilename)
	assert.Equal(t, "fake ogg bytes", gotFileContent)
}

func TestTranscribeWithNoAPIKeyNeverCallsTheNetwork(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	tr := Transcriber{BaseURL: server.URL}
	_, err := tr.Transcribe(context.Background(), []byte("audio"), "voice.ogg")

	require.ErrorIs(t, err, ErrNotConfigured)
	assert.False(t, called, "no API key means no network call, not a failed one")
}

func TestTranscribeSurfacesAnAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	tr := Transcriber{APIKey: "sk-bad", BaseURL: server.URL}
	_, err := tr.Transcribe(context.Background(), []byte("audio"), "voice.ogg")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "invalid api key")
}

func TestTranscribeSurfacesAMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	tr := Transcriber{APIKey: "sk-test", BaseURL: server.URL}
	_, err := tr.Transcribe(context.Background(), []byte("audio"), "voice.ogg")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing transcription response")
}

func TestTranscribeDefaultsAnEmptyFilename(t *testing.T) {
	var gotFilename string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(1<<20))
		_, header, err := r.FormFile("file")
		require.NoError(t, err)
		gotFilename = header.Filename
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer server.Close()

	tr := Transcriber{APIKey: "sk-test", BaseURL: server.URL}
	_, err := tr.Transcribe(context.Background(), []byte("audio"), "")

	require.NoError(t, err)
	assert.Equal(t, "audio.ogg", gotFilename)
}

// TestTranscribeRequestIsValidMultipart guards against a hand-rolled
// boundary/content-type bug: the request must parse as ordinary multipart
// form-data, which is what lets httptest's own r.ParseMultipartForm (used by
// every other test here, and by any real server) read it at all.
func TestTranscribeRequestIsValidMultipart(t *testing.T) {
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer server.Close()

	tr := Transcriber{APIKey: "sk-test", BaseURL: server.URL}
	_, err := tr.Transcribe(context.Background(), []byte("x"), "a.ogg")
	require.NoError(t, err)

	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	assert.Equal(t, "multipart/form-data", mediaType)
	_, hasBoundary := params["boundary"]
	assert.True(t, hasBoundary)
}
