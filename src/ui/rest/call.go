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
	app.Post("/call", rest.StartCall)
	app.Post("/call/reject", rest.RejectIncomingCall)
	app.Post("/call/:call_id/webrtc", rest.ExchangeWebRTC)
	app.Post("/call/:call_id/accept", rest.AcceptCall)
	app.Post("/call/:call_id/reject", rest.RejectCall)
	app.Delete("/call/:call_id", rest.EndCall)
	app.Get("/calls", rest.ListCalls)
	app.Get("/call/:call_id", rest.GetCall)
	return rest
}

func (controller *Call) StartCall(c *fiber.Ctx) error {
	var request domainCall.StartCallRequest
	utils.PanicIfNeeded(c.BodyParser(&request))

	response, err := controller.Service.StartCall(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Call started", Results: response})
}

func (controller *Call) ExchangeWebRTC(c *fiber.Ctx) error {
	var request domainCall.WebRTCRequest
	utils.PanicIfNeeded(c.BodyParser(&request))
	request.CallID = c.Params("call_id")

	response, err := controller.Service.ExchangeWebRTC(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "WebRTC negotiated", Results: response})
}

func (controller *Call) AcceptCall(c *fiber.Ctx) error {
	var request domainCall.CallIDRequest
	_ = c.BodyParser(&request)
	request.CallID = c.Params("call_id")

	response, err := controller.Service.AcceptCall(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Call accepted", Results: response})
}

func (controller *Call) RejectCall(c *fiber.Ctx) error {
	response, err := controller.Service.RejectCall(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)), domainCall.CallIDRequest{CallID: c.Params("call_id")})
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Call rejected", Results: response})
}

func (controller *Call) RejectIncomingCall(c *fiber.Ctx) error {
	var request domainCall.RejectCallRequest
	utils.PanicIfNeeded(c.BodyParser(&request))

	err := controller.Service.RejectIncomingCall(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Call rejected successfully",
		Results: nil,
	})
}

func (controller *Call) EndCall(c *fiber.Ctx) error {
	response, err := controller.Service.EndCall(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)), domainCall.CallIDRequest{CallID: c.Params("call_id")})
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Call ended", Results: response})
}

func (controller *Call) GetCall(c *fiber.Ctx) error {
	response, err := controller.Service.GetCall(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)), domainCall.CallIDRequest{CallID: c.Params("call_id")})
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Call found", Results: response})
}

func (controller *Call) ListCalls(c *fiber.Ctx) error {
	response, err := controller.Service.ListCalls(whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c)))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Calls found", Results: response})
}
