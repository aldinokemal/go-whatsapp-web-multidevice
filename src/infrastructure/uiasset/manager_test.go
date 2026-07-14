package uiasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeGithub struct {
	tag        string
	asset      []byte
	digest     bool // include the sha256 digest field
	etag       string
	lookups    int
	notModked  int
	downloads  int
	badDigest  bool
	server     *httptest.Server
	assetRoute string
}

func newFakeGithub(t *testing.T, tag string, asset []byte) *fakeGithub {
	t.Helper()
	fake := &fakeGithub{tag: tag, asset: asset, digest: true, etag: `W/"rel-1"`, assetRoute: "/dl/gowa-ui.html"}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/aldinokemal/gowa-ui/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fake.lookups++
		if r.Header.Get("If-None-Match") == fake.etag {
			fake.notModked++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		digest := ""
		if fake.digest {
			sum := sha256.Sum256(fake.asset)
			hexSum := hex.EncodeToString(sum[:])
			if fake.badDigest {
				hexSum = "deadbeef" + hexSum[8:]
			}
			digest = `"digest":"sha256:` + hexSum + `",`
		}
		w.Header().Set("ETag", fake.etag)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[
			{"name":"other.txt","browser_download_url":"%s/dl/other.txt"},
			{"name":"gowa-ui.html",%s"browser_download_url":"%s%s"}
		]}`, fake.tag, fake.server.URL, digest, fake.server.URL, fake.assetRoute)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, _ *http.Request) {
		fake.downloads++
		_, _ = w.Write(fake.asset)
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func newTestManager(t *testing.T, fake *fakeGithub) *Manager {
	t.Helper()
	previous := githubAPIBase
	githubAPIBase = fake.server.URL
	t.Cleanup(func() { githubAPIBase = previous })

	return New(Config{
		Repo:      "aldinokemal/gowa-ui",
		AssetName: "gowa-ui.html",
		CacheDir:  filepath.Join(t.TempDir(), "ui"),
	})
}

func TestEnsureLatestDownloadsAndCaches(t *testing.T) {
	html := []byte("<html>v1</html>")
	fake := newFakeGithub(t, "v1.0.0", html)
	manager := newTestManager(t, fake)

	_, _, ok := manager.Content()
	assert.False(t, ok)

	require.NoError(t, manager.EnsureLatest(context.Background()))

	served, etag, ok := manager.Content()
	require.True(t, ok)
	assert.Equal(t, html, served)
	assert.Equal(t, contentSHA(html), etag)
	assert.Equal(t, 1, fake.downloads)

	cached, err := os.ReadFile(filepath.Join(manager.cfg.CacheDir, "index.html"))
	require.NoError(t, err)
	assert.Equal(t, html, cached)
}

func TestEnsureLatestSkipsWhenNotModified(t *testing.T) {
	fake := newFakeGithub(t, "v1.0.0", []byte("<html>v1</html>"))
	manager := newTestManager(t, fake)

	require.NoError(t, manager.EnsureLatest(context.Background()))
	require.NoError(t, manager.EnsureLatest(context.Background()))

	assert.Equal(t, 1, fake.downloads, "304 must not re-download")
	assert.Equal(t, 1, fake.notModked)
}

func TestEnsureLatestSkipsWhenDigestMatches(t *testing.T) {
	html := []byte("<html>v1</html>")
	fake := newFakeGithub(t, "v1.0.0", html)
	manager := newTestManager(t, fake)

	require.NoError(t, manager.EnsureLatest(context.Background()))

	// New release ETag but identical asset digest: lookup happens, no download.
	fake.etag = `W/"rel-2"`
	fake.tag = "v1.0.1"
	require.NoError(t, manager.EnsureLatest(context.Background()))

	assert.Equal(t, 1, fake.downloads)
	served, _, ok := manager.Content()
	require.True(t, ok)
	assert.Equal(t, html, served)
}

func TestEnsureLatestRejectsDigestMismatch(t *testing.T) {
	fake := newFakeGithub(t, "v1.0.0", []byte("<html>tampered</html>"))
	fake.badDigest = true
	manager := newTestManager(t, fake)

	err := manager.EnsureLatest(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest mismatch")

	_, _, ok := manager.Content()
	assert.False(t, ok, "tampered asset must not be served")
}

func TestEnsureLatestUpgradesToNewRelease(t *testing.T) {
	fake := newFakeGithub(t, "v1.0.0", []byte("<html>v1</html>"))
	manager := newTestManager(t, fake)
	require.NoError(t, manager.EnsureLatest(context.Background()))

	next := []byte("<html>v2</html>")
	fake.asset = next
	fake.tag = "v2.0.0"
	fake.etag = `W/"rel-2"`
	require.NoError(t, manager.EnsureLatest(context.Background()))

	served, _, ok := manager.Content()
	require.True(t, ok)
	assert.Equal(t, next, served)
	assert.Equal(t, 2, fake.downloads)
}

func TestLoadCacheServesPreseededFile(t *testing.T) {
	manager := New(Config{
		Repo:      "aldinokemal/gowa-ui",
		AssetName: "gowa-ui.html",
		CacheDir:  t.TempDir(),
	})
	html := []byte("<html>air-gapped</html>")
	require.NoError(t, os.WriteFile(filepath.Join(manager.cfg.CacheDir, "index.html"), html, 0o644))

	require.NoError(t, manager.LoadCache())

	served, etag, ok := manager.Content()
	require.True(t, ok)
	assert.Equal(t, html, served)
	assert.Equal(t, contentSHA(html), etag)
}

func TestLoadCacheMissingIsAnError(t *testing.T) {
	manager := New(Config{CacheDir: t.TempDir(), AssetName: "gowa-ui.html"})
	assert.Error(t, manager.LoadCache())
}

func TestPinnedSHARejectsMismatchedRelease(t *testing.T) {
	fake := newFakeGithub(t, "v1.0.0", []byte("<html>unaudited build</html>"))
	manager := newTestManager(t, fake)
	manager.cfg.PinnedSHA256 = contentSHA([]byte("the build the operator audited"))

	err := manager.EnsureLatest(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pinned sha256")
	assert.Equal(t, 0, fake.downloads, "mismatched digest must be rejected before downloading")

	_, _, ok := manager.Content()
	assert.False(t, ok)
}

func TestPinnedSHAAcceptsMatchingRelease(t *testing.T) {
	html := []byte("<html>audited build</html>")
	fake := newFakeGithub(t, "v1.0.0", html)
	manager := newTestManager(t, fake)
	manager.cfg.PinnedSHA256 = contentSHA(html)

	require.NoError(t, manager.EnsureLatest(context.Background()))

	served, _, ok := manager.Content()
	require.True(t, ok)
	assert.Equal(t, html, served)
}

func TestPinnedSHARejectsTamperedCache(t *testing.T) {
	manager := New(Config{
		Repo:         "aldinokemal/gowa-ui",
		AssetName:    "gowa-ui.html",
		CacheDir:     t.TempDir(),
		PinnedSHA256: contentSHA([]byte("expected build")),
	})
	require.NoError(t, os.WriteFile(
		filepath.Join(manager.cfg.CacheDir, "index.html"), []byte("<html>tampered</html>"), 0o644))

	err := manager.LoadCache()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pinned sha256")

	_, _, ok := manager.Content()
	assert.False(t, ok)
}

func TestEnsureLatestRejectsOversizedAsset(t *testing.T) {
	previous := maxAssetBytes
	maxAssetBytes = 16
	t.Cleanup(func() { maxAssetBytes = previous })

	fake := newFakeGithub(t, "v1.0.0", []byte("<html>this asset is larger than the limit</html>"))
	manager := newTestManager(t, fake)

	err := manager.EnsureLatest(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "download limit")

	_, _, ok := manager.Content()
	assert.False(t, ok, "oversized asset must not be served")
	_, statErr := os.Stat(filepath.Join(manager.cfg.CacheDir, "index.html"))
	assert.True(t, os.IsNotExist(statErr), "oversized asset must not be cached")
}

func TestFallbackHTMLMentionsRepoAndVersion(t *testing.T) {
	page := string(FallbackHTML("v9.0.0", "aldinokemal/gowa-ui"))
	assert.Contains(t, page, "v9.0.0")
	assert.Contains(t, page, "aldinokemal/gowa-ui")
}
