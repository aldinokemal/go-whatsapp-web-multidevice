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
	app.Post("/newsletter/follow", rest.Follow)
	app.Post("/newsletter/unfollow", rest.Unfollow)
	app.Get("/newsletter/info", rest.GetInfo)
	app.Get("/newsletter/directory", rest.Directory)
	app.Get("/newsletter/messages", rest.GetMessages)
	app.Get("/newsletter/message-updates", rest.GetMessageUpdates)
	app.Get("/newsletter/live-updates", rest.ListLiveUpdates)
	app.Post("/newsletter/live-updates", rest.SubscribeLiveUpdates)
	app.Delete("/newsletter/live-updates", rest.UnsubscribeLiveUpdates)
	app.Get("/newsletter/messages/:server_id/download", rest.DownloadMedia)
	return rest
}

func (controller *Newsletter) Follow(c fiber.Ctx) error {
	var request domainNewsletter.FollowRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.Follow(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success follow newsletter",
	})
}

func (controller *Newsletter) GetInfo(c fiber.Ctx) error {
	var request domainNewsletter.GetInfoRequest
	err := c.Bind().Query(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.GetInfo(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get newsletter info",
		Results: response,
	})
}

func (controller *Newsletter) Directory(c fiber.Ctx) error {
	var request domainNewsletter.DirectoryRequest
	err := c.Bind().Query(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.Directory(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success search newsletter directory",
		Results: response,
	})
}

func (controller *Newsletter) GetMessageUpdates(c fiber.Ctx) error {
	var request domainNewsletter.GetMessageUpdatesRequest
	err := c.Bind().Query(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.GetMessageUpdates(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get newsletter message updates",
		Results: response,
	})
}

func (controller *Newsletter) ListLiveUpdates(c fiber.Ctx) error {
	response, err := controller.Service.ListLiveUpdates(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)))
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success list newsletter live updates",
		Results: response,
	})
}

func (controller *Newsletter) SubscribeLiveUpdates(c fiber.Ctx) error {
	var request domainNewsletter.LiveUpdatesRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.SubscribeLiveUpdates(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success subscribe newsletter live updates",
		Results: response,
	})
}

func (controller *Newsletter) UnsubscribeLiveUpdates(c fiber.Ctx) error {
	var request domainNewsletter.LiveUpdatesRequest
	err := c.Bind().Query(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.UnsubscribeLiveUpdates(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success unsubscribe newsletter live updates",
	})
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
