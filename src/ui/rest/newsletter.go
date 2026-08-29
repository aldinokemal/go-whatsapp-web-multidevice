package rest

import (
	"strconv"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type Newsletter struct {
	Service domainNewsletter.INewsletterUsecase
}

func InitRestNewsletter(app fiber.Router, service domainNewsletter.INewsletterUsecase) Newsletter {
	rest := Newsletter{Service: service}
	app.Post("/newsletter/unfollow", rest.Unfollow)
	app.Get("/newsletter/messages", rest.GetMessages)
	app.Get("/newsletter/messages/:server_id/download", rest.DownloadMedia)
	return rest
}

func (controller *Newsletter) Unfollow(c fiber.Ctx) error {
	var request domainNewsletter.UnfollowRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.Unfollow(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success unfollow newsletter",
	})
}

func (controller *Newsletter) GetMessages(c fiber.Ctx) error {
	var request domainNewsletter.GetMessagesRequest
	err := c.Bind().Query(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.GetMessages(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get newsletter messages",
		Results: response,
	})
}

func (controller *Newsletter) DownloadMedia(c fiber.Ctx) error {
	var request domainNewsletter.DownloadMediaRequest
	err := c.Bind().Query(&request)
	utils.PanicIfNeeded(err)

	request.ServerID, err = strconv.Atoi(c.Params("server_id"))
	if err != nil {
		utils.PanicIfNeeded(pkgError.ValidationError("server_id: must be a valid integer."))
	}

	response, err := controller.Service.DownloadMedia(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)
	if response.FileURL == "" {
		response.FileURL = publicStaticFileURL(c, response.FilePath)
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}
