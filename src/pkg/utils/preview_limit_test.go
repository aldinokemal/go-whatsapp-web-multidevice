package utils_test

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
)

func TestGetMetaDataFromURLParsesCappedPrefixOfDecompressedHTML(t *testing.T) {
	t.Cleanup(utils.AllowUntrustedLoopbackForTest())
	payload := []byte(`<html><head><title>Long page</title></head><body><!--` +
		strings.Repeat("x", 32*utils.MaxPreviewHTMLBytes) +
		`--><meta name="description" content="past the cap"></body></html>`)
	var compressed bytes.Buffer
	zw, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	require.NoError(t, err)
	_, err = zw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.Less(t, compressed.Len(), utils.MaxPreviewHTMLBytes, "fixture must be small on the wire")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			http.Error(w, "client did not ask for gzip", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	t.Cleanup(server.Close)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	meta, err := utils.GetMetaDataFromURL(server.URL)
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	require.Equal(t, "Long page", meta.Title)
	require.Empty(t, meta.Description, "bytes past the cap must not be parsed")
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(len(payload)/2), "the decoded body must not be buffered")
}

func TestGetMetaDataFromURLAcceptsHTMLUnderCap(t *testing.T) {
	t.Cleanup(utils.AllowUntrustedLoopbackForTest())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Short</title></head><body>ok</body></html>`))
	}))
	t.Cleanup(server.Close)

	meta, err := utils.GetMetaDataFromURL(server.URL)
	require.NoError(t, err)
	require.Equal(t, "Short", meta.Title)
}
