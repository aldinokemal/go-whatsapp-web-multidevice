package services

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/gofiber/fiber/v2"
)

type SendService interface {
	SendText(c *fiber.Ctx, request structs.SendMessageRequest) (response structs.SendMessageResponse, err error)
	SendImage(c *fiber.Ctx, request structs.SendImageRequest) (response structs.SendImageResponse, err error)
	SendFile(c *fiber.Ctx, request structs.SendFileRequest) (response structs.SendFileResponse, err error)
	SendVideo(c *fiber.Ctx, request structs.SendVideoRequest) (response structs.SendVideoResponse, err error)
	SendContact(c *fiber.Ctx, request structs.SendContactRequest) (response structs.SendContactResponse, err error)
}
