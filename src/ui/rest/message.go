package rest

import (
	"fmt"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Message struct {
	Service domainMessage.IMessageUsecase
}

func InitRestMessage(app *fiber.App, service domainMessage.IMessageUsecase) Message {
	rest := Message{Service: service}
	app.Post("/message/:message_id/reaction", rest.ReactMessage)
	app.Post("/message/:message_id/revoke", rest.RevokeMessage)
	app.Post("/message/:message_id/delete", rest.DeleteMessage)
	app.Post("/message/:message_id/update", rest.UpdateMessage)
	app.Post("/message/:message_id/read", rest.MarkAsRead)
	app.Post("/message/:message_id/star", rest.StarMessage)
	app.Post("/message/:message_id/unstar", rest.UnstarMessage)
	return rest
}

func (controller *Message) RevokeMessage(c *fiber.Ctx) error {
	var request domainMessage.RevokeRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	request.MessageID = c.Params("message_id")
	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.RevokeMessage(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "REVOKE_MESSAGE_FAILED",
			Message: fmt.Sprintf("Failed to revoke message: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) DeleteMessage(c *fiber.Ctx) error {
	var request domainMessage.DeleteRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	request.MessageID = c.Params("message_id")
	whatsapp.SanitizePhone(&request.Phone)

	if err := controller.Service.DeleteMessage(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "DELETE_MESSAGE_FAILED",
			Message: fmt.Sprintf("Failed to delete message: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Message deleted successfully",
		Results: nil,
	})
}

func (controller *Message) UpdateMessage(c *fiber.Ctx) error {
	var request domainMessage.UpdateMessageRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	request.MessageID = c.Params("message_id")
	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.UpdateMessage(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "UPDATE_MESSAGE_FAILED",
			Message: fmt.Sprintf("Failed to update message: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) ReactMessage(c *fiber.Ctx) error {
	var request domainMessage.ReactionRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	request.MessageID = c.Params("message_id")
	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.ReactMessage(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "REACT_MESSAGE_FAILED",
			Message: fmt.Sprintf("Failed to react to message: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) MarkAsRead(c *fiber.Ctx) error {
	var request domainMessage.MarkAsReadRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	request.MessageID = c.Params("message_id")
	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.MarkAsRead(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "MARK_AS_READ_FAILED",
			Message: fmt.Sprintf("Failed to mark message as read: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) StarMessage(c *fiber.Ctx) error {
	var request domainMessage.StarRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	request.MessageID = c.Params("message_id")
	whatsapp.SanitizePhone(&request.Phone)
	request.IsStarred = true

	if err := controller.Service.StarMessage(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "STAR_MESSAGE_FAILED",
			Message: fmt.Sprintf("Failed to star message: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Starred message successfully",
		Results: nil,
	})
}

func (controller *Message) UnstarMessage(c *fiber.Ctx) error {
	var request domainMessage.StarRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	request.MessageID = c.Params("message_id")
	whatsapp.SanitizePhone(&request.Phone)
	request.IsStarred = false

	if err := controller.Service.StarMessage(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "UNSTAR_MESSAGE_FAILED",
			Message: fmt.Sprintf("Failed to unstar message: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Unstarred message successfully",
		Results: nil,
	})
}
