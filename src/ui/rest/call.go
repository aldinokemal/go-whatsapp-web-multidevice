package rest

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

// callIDRequest is the shared body for the beta endpoints that act on a single
// live call (answer, hangup).
type callIDRequest struct {
	CallID string `json:"call_id" form:"call_id"`
}

// answerRequest accepts an inbound call and optionally records the peer's audio
// to a WAV file.
type answerRequest struct {
	CallID     string `json:"call_id" form:"call_id"`
	RecordPath string `json:"record_path" form:"record_path"`
}

type Call struct {
	Service domainCall.ICallUsecase
}

func InitRestCall(app fiber.Router, service domainCall.ICallUsecase) Call {
	rest := Call{Service: service}
	app.Post("/call/reject", rest.RejectCall)
	if config.WhatsappBetaCallEnabled {
		app.Post("/call/start", rest.StartCall)
		app.Post("/call/answer", rest.AnswerCall)
		app.Post("/call/hangup", rest.HangupCall)
		// Live two-way audio for the browser console. Browsers cannot set
		// headers on WebSocket handshakes, so the device comes from ?device_id=.
		app.Use("/call/audio", func(c fiber.Ctx) error {
			if websocket.IsWebSocketUpgrade(c) {
				return c.Next()
			}
			return c.SendStatus(fiber.StatusUpgradeRequired)
		})
		app.Get("/call/audio", websocket.New(rest.AudioStream))
	}
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

// StartCall places a beta 1:1 voice call. body: {"target": "+628...", "audio_file_path": "hello.mp3"}
// Media paths are relative to config.PathStorages.
func (controller *Call) StartCall(c fiber.Ctx) error {
	var request domainCall.StartCallRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.StartCall(
		whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)),
		request,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Call started successfully",
		Results: response,
	})
}

// AnswerCall accepts a ringing beta inbound call. body: {"call_id": "...", "record_path": "optional.wav"}
func (controller *Call) AnswerCall(c fiber.Ctx) error {
	var request answerRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.AnswerCall(
		whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)),
		request.CallID,
		request.RecordPath,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Call answered successfully",
		Results: nil,
	})
}

// HangupCall ends an active beta call. body: {"call_id": "..."}
func (controller *Call) HangupCall(c fiber.Ctx) error {
	var request callIDRequest
	err := c.Bind().Body(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.HangupCall(
		whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)),
		request.CallID,
	)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Call ended successfully",
		Results: nil,
	})
}
