package utils_test

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
)

func TestGetMetaDataFromURLRejectsDecompressedHTMLOverCap(t *testing.T) {
	payload := bytes.Repeat([]byte("<p>"), (utils.MaxPreviewHTMLBytes/3)+8)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := zw.Write(payload)
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

	_, err = utils.GetMetaDataFromURL(server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decoded size")
}

func TestGetMetaDataFromURLAcceptsHTMLUnderCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Short</title></head><body>ok</body></html>`))
	}))
	t.Cleanup(server.Close)

	meta, err := utils.GetMetaDataFromURL(server.URL)
	require.NoError(t, err)
	require.Equal(t, "Short", meta.Title)
}
