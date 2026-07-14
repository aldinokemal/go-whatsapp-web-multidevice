// Package uiasset downloads the gowa-ui dashboard (a single self-contained
// HTML file published as a GitHub release asset) and serves it from an
// in-memory copy backed by a disk cache. Network failures are never fatal:
// the manager keeps serving the cached copy, or callers fall back to
// FallbackHTML until a download succeeds.
package uiasset

import (
	"context"
	"math/rand/v2"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

type Config struct {
	// Repo is the "owner/name" GitHub repository to download from.
	Repo string
	// AssetName is the exact release asset filename to match.
	AssetName string
	// CacheDir is a writable directory holding index.html + meta.json.
	CacheDir string
	// GithubToken optionally authenticates GitHub API calls (rate limits).
	GithubToken string
	// Interval between update checks; jittered by ±10%.
	Interval time.Duration
	// PinnedSHA256 optionally pins the exact dashboard build (hex sha256).
	// The release digest alone proves the download matches what GitHub
	// advertises, not that the publisher is trustworthy; the pin is an
	// operator-controlled trust anchor independent of the release metadata.
	PinnedSHA256 string
}

type cachedAsset struct {
	html   []byte
	sha256 string
	tag    string
	etag   string // GitHub API ETag for conditional release lookups
}

type Manager struct {
	cfg     Config
	current atomic.Pointer[cachedAsset]
	client  *http.Client
}

func New(cfg Config) *Manager {
	return &Manager{
		cfg: cfg,
		// Default transport honors HTTP(S)_PROXY from the environment.
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Content returns the dashboard HTML and its sha256 (usable as an ETag).
func (m *Manager) Content() (html []byte, etag string, ok bool) {
	asset := m.current.Load()
	if asset == nil || len(asset.html) == 0 {
		return nil, "", false
	}
	return asset.html, asset.sha256, true
}

// StartAutoUpdate periodically refreshes the dashboard until ctx is canceled.
func (m *Manager) StartAutoUpdate(ctx context.Context) {
	interval := m.cfg.Interval
	if interval <= 0 {
		interval = 3 * time.Hour
	}
	for {
		jitter := time.Duration((rand.Float64()*0.2 - 0.1) * float64(interval))
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval + jitter):
			if err := m.EnsureLatest(ctx); err != nil {
				logrus.Warnf("[UI_ASSET] update check failed: %v", err)
			}
		}
	}
}
