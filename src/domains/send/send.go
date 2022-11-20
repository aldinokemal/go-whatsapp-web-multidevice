package send

import (
	"github.com/gofiber/fiber/v2"
)

type Type string

const TypeUser Type = "user"
const TypeGroup Type = "group"

type ISendService interface {
	SendText(c *fiber.Ctx, request MessageRequest) (response MessageResponse, err error)
	SendImage(c *fiber.Ctx, request ImageRequest) (response ImageResponse, err error)
	SendFile(c *fiber.Ctx, request FileRequest) (response FileResponse, err error)
	SendVideo(c *fiber.Ctx, request VideoRequest) (response VideoResponse, err error)
	SendContact(c *fiber.Ctx, request ContactRequest) (response ContactResponse, err error)
}
