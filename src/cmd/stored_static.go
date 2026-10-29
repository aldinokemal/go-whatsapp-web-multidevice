package cmd

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v3"
)

// storedStaticRoot is the directory published at {AppBasePath}/statics.
// Tests point it at a temporary directory.
var storedStaticRoot = "statics"

// registerStoredStatic serves files under /statics. The embedded dashboard
// HTML is registered separately at "/" and is not read from this tree.
// User-supplied message documents are attachments with a non-active content
// type. JPEG, PNG, GIF, and WebP are inline only on the image paths the
// dashboard and QR screen use, and only when the bytes sniff as that image.
func registerStoredStatic(app *fiber.App) {
	prefix := storedStaticPrefix()
	app.Use(prefix, serveStoredStatic)
}

func storedStaticPrefix() string {
	base := strings.TrimRight(strings.TrimSpace(config.AppBasePath), "/")
	return base + "/statics"
}

func serveStoredStatic(c fiber.Ctx) error {
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
		return c.SendStatus(fiber.StatusMethodNotAllowed)
	}
	rel, ok := storedStaticRelative(c.Path())
	if !ok {
		return c.SendStatus(fiber.StatusNotFound)
	}
	root, err := filepath.Abs(storedStaticRoot)
	if err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	full, err = filepath.Abs(full)
	if err != nil || (full != root && !strings.HasPrefix(full, root+string(os.PathSeparator))) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return c.SendStatus(fiber.StatusNotFound)
	}
	c.Set(fiber.HeaderLastModified, info.ModTime().UTC().Format(http.TimeFormat))
	if c.Fresh() {
		return c.SendStatus(fiber.StatusNotModified)
	}
	file, err := os.Open(full)
	if err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}

	header := make([]byte, 512)
	n, readErr := io.ReadFull(file, header)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		_ = file.Close()
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	if imageType, inline := inlineImageType(rel, header[:n]); inline {
		c.Set(fiber.HeaderContentType, imageType)
		c.Set(fiber.HeaderContentDisposition, "inline")
	} else {
		// Never let a stored document execute as this origin. HTML, SVG,
		// scripts, and unrecognized bytes are downloads.
		c.Set(fiber.HeaderContentType, "application/octet-stream")
		c.Set(fiber.HeaderContentDisposition, attachmentDisposition(info.Name()))
	}
	// fasthttp closes the stream after writing the response. A HEAD response
	// keeps the Content-Length and skips the body.
	return c.SendStream(file, int(info.Size()))
}

func storedStaticRelative(requestPath string) (string, bool) {
	prefix := storedStaticPrefix()
	if requestPath != prefix && !strings.HasPrefix(requestPath, prefix+"/") {
		return "", false
	}
	rel := strings.TrimPrefix(requestPath, prefix)
	rel = strings.TrimPrefix(rel, "/")
	// c.Path() is the raw request path, and stored file URLs escape each
	// segment. Decode first so the traversal and backslash checks below see
	// the real name.
	rel, err := url.PathUnescape(rel)
	if err != nil || strings.ContainsRune(rel, 0) {
		return "", false
	}
	if rel == "" || rel == "." {
		return "", false
	}
	cleaned := path.Clean("/" + rel)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "\\") {
		return "", false
	}
	return cleaned, true
}

func inlineImageType(rel string, sniffed []byte) (string, bool) {
	if !isImagePreviewPath(rel) {
		return "", false
	}
	mediaType := sniffedMediaType(sniffed)
	switch mediaType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return mediaType, true
	default:
		return "", false
	}
}

func isImagePreviewPath(rel string) bool {
	rel = strings.ToLower(strings.Trim(rel, "/"))
	return rel == "qrcode" || strings.HasPrefix(rel, "qrcode/") ||
		strings.HasPrefix(rel, "media/") ||
		strings.HasPrefix(rel, "senditems/")
}

func sniffedMediaType(sniffed []byte) string {
	detected := http.DetectContentType(sniffed)
	mediaType, _, err := mime.ParseMediaType(detected)
	if err != nil {
		return ""
	}
	return strings.ToLower(mediaType)
}

func attachmentDisposition(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == '/' {
			return -1
		}
		return r
	}, name)
	if cleaned == "" {
		cleaned = "download"
	}
	disposition := `attachment; filename="` + cleaned + `"`
	if strings.ContainsFunc(cleaned, func(r rune) bool { return r > unicode.MaxASCII }) {
		disposition += `; filename*=UTF-8''` + url.PathEscape(cleaned)
	}
	return disposition
}
