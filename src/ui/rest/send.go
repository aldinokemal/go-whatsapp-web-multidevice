package rest

import (
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Send struct {
	Service domainSend.ISendUsecase
}

func InitRestSend(app fiber.Router, service domainSend.ISendUsecase) Send {
	rest := Send{Service: service}
	app.Post("/send/message", rest.SendText)
	app.Post("/send/image", rest.SendImage)
	app.Post("/send/file", rest.SendFile)
	app.Post("/send/video", rest.SendVideo)
	app.Post("/send/sticker", rest.SendSticker)
	app.Post("/send/contact", rest.SendContact)
	app.Post("/send/link", rest.SendLink)
	app.Post("/send/location", rest.SendLocation)
	app.Post("/send/audio", rest.SendAudio)
	app.Post("/send/poll", rest.SendPoll)
	app.Post("/send/presence", rest.SendPresence)
	app.Post("/send/chat-presence", rest.SendChatPresence)
	return rest
}

func (controller *Send) SendText(c *fiber.Ctx) error {
	var request domainSend.MessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendText(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendImage(c *fiber.Ctx) error {
	var request domainSend.ImageRequest
	request.Compress = true

	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("image")
	if err == nil {
		request.Image = file
	}

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendImage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendFile(c *fiber.Ctx) error {
	var request domainSend.FileRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("file")
	utils.PanicIfNeeded(err)

	request.File = file
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendFile(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendVideo(c *fiber.Ctx) error {
	var request domainSend.VideoRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// Try to get file but ignore error if not provided
	if videoFile, errFile := c.FormFile("video"); errFile == nil {
		request.Video = videoFile
	}

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendVideo(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendSticker(c *fiber.Ctx) error {
	var request domainSend.StickerRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// Try to get file but ignore error if not provided
	if stickerFile, errFile := c.FormFile("sticker"); errFile == nil {
		request.Sticker = stickerFile
	}

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendSticker(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendContact(c *fiber.Ctx) error {
	var request domainSend.ContactRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendContact(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendLink(c *fiber.Ctx) error {
	var request domainSend.LinkRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendLink(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendLocation(c *fiber.Ctx) error {
	var request domainSend.LocationRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendLocation(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendAudio(c *fiber.Ctx) error {
	var request domainSend.AudioRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// Try to get file but ignore error if not provided
	if audioFile, errFile := c.FormFile("audio"); errFile == nil {
		request.Audio = audioFile
	}

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendAudio(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendPoll(c *fiber.Ctx) error {
	var request domainSend.PollRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendPoll(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendPresence(c *fiber.Ctx) error {
	var request domainSend.PresenceRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.SendPresence(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendChatPresence(c *fiber.Ctx) error {
	var request domainSend.ChatPresenceRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendChatPresence(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}
