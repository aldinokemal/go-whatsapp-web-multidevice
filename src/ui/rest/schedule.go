package rest

import (
	"strings"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type Schedule struct {
	Service domainSend.IScheduleUsecase
}

func InitRestSchedule(app fiber.Router, service domainSend.IScheduleUsecase) Schedule {
	rest := Schedule{Service: service}
	app.Get("/send/schedules", rest.List)
	app.Get("/send/schedules/:schedule_id", rest.Get)
	app.Post("/send/schedules/:schedule_id/pause", rest.Pause)
	app.Post("/send/schedules/:schedule_id/resume", rest.Resume)
	app.Post("/send/schedules/:schedule_id/cancel", rest.Cancel)
	return rest
}

func (controller Schedule) List(c fiber.Ctx) error {
	filter := domainSend.ScheduleFilter{
		Status:      strings.TrimSpace(c.Query("status")),
		Search:      strings.TrimSpace(c.Query("search")),
		MessageType: strings.TrimSpace(c.Query("message_type")),
		Limit:       fiber.Query[int](c, "limit", 25),
		Offset:      fiber.Query[int](c, "offset", 0),
	}
	result, err := controller.Service.List(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), filter)
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Schedules fetched", Results: result})
}

func (controller Schedule) Get(c fiber.Ctx) error {
	result, err := controller.Service.Get(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), c.Params("schedule_id"))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Schedule fetched", Results: result})
}

func (controller Schedule) Pause(c fiber.Ctx) error {
	err := controller.Service.Pause(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), c.Params("schedule_id"))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Schedule paused", Results: nil})
}

func (controller Schedule) Resume(c fiber.Ctx) error {
	err := controller.Service.Resume(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), c.Params("schedule_id"))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Schedule resumed", Results: nil})
}

func (controller Schedule) Cancel(c fiber.Ctx) error {
	err := controller.Service.Cancel(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), c.Params("schedule_id"))
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Schedule cancelled", Results: nil})
}
