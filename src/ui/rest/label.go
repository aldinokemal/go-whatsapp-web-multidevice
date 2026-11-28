package rest

import (
	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type Label struct {
	Service domainLabel.ILabelUsecase
}

func InitRestLabel(app fiber.Router, service domainLabel.ILabelUsecase) Label {
	rest := Label{Service: service}

	app.Get("/labels", rest.ListLabels)
	app.Post("/labels/resync", rest.ResyncLabels)
	app.Post("/label", rest.EditLabel)
	app.Post("/chat/:chat_jid/label", rest.LabelChat)

	return rest
}

func (controller *Label) ListLabels(c fiber.Ctx) error {
	response, err := controller.Service.ListLabels(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)))
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get labels",
		Results: response,
	})
}

func (controller *Label) ResyncLabels(c fiber.Ctx) error {
	response, err := controller.Service.ResyncLabels(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)))
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Labels resynced successfully",
		Results: response,
	})
}

func (controller *Label) EditLabel(c fiber.Ctx) error {
	var request domainLabel.EditLabelRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	response, err := controller.Service.EditLabel(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Message,
		Results: response,
	})
}

func (controller *Label) LabelChat(c fiber.Ctx) error {
	var request domainLabel.LabelChatRequest

	// Parse path parameter
	chatJID, err := chatJIDParam(c)
	if err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "invalid chat_jid path parameter: " + err.Error(),
			Results: nil,
		})
	}

	// Parse JSON body
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}
	// Route JID is authoritative; a body chat_jid must never override it.
	request.ChatJID = chatJID

	response, err := controller.Service.LabelChat(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Message,
		Results: response,
	})
}
