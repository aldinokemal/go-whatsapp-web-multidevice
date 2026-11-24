package usecase

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
)

// WTF (phase 2 task 4): where downloaded media lands, and the URL it is
// fetched from, follow MediaRootHook when set.

// Behaviour: upstream (hook nil) keeps config.PathMedia and sets no URL, so
// the REST layer's own /statics URL is used as before.
func TestMediaRootHookIsNilUpstream(t *testing.T) {
	require.Nil(t, MediaRootHook)
	base, prefix := mediaRoot(context.Background())
	require.Equal(t, config.PathMedia, base)
	require.Empty(t, prefix)
	require.Empty(t, mediaFileURL(base, prefix, filepath.Join(base, "2782", "2026-11-23", "a.jpg")))
}

// Behaviour: with the hook, downloads land under the device's directory and
// the URL names the same relative path, each segment escaped.
func TestDownloadWritesUnderTheDeviceDir(t *testing.T) {
	t.Cleanup(func() { MediaRootHook = nil })
	MediaRootHook = func(context.Context) (string, string, bool) {
		return filepath.Join("statics", "media", "d_alice"), "/files/d_alice", true
	}
	base, prefix := mediaRoot(context.Background())
	require.Equal(t, filepath.Join("statics", "media", "d_alice"), base)

	path := filepath.Join(base, "27825550101", "2026-11-23", "photo 1#.jpg")
	require.Equal(t, "/files/d_alice/27825550101/2026-11-23/photo%201%23.jpg", mediaFileURL(base, prefix, path))

	// A path that is somehow not under base never yields a URL.
	require.Empty(t, mediaFileURL(base, prefix, filepath.Join("statics", "media", "d_bob", "x.jpg")))

	// The hook may decline (no device in context): upstream's root, no URL.
	MediaRootHook = func(context.Context) (string, string, bool) { return "", "", false }
	base, prefix = mediaRoot(context.Background())
	require.Equal(t, config.PathMedia, base)
	require.Empty(t, prefix)
}
