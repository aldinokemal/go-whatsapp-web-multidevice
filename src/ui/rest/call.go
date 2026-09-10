package rest

import (
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
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

	err := c.Bind().Query(&request)
	if err != nil {
		err = pkgError.ValidationError(err.Error())
	}
	utils.PanicIfNeeded(err)

	response, err := controller.Service.ListCallLogs(
		whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	c.Set("Cache-Control", "no-store")
	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get call logs",
		Results: response,
	})
}
