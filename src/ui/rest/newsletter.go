package rest

import (
	"fmt"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Newsletter struct {
	Service domainNewsletter.INewsletterUsecase
}

func InitRestNewsletter(app *fiber.App, service domainNewsletter.INewsletterUsecase) Newsletter {
	rest := Newsletter{Service: service}
	app.Post("/newsletter/unfollow", rest.Unfollow)
	return rest
}

func (controller *Newsletter) Unfollow(c *fiber.Ctx) error {
	var request domainNewsletter.UnfollowRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	if err := controller.Service.Unfollow(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "UNFOLLOW_NEWSLETTER_FAILED",
			Message: fmt.Sprintf("Failed to unfollow newsletter: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success unfollow newsletter",
	})
}
