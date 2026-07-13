package rest

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type Message struct {
	Service     domainMessage.IMessageUsecase
	SendService domainSend.ISendUsecase
}

func InitRestMessage(app fiber.Router, service domainMessage.IMessageUsecase, sendService domainSend.ISendUsecase) Message {
	rest := Message{Service: service, SendService: sendService}

	// Message action endpoints
	app.Post("/message/:message_id/reaction", rest.ReactMessage)
	app.Post("/message/:message_id/revoke", rest.RevokeMessage)
	app.Post("/message/:message_id/delete", rest.DeleteMessage)
	app.Post("/message/:message_id/update", rest.UpdateMessage)
	app.Post("/message/:message_id/read", rest.MarkAsRead)
	app.Post("/message/:message_id/star", rest.StarMessage)
	app.Post("/message/:message_id/unstar", rest.UnstarMessage)
	app.Post("/message/:message_id/forward", rest.ForwardMessage)
	app.Get("/message/:message_id/download", rest.DownloadMedia)
	return rest
}

func (controller *Message) RevokeMessage(c fiber.Ctx) error {
	var request domainMessage.RevokeRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.RevokeMessage(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) DeleteMessage(c fiber.Ctx) error {
	var request domainMessage.DeleteRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	err = controller.Service.DeleteMessage(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Message deleted successfully",
		Results: nil,
	})
}

func (controller *Message) UpdateMessage(c fiber.Ctx) error {
	var request domainMessage.UpdateMessageRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.UpdateMessage(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) ReactMessage(c fiber.Ctx) error {
	var request domainMessage.ReactionRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.ReactMessage(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) MarkAsRead(c fiber.Ctx) error {
	var request domainMessage.MarkAsReadRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.MarkAsRead(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) StarMessage(c fiber.Ctx) error {
	var request domainMessage.StarRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)
	request.IsStarred = true

	err = controller.Service.StarMessage(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Starred message successfully",
		Results: nil,
	})
}

func (controller *Message) UnstarMessage(c fiber.Ctx) error {
	var request domainMessage.StarRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)
	request.IsStarred = false
	err = controller.Service.StarMessage(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Unstarred message successfully",
		Results: nil,
	})
}

func (controller *Message) ForwardMessage(c fiber.Ctx) error {
	var request domainSend.ForwardRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.SendService.SendForward(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) DownloadMedia(c fiber.Ctx) error {
	var request domainMessage.DownloadMediaRequest

	request.MessageID = c.Params("message_id")
	request.Phone = c.Query("phone")
	utils.SanitizePhone(&request.Phone)

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

func publicStaticFileURL(c fiber.Ctx, filePath string) string {
	staticPath := publicStaticPath(filePath)
	if staticPath == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s%s%s", c.Scheme(), c.Hostname(), config.AppBasePath, staticPath)
}

func publicStaticPath(filePath string) string {
	if filePath == "" {
		return ""
	}

	normalizedPath := filepath.FromSlash(strings.ReplaceAll(filePath, "\\", "/"))
	rel, err := filepath.Rel("statics", normalizedPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return "/statics/" + strings.Join(parts, "/")
}
