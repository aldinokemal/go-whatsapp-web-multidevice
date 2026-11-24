package usecase

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// WTF: MediaRootHook, when set (multi-user mode only), picks the directory a
// download is written under and the URL prefix it is fetched from, so each
// device's media lives in its own directory and is served owner-checked.
// ok=false (or a nil hook) keeps upstream's config.PathMedia and no URL.
var MediaRootHook func(ctx context.Context) (dir string, urlPrefix string, ok bool)

// mediaRoot returns the base directory for downloads and the URL prefix ("" upstream).
func mediaRoot(ctx context.Context) (string, string) {
	if MediaRootHook != nil {
		if dir, prefix, ok := MediaRootHook(ctx); ok && dir != "" {
			return dir, prefix
		}
	}
	return config.PathMedia, ""
}

// mediaFileURL is prefix + the path of file relative to base, slash-separated
// with each segment escaped, or "" when there is no prefix or file is not
// under base.
func mediaFileURL(base, prefix, file string) string {
	if prefix == "" {
		return ""
	}
	rel, err := filepath.Rel(base, file)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.Join(parts, "/")
}
