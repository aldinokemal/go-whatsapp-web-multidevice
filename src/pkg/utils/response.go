package utils

import (
	"github.com/gofiber/fiber/v2"
)

type ResponseData struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Results any    `json:"results,omitempty"`
}

func ResponseError(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(ResponseData{
		Code:    "INVALID_REQUEST",
		Message: message,
	})
}
