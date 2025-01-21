package rest

import (
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type Send struct {
	Service domainSend.ISendService
}

func InitRestSend(app *fiber.App, service domainSend.ISendService) Send {
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
	return rest
}

func (controller *Send) SendText(c *fiber.Ctx) error {
	var request domainSend.MessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.Phone)

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
	logrus.Debug("Starting SendImage handler")
	
	var request domainSend.ImageRequest
	request.Compress = true
	
	if isJsonRequest(c) {
		logrus.Debug("Processing JSON request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse JSON body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON body")
		}
	} else {
		logrus.Debug("Processing multipart form request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse form body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid form body")
		}
		
		request.ImageUrl = c.FormValue("image_url")
		if request.ImageUrl == "" {
			if file, err := c.FormFile("image"); err == nil {
				request.Image = file
				logrus.WithField("filename", file.Filename).Debug("Image file received")
			}
		}
	}

	logrus.WithFields(logrus.Fields{
		"content_type": c.Get("Content-Type"),
		"phone": request.Phone,
		"image_url": request.ImageUrl,
		"has_image": request.Image != nil,
		"caption": request.Caption,
		"view_once": request.ViewOnce,
		"compress": request.Compress,
	}).Debug("Request details")

	if request.ImageUrl == "" && request.Image == nil {
		return fiber.NewError(fiber.StatusBadRequest, "Either image or image_url must be provided")
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendImage(c.UserContext(), request)
	if err != nil {
		logrus.WithError(err).Error("Failed to send image")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to send image: " + err.Error())
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func isJsonRequest(c *fiber.Ctx) bool {
	return c.Get("Content-Type") == "application/json"
}

func (controller *Send) SendFile(c *fiber.Ctx) error {
	logrus.Debug("Starting SendFile handler")
	
	var request domainSend.FileRequest
	
	// Handle based on content type
	if isJsonRequest(c) {
		logrus.Debug("Processing JSON request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse JSON body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON body")
		}
	} else {
		logrus.Debug("Processing multipart form request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse form body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid form body")
		}
		
		// Handle form-specific fields
		request.FileUrl = c.FormValue("file_url")
		if request.FileUrl == "" {
			file, err := c.FormFile("file")
			if err == nil {
				request.File = file
			}
		}
	}

	logrus.WithFields(logrus.Fields{
		"content_type": c.Get("Content-Type"),
		"phone": request.Phone,
		"file_url": request.FileUrl,
		"has_file": request.File != nil,
	}).Debug("Request details")

	// Validate request
	if request.FileUrl == "" && request.File == nil {
		return fiber.NewError(fiber.StatusBadRequest, "Either file or file_url must be provided")
	}

	logrus.Debug("Sanitizing phone number")
	whatsapp.SanitizePhone(&request.Phone)

	logrus.WithField("phone", request.Phone).Debug("Sending file")
	response, err := controller.Service.SendFile(c.UserContext(), request)
	if err != nil {
		logrus.WithError(err).Error("Service.SendFile failed")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to send file: " + err.Error())
	}

	logrus.WithField("message_id", response.MessageID).Debug("File sent successfully")
	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendVideo(c *fiber.Ctx) error {
	var request domainSend.VideoRequest
	
	if isJsonRequest(c) {
		logrus.Debug("Processing JSON request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse JSON body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON body")
		}
	} else {
		logrus.Debug("Processing multipart form request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse form body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid form body")
		}
		
		request.VideoUrl = c.FormValue("video_url")
		if request.VideoUrl == "" {
			video, err := c.FormFile("video")
			if err == nil {
				request.Video = video
			}
		}
	}

	logrus.WithFields(logrus.Fields{
		"content_type": c.Get("Content-Type"),
		"phone": request.Phone,
		"video_url": request.VideoUrl,
		"has_video": request.Video != nil,
	}).Debug("Request details")

	if request.VideoUrl == "" && request.Video == nil {
		return fiber.NewError(fiber.StatusBadRequest, "Either video or video_url must be provided")
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendVideo(c.UserContext(), request)
	if err != nil {
		logrus.WithError(err).Error("Failed to send video")
		return err
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
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.Phone)

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

	whatsapp.SanitizePhone(&request.Phone)

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

	whatsapp.SanitizePhone(&request.Phone)

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
	
	if isJsonRequest(c) {
		logrus.Debug("Processing JSON request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse JSON body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON body")
		}
	} else {
		logrus.Debug("Processing multipart form request")
		if err := c.BodyParser(&request); err != nil {
			logrus.WithError(err).Error("Failed to parse form body")
			return fiber.NewError(fiber.StatusBadRequest, "Invalid form body")
		}
		
		audio, err := c.FormFile("audio")
		if err == nil {
			request.Audio = audio
		}
	}

	logrus.WithFields(logrus.Fields{
		"content_type": c.Get("Content-Type"),
		"phone": request.Phone,
		"has_audio": request.Audio != nil,
	}).Debug("Request details")

	if request.Audio == nil {
		return fiber.NewError(fiber.StatusBadRequest, "Audio file must be provided")
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendAudio(c.UserContext(), request)
	if err != nil {
		logrus.WithError(err).Error("Failed to send audio")
		return err
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
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.SendPoll(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}
