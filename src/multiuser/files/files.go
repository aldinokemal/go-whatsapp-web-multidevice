// Package files serves downloaded media to its owner only: GET
// /files/<device_id>/<path>. The device is resolved through the owner
// resolver (another user's device is the same 404 as a missing file), and
// the path can never leave that device's directory under the media root.
package files

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/gofiber/fiber/v3"
)

// Resolver is owners.Resolver: the device, only when the context's user owns it.
type Resolver interface {
	Resolve(ctx context.Context, deviceID string) (*whatsapp.DeviceInstance, string, error)
}

type Deps struct {
	Resolver Resolver
	Root     string // the media root, "statics/media" in production
}

// RoutePrefix is where files are served from.
const RoutePrefix = "/files"

// MediaRoot is the usecase.MediaRootHook for multi-user mode: the device in
// the context downloads into <root>/<device id>, and its files are fetched
// from <baseURL>/files/<device id>. With no device in context it declines.
func MediaRoot(root, baseURL string) func(ctx context.Context) (string, string, bool) {
	baseURL = strings.TrimRight(baseURL, "/")
	return func(ctx context.Context) (string, string, bool) {
		inst, ok := whatsapp.DeviceFromContext(ctx)
		if !ok || inst == nil || !safeSegment(inst.ID()) {
			return "", "", false
		}
		id := inst.ID()
		return filepath.Join(root, id), baseURL + RoutePrefix + "/" + url.PathEscape(id), true
	}
}

// Mount registers GET /files/:device_id/*. Mount it behind the REST API gate
// (session cookie or API key); it answers 401 itself without a user anyway.
func Mount(r fiber.Router, d Deps) {
	r.Get(RoutePrefix+"/:device_id/*", func(c fiber.Ctx) error {
		if _, ok := auth.UserFrom(c.Context()); !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"status": fiber.StatusUnauthorized, "code": "UNAUTHENTICATED", "message": "sign in required",
			})
		}
		path, ok := d.open(c)
		if !ok {
			return notFound(c)
		}
		f, err := os.Open(path)
		if err != nil {
			return notFound(c)
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = f.Close()
			return notFound(c)
		}
		name := filepath.Base(path)
		c.Set(fiber.HeaderContentType, contentType(name))
		c.Set(fiber.HeaderContentDisposition, "attachment; filename*=UTF-8''"+rfc5987(name))
		c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
		c.Set(fiber.HeaderContentSecurityPolicy, "sandbox; default-src 'none'")
		c.Set(fiber.HeaderCacheControl, "private, no-store")
		return c.SendStream(f, int(info.Size())) // fiber closes f once sent
	})
}

func notFound(c fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
		"status": fiber.StatusNotFound, "code": "NOT_FOUND", "message": "file not found",
	})
}

// open returns the resolved on-disk path of the requested file, or false.
func (d Deps) open(c fiber.Ctx) (string, bool) {
	deviceID, err := url.PathUnescape(c.Params("device_id"))
	if err != nil || !safeSegment(deviceID) {
		return "", false
	}
	_, resolvedID, err := d.Resolver.Resolve(c.Context(), deviceID)
	if err != nil || !safeSegment(resolvedID) {
		return "", false
	}
	segs, ok := splitRest(c.Params("*"))
	if !ok {
		return "", false
	}
	deviceDir := filepath.Join(d.Root, resolvedID)
	realDir, err := filepath.EvalSymlinks(deviceDir)
	if err != nil {
		return "", false
	}
	realFile, err := filepath.EvalSymlinks(filepath.Join(append([]string{deviceDir}, segs...)...))
	if err != nil {
		return "", false
	}
	if !within(realDir, realFile) {
		return "", false
	}
	return realFile, true
}

// splitRest percent-decodes the path after the device id, one segment at a
// time, and refuses anything that could name a different place: empty, "."
// or ".." segments (so no leading or doubled slash either), backslashes, NUL
// and other control characters, and segments that decode to contain a slash.
func splitRest(rest string) ([]string, bool) {
	if rest == "" {
		return nil, false
	}
	raw := strings.Split(rest, "/")
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		s, err := url.PathUnescape(r)
		if err != nil || !safeSegment(s) {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func safeSegment(s string) bool {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return false
		}
	}
	return true
}

// within reports whether path is dir itself's descendant (both already
// symlink-resolved).
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var types = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp", ".gif": "image/gif",
	".mp4": "video/mp4", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg", ".mp3": "audio/mpeg",
	".m4a": "audio/mp4", ".pdf": "application/pdf",
}

// contentType is a fixed allowlist by extension; anything else is a plain download.
func contentType(name string) string {
	if t, ok := types[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

// rfc5987 percent-encodes everything but RFC 5987 attr-chars.
func rfc5987(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ('0' <= ch && ch <= '9') || strings.IndexByte("!#$&+-.^_`|~", ch) >= 0 {
			b.WriteByte(ch)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[ch>>4])
		b.WriteByte(hex[ch&15])
	}
	return b.String()
}
