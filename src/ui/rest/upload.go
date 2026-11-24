package rest

import (
	"os"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/upload"
	"github.com/gofiber/fiber/v3"
)

// InitRestUpload registers the pre-signed-style upload PUT/GET pair.
//
// These are deliberately registered OUTSIDE every auth gate (see cmd/rest.go,
// where this is called before config.MultiUserEnabled's setupMultiUser runs):
// the ticket id itself, 160 random bits, minted only through an already
// authenticated whatsapp_app "create_upload" MCP call, is the one credential,
// the same shape as an S3 presigned URL. They exist so an MCP client holding
// only local bytes - a screenshot it just took, with no URL to host it at -
// can still reach image_url, which only ever downloads from a URL.
func InitRestUpload(app fiber.Router, store *upload.Store) {
	app.Put("/uploads/:id", func(c fiber.Ctx) error {
		body := c.Body()
		if int64(len(body)) > config.WhatsappSettingMaxImageSize {
			return uploadError(c, upload.ErrTooLarge)
		}
		if err := store.Upload(c.Params("id"), body); err != nil {
			return uploadError(c, err)
		}
		return c.JSON(fiber.Map{"status": "uploaded"})
	})

	// The trailing segment is wildcarded and never inspected - only :id is
	// looked up. It exists because WTF's own DownloadImageFromURL (which
	// image_url ultimately goes through) derives an extension from the
	// URL's own last path segment and refuses anything not jpg/png/webp,
	// regardless of the real Content-Type header; create_upload's returned
	// fetch_path always names a harmless ".jpg" there for exactly this
	// reason, since the real format isn't known until after the upload.
	// Accepting (and ignoring) whatever name shows up here means a caller
	// who, say, appends the real extension themselves still works too.
	app.Get("/uploads/:id/*", func(c fiber.Ctx) error {
		path, mimeType, err := store.Fetch(c.Params("id"))
		if err != nil {
			return uploadError(c, err)
		}
		f, err := os.Open(path)
		if err != nil {
			return uploadError(c, upload.ErrNotFound)
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return uploadError(c, upload.ErrNotFound)
		}
		c.Set(fiber.HeaderContentType, mimeType)
		c.Set(fiber.HeaderCacheControl, "private, no-store")
		return c.SendStream(f, int(info.Size())) // fiber closes f once sent
	})
}

func uploadError(c fiber.Ctx, err error) error {
	switch err {
	case upload.ErrNotFound, upload.ErrExpired:
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"status": fiber.StatusNotFound, "code": "NOT_FOUND", "message": "upload ticket not found or expired",
		})
	case upload.ErrAlreadyUploaded:
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"status": fiber.StatusConflict, "code": "ALREADY_UPLOADED", "message": "this upload ticket was already used",
		})
	case upload.ErrTooLarge:
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{
			"status": fiber.StatusRequestEntityTooLarge, "code": "TOO_LARGE", "message": "file exceeds the size limit",
		})
	case upload.ErrUnsupportedType:
		return c.Status(fiber.StatusUnsupportedMediaType).JSON(fiber.Map{
			"status": fiber.StatusUnsupportedMediaType, "code": "UNSUPPORTED_TYPE", "message": "unsupported content type; use jpg/png/webp",
		})
	default:
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"status": fiber.StatusInternalServerError, "code": "INTERNAL_ERROR", "message": "internal error",
		})
	}
}
