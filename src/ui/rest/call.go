package rest

import (
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type Call struct {
	Service domainCall.ICallUsecase
}

func InitRestCall(app fiber.Router, service domainCall.ICallUsecase) Call {
	rest := Call{Service: service}
	app.Post("/call/reject", rest.RejectCall)
	app.Get("/call/logs", rest.ListCallLogs)
	return rest
}

func (controller *Call) RejectCall(c fiber.Ctx) error {
	var request domainCall.RejectCallRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.RejectCall(
		whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)),
		request.CallerJID,
		request.CallID,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Call rejected successfully",
		Results: nil,
	})
}

func (controller *Call) ListCallLogs(c fiber.Ctx) error {
	var request domainCall.ListCallLogsRequest

	request.Limit = fiber.Query[int](c, "limit", 25)
	request.Offset = fiber.Query[int](c, "offset", 0)
	request.ChatJID = c.Query("chat_jid", "")

	response, err := controller.Service.ListCallLogs(
		whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get call logs",
		Results: response,
	})
}
