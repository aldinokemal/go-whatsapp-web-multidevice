package rest

import (
	"fmt"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Send struct {
	Service domainSend.ISendUsecase
}

func InitRestSend(app *fiber.App, service domainSend.ISendUsecase) Send {
	rest := Send{Service: service}
	app.Post("/send/message", rest.SendText)
	app.Post("/send/image", rest.SendImage)
	app.Post("/send/file", rest.SendFile)
	app.Post("/send/video", rest.SendVideo)
	app.Post("/send/contact", rest.SendContact)
	app.Post("/send/link", rest.SendLink)
	app.Post("/send/location", rest.SendLocation)
	app.Post("/send/audio", rest.SendAudio)
	app.Post("/send/poll", rest.SendPoll)
	app.Post("/send/presence", rest.SendPresence)
	return rest
}

func (controller *Send) SendText(c *fiber.Ctx) error {
	var request domainSend.MessageRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendText(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_TEXT_FAILED",
			Message: fmt.Sprintf("Failed to send text message: %v", err),
		})
	}

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

	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	file, err := c.FormFile("image")
	if err == nil {
		request.Image = file
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendImage(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_IMAGE_FAILED",
			Message: fmt.Sprintf("Failed to send image: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendFile(c *fiber.Ctx) error {
	var request domainSend.FileRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "FILE_REQUIRED",
			Message: "File is required for this operation",
		})
	}

	request.File = file
	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendFile(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_FILE_FAILED",
			Message: fmt.Sprintf("Failed to send file: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendVideo(c *fiber.Ctx) error {
	var request domainSend.VideoRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	// Try to get file but ignore error if not provided
	if videoFile, errFile := c.FormFile("video"); errFile == nil {
		request.Video = videoFile
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendVideo(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_VIDEO_FAILED",
			Message: fmt.Sprintf("Failed to send video: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendContact(c *fiber.Ctx) error {
	var request domainSend.ContactRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendContact(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_CONTACT_FAILED",
			Message: fmt.Sprintf("Failed to send contact: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendLink(c *fiber.Ctx) error {
	var request domainSend.LinkRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendLink(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_LINK_FAILED",
			Message: fmt.Sprintf("Failed to send link: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendLocation(c *fiber.Ctx) error {
	var request domainSend.LocationRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendLocation(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_LOCATION_FAILED",
			Message: fmt.Sprintf("Failed to send location: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendAudio(c *fiber.Ctx) error {
	var request domainSend.AudioRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	// Try to get file but ignore error if not provided
	if audioFile, errFile := c.FormFile("audio"); errFile == nil {
		request.Audio = audioFile
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendAudio(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_AUDIO_FAILED",
			Message: fmt.Sprintf("Failed to send audio: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendPoll(c *fiber.Ctx) error {
	var request domainSend.PollRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendPoll(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_POLL_FAILED",
			Message: fmt.Sprintf("Failed to send poll: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendPresence(c *fiber.Ctx) error {
	var request domainSend.PresenceRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	response, err := controller.Service.SendPresence(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SEND_PRESENCE_FAILED",
			Message: fmt.Sprintf("Failed to send presence: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}
