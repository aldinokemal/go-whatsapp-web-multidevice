package rest

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// InitRestMedia initializes media download routes
func InitRestMedia(router fiber.Router) {
	// Support both nested path format and simple filename
	router.Get("/media/download/:deviceid/:chatjid/:filename", downloadMediaHandler)
	router.Get("/media/download/:filename", downloadMediaHandler)
}

// downloadMediaHandler handles media downloads for private S3 buckets
// This endpoint fetches media from S3 using server credentials and streams to client
func downloadMediaHandler(c *fiber.Ctx) error {
	// Build path from parameters (supports nested or simple format)
	deviceID := c.Params("deviceid")
	chatJID := c.Params("chatjid")
	filename := c.Params("filename")

	if filename == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "filename is required",
		})
	}

	// Build full path if nested format is used
	var path string
	if deviceID != "" && chatJID != "" {
		path = deviceID + "/" + chatJID + "/" + filename
	} else {
		path = filename
	}

	logrus.Debugf("📥 Downloading media from storage: %s", path)

	// Get storage instance
	mediaStorage := storage.GetStorage()
	if mediaStorage == nil {
		logrus.Error("Media storage not initialized")
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "media storage not initialized",
		})
	}

	// Fetch media from storage
	ctx := context.Background()
	data, err := mediaStorage.Get(ctx, path)
	if err != nil {
		logrus.Errorf("Failed to download media %s: %v", path, err)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "media not found",
		})
	}

	logrus.Debugf("✅ Successfully downloaded media: %s, size: %d bytes", path, len(data))

	// Set appropriate content type header
	c.Set("Content-Type", "application/octet-stream")
	c.Set("Content-Disposition", "inline; filename=\""+filename+"\"")

	return c.Send(data)
}
