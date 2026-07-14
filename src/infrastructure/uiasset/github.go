package uiasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

// githubAPIBase is a variable so tests can point it at a local server.
var githubAPIBase = "https://api.github.com"

// maxAssetBytes bounds the release download so a hostile or misconfigured
// release cannot exhaust memory: the dashboard is a single HTML file (~1 MB),
// so 64 MiB is generous headroom. A variable so tests can lower it.
var maxAssetBytes int64 = 64 << 20

type releaseAsset struct {
	Name               string `json:"name"`
	Digest             string `json:"digest"` // "sha256:<hex>", may be empty
	BrowserDownloadURL string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

// EnsureLatest checks the newest GitHub release and downloads the dashboard
// asset when the cached copy is missing or outdated. It is safe to call
// concurrently with Content and never leaves a partially written cache.
func (m *Manager) EnsureLatest(ctx context.Context) error {
	current := m.current.Load()

	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, m.cfg.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if m.cfg.GithubToken != "" {
		req.Header.Set("Authorization", "Bearer "+m.cfg.GithubToken)
	}
	if current != nil && current.etag != "" {
		req.Header.Set("If-None-Match", current.etag)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("release lookup returned %s", resp.Status)
	}

	var rel release
	if err = json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}

	var asset *releaseAsset
	for i := range rel.Assets {
		if rel.Assets[i].Name == m.cfg.AssetName {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		return fmt.Errorf("release %s has no asset named %s", rel.TagName, m.cfg.AssetName)
	}

	etag := resp.Header.Get("ETag")
	remoteSHA := strings.TrimPrefix(asset.Digest, "sha256:")

	if m.cfg.PinnedSHA256 != "" && remoteSHA != "" && !strings.EqualFold(remoteSHA, m.cfg.PinnedSHA256) {
		return fmt.Errorf("release %s asset digest %s does not match the pinned sha256 %s; refusing to download",
			rel.TagName, remoteSHA, m.cfg.PinnedSHA256)
	}

	if current != nil && remoteSHA != "" && remoteSHA == current.sha256 {
		// Same content; just remember the fresher ETag/tag for future checks.
		m.current.Store(&cachedAsset{html: current.html, sha256: current.sha256, tag: rel.TagName, etag: etag})
		m.persistMeta(rel.TagName, current.sha256, etag)
		return nil
	}

	html, downloadedSHA, err := m.download(ctx, asset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	if remoteSHA != "" && downloadedSHA != remoteSHA {
		return fmt.Errorf("digest mismatch: release says %s, downloaded %s", remoteSHA, downloadedSHA)
	}
	if m.cfg.PinnedSHA256 != "" && !strings.EqualFold(downloadedSHA, m.cfg.PinnedSHA256) {
		return fmt.Errorf("downloaded asset sha256 %s does not match the pinned sha256 %s; refusing to serve",
			downloadedSHA, m.cfg.PinnedSHA256)
	}

	if err = m.persist(html, rel.TagName, downloadedSHA, etag); err != nil {
		logrus.Warnf("[UI_ASSET] cache write failed (serving from memory): %v", err)
	}
	m.current.Store(&cachedAsset{html: html, sha256: downloadedSHA, tag: rel.TagName, etag: etag})
	logrus.Infof("[UI_ASSET] dashboard updated to %s (%d bytes)", rel.TagName, len(html))
	return nil
}

func (m *Manager) download(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if m.cfg.GithubToken != "" {
		req.Header.Set("Authorization", "Bearer "+m.cfg.GithubToken)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("asset download returned %s", resp.Status)
	}

	hasher := sha256.New()
	// Read one byte past the limit so truncation is detected as an explicit
	// error instead of a silently short (and wrongly hashed) asset.
	body, err := io.ReadAll(io.LimitReader(io.TeeReader(resp.Body, hasher), maxAssetBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxAssetBytes {
		return nil, "", fmt.Errorf("asset exceeds the %d MiB download limit; refusing", maxAssetBytes>>20)
	}
	return body, hex.EncodeToString(hasher.Sum(nil)), nil
}
