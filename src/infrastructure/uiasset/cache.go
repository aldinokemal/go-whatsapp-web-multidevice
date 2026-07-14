package uiasset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

func contentSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

const (
	cacheFileName = "index.html"
	metaFileName  = "meta.json"
)

type cacheMeta struct {
	Tag          string    `json:"tag"`
	AssetName    string    `json:"asset_name"`
	SHA256       string    `json:"sha256"`
	ETag         string    `json:"etag"`
	DownloadedAt time.Time `json:"downloaded_at"`
}

// LoadCache loads a previously downloaded dashboard from disk, if present.
// Air-gapped deployments can pre-seed CacheDir with index.html (+ optional
// meta.json) and disable auto-update.
func (m *Manager) LoadCache() error {
	html, err := os.ReadFile(filepath.Join(m.cfg.CacheDir, cacheFileName))
	if err != nil {
		return err
	}

	asset := cachedAsset{html: html, sha256: contentSHA(html)}
	if metaRaw, metaErr := os.ReadFile(filepath.Join(m.cfg.CacheDir, metaFileName)); metaErr == nil {
		var meta cacheMeta
		if json.Unmarshal(metaRaw, &meta) == nil && meta.AssetName == m.cfg.AssetName {
			asset.tag = meta.Tag
			asset.etag = meta.ETag
		}
	}

	if m.cfg.PinnedSHA256 != "" && !strings.EqualFold(asset.sha256, m.cfg.PinnedSHA256) {
		return fmt.Errorf("cached dashboard sha256 %s does not match the pinned sha256 %s; refusing to serve",
			asset.sha256, m.cfg.PinnedSHA256)
	}

	m.current.Store(&asset)
	return nil
}

func (m *Manager) persist(html []byte, tag, sha, etag string) error {
	if err := os.MkdirAll(m.cfg.CacheDir, 0o755); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(m.cfg.CacheDir, cacheFileName), html); err != nil {
		return err
	}
	m.persistMeta(tag, sha, etag)
	return nil
}

func (m *Manager) persistMeta(tag, sha, etag string) {
	meta := cacheMeta{
		Tag:          tag,
		AssetName:    m.cfg.AssetName,
		SHA256:       sha,
		ETag:         etag,
		DownloadedAt: time.Now().UTC(),
	}
	if raw, err := json.MarshalIndent(meta, "", "  "); err == nil {
		if writeErr := atomicWrite(filepath.Join(m.cfg.CacheDir, metaFileName), raw); writeErr != nil {
			logrus.Warnf("[UI_ASSET] cache metadata write failed (ETag/tag lost on restart): %v", writeErr)
		}
	}
}

func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
