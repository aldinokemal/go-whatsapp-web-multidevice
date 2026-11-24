package files

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type fakeDM map[string]*whatsapp.DeviceInstance

func (f fakeDM) GetDevice(id string) (*whatsapp.DeviceInstance, bool) { d, ok := f[id]; return d, ok }

type env struct {
	app        *fiber.App
	root       string
	aliceKey   string
	bobKey     string
	aliceID    string
	bobID      string
	aliceDev   string
	bobDev     string
	aliceStore *store.Store
}

// newEnv mounts /files behind the same gates the runtime puts in front of
// the REST API, with a real store and owner resolver. Alice owns d_alice,
// Bob owns d_bob, and each device directory holds a photo.
func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	alice, _ := st.CreateUser(ctx, "+27825550101")
	bob, _ := st.CreateUser(ctx, "+27825550102")
	require.NoError(t, st.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, st.SetOwner(ctx, "d_bob", bob.ID))
	aKey, _, err := st.CreateAPIKey(ctx, alice.ID, "a")
	require.NoError(t, err)
	bKey, _, err := st.CreateAPIKey(ctx, bob.ID, "b")
	require.NoError(t, err)
	dm := fakeDM{"d_alice": whatsapp.NewDeviceInstance("d_alice", nil, nil), "d_bob": whatsapp.NewDeviceInstance("d_bob", nil, nil)}

	root := t.TempDir()
	for _, p := range []string{"d_alice/2782/2026-11-23/a.jpg", "d_bob/2782/2026-11-23/x.jpg", "d_bob/secret.pdf"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte("content of "+p), 0o644))
	}
	outside := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-outside.txt")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
	t.Cleanup(func() { _ = os.Remove(outside) })
	// Symlinks inside Alice's directory pointing out of it.
	require.NoError(t, os.Symlink(filepath.Join(root, "d_bob", "secret.pdf"), filepath.Join(root, "d_alice", "link.pdf")))
	require.NoError(t, os.Symlink(filepath.Join(root, "d_bob"), filepath.Join(root, "d_alice", "bobdir")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "d_alice", "out.txt")))

	m := auth.New(auth.Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour, Secure: true}, st)
	app := fiber.New()
	app.Use(m.Middleware())
	app.Use(m.APIKey(), m.RequireAPI(), m.RequireSameOriginAPI(), m.VerifyCSRF())
	Mount(app, Deps{Resolver: owners.NewResolver(dm, st), Root: root})
	return &env{app: app, root: root, aliceKey: aKey, bobKey: bKey, aliceID: alice.ID, bobID: bob.ID, aliceDev: "d_alice", bobDev: "d_bob", aliceStore: st}
}

func (e *env) get(t *testing.T, key, rawPath string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, rawPath, nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := e.app.Test(req)
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestOwnFileIsServed(t *testing.T) {
	e := newEnv(t)
	resp, body := e.get(t, e.aliceKey, "/files/d_alice/2782/2026-11-23/a.jpg")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "content of d_alice/2782/2026-11-23/a.jpg", body)
}

func TestAnonymousIs401(t *testing.T) {
	e := newEnv(t)
	resp, _ := e.get(t, "", "/files/d_alice/2782/2026-11-23/a.jpg")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// Security: files are downloads that can never run as a page on our origin.
func TestHeadersMakeItADownload(t *testing.T) {
	e := newEnv(t)
	resp, _ := e.get(t, e.aliceKey, "/files/d_alice/2782/2026-11-23/a.jpg")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))
	require.Equal(t, `attachment; filename*=UTF-8''a.jpg`, resp.Header.Get("Content-Disposition"))
	require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
	require.Equal(t, "sandbox; default-src 'none'", resp.Header.Get("Content-Security-Policy"))
	require.Equal(t, "private, no-store", resp.Header.Get("Cache-Control"))
}

func TestUnknownTypesAreOctetStream(t *testing.T) {
	e := newEnv(t)
	html := filepath.Join(e.root, "d_alice", "page.html")
	require.NoError(t, os.WriteFile(html, []byte("<script>alert(1)</script>"), 0o644))
	resp, _ := e.get(t, e.aliceKey, "/files/d_alice/page.html")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
}

// Security: no spelling of a path escapes the owner's device directory, and
// every refusal is the same 404 as a file that does not exist.
func TestTraversalIs404(t *testing.T) {
	e := newEnv(t)
	_, missing := e.get(t, e.aliceKey, "/files/d_alice/no-such-file.jpg")
	for _, p := range []string{
		"/files/d_alice/no-such-file.jpg",
		"/files/d_alice/../d_bob/secret.pdf",
		"/files/d_alice/%2e%2e/d_bob/secret.pdf",
		"/files/d_alice/%2E%2E/d_bob/secret.pdf",
		"/files/d_alice/..%2fd_bob%2fsecret.pdf",
		"/files/d_alice/..%5cd_bob%5csecret.pdf",
		"/files/d_alice/%2fetc%2fpasswd",
		"/files/d_alice//etc/passwd",
		"/files/d_alice/a%5C..%5Cb",
		"/files/d_alice/x%00.jpg",
		"/files/d_alice/./2782/2026-11-23/a.jpg",
		"/files/d_alice/link.pdf",          // symlink to a file in Bob's directory
		"/files/d_alice/bobdir/secret.pdf", // symlinked directory into Bob's
		"/files/d_alice/out.txt",           // symlink out of the media root
		"/files/d_alice/2782",              // a directory, not a file
		"/files/d_alice/",                  // the device directory itself
		"/files/d_alice/%252e%252e/d_bob/secret.pdf",
	} {
		resp, body := e.get(t, e.aliceKey, p)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, p)
		require.Equal(t, missing, body, "%s: same answer as a missing file", p)
		require.NotContains(t, body, "content of", p)
	}
}

// Security: another user's device is a 404 that looks exactly like a
// missing file, for a file that does exist.
func TestAnotherUsersDeviceIs404(t *testing.T) {
	e := newEnv(t)
	_, missing := e.get(t, e.aliceKey, "/files/d_alice/no-such-file.jpg")
	resp, body := e.get(t, e.aliceKey, "/files/d_bob/secret.pdf")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, missing, body)

	resp, _ = e.get(t, e.bobKey, "/files/d_bob/secret.pdf")
	require.Equal(t, http.StatusOK, resp.StatusCode, "Bob can fetch his own")

	resp, _ = e.get(t, e.aliceKey, "/files/d_nobody/secret.pdf")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Behaviour: the hook the usecase calls puts a device's downloads under its
// own directory and names the matching /files URL.
func TestMediaRootFollowsTheDeviceInContext(t *testing.T) {
	hook := MediaRoot("statics/media", "https://wtf.example.com")
	_, _, ok := hook(context.Background())
	require.False(t, ok, "no device in context: decline")

	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("d_alice", nil, nil))
	dir, prefix, ok := hook(ctx)
	require.True(t, ok)
	require.Equal(t, filepath.Join("statics", "media", "d_alice"), dir)
	require.Equal(t, "https://wtf.example.com/files/d_alice", prefix)
}
