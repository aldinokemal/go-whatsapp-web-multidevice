package rest

import (
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Call struct {
	Service domainCall.ICallUsecase
}

func InitRestCall(app fiber.Router, service domainCall.ICallUsecase) Call {
	rest := Call{Service: service}
	app.Post("/call/reject", rest.RejectCall)
	return rest
}

func (controller *Call) RejectCall(c *fiber.Ctx) error {
	var request domainCall.RejectCallRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.RejectCall(
		whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)),
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
