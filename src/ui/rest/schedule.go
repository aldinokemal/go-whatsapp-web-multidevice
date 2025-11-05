package rest

import (
	"strconv"
	"strings"

	domainSchedule "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/schedule"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

var allowedScheduleStatuses = map[domainSchedule.Status]struct{}{
	domainSchedule.StatusPending: {},
	domainSchedule.StatusSending: {},
	domainSchedule.StatusSent:    {},
	domainSchedule.StatusFailed:  {},
}

type Schedule struct {
	Service domainSchedule.IScheduleUsecase
}

func InitRestSchedule(app fiber.Router, service domainSchedule.IScheduleUsecase) Schedule {
	rest := Schedule{Service: service}

	app.Get("/schedule/messages", rest.List)
	app.Get("/schedule/messages/:id", rest.Get)
	app.Post("/schedule/messages", rest.Create)
	app.Put("/schedule/messages/:id", rest.Update)
	app.Delete("/schedule/messages/:id", rest.Delete)
	app.Post("/schedule/messages/:id/run", rest.RunNow)

	return rest
}

func (controller *Schedule) List(c *fiber.Ctx) error {
	statusesParam := c.Query("statuses")
	statuses, err := parseStatuses(statusesParam)
	utils.PanicIfNeeded(err)

	limit := parseQueryInt(c.Query("limit"), 0)
	offset := parseQueryInt(c.Query("offset"), 0)

	request := domainSchedule.ListScheduledMessagesRequest{
		Statuses: statuses,
		Limit:    limit,
		Offset:   offset,
	}

	messages, err := controller.Service.List(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Scheduled messages fetched",
		Results: map[string]any{
			"items": messages,
		},
	})
}

func (controller *Schedule) Create(c *fiber.Ctx) error {
	var payload domainSchedule.ScheduleMessagePayload
	err := c.BodyParser(&payload)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&payload.Phone)

	message, err := controller.Service.Create(c.UserContext(), payload)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Scheduled message created",
		Results: message,
	})
}

func (controller *Schedule) Get(c *fiber.Ctx) error {
	id, err := parseScheduleID(c)
	utils.PanicIfNeeded(err)

	message, err := controller.Service.Get(c.UserContext(), id)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Scheduled message fetched",
		Results: message,
	})
}

func (controller *Schedule) Update(c *fiber.Ctx) error {
	id, err := parseScheduleID(c)
	utils.PanicIfNeeded(err)

	var payload domainSchedule.ScheduleMessagePayload
	err = c.BodyParser(&payload)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&payload.Phone)

	message, err := controller.Service.Update(c.UserContext(), id, payload)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Scheduled message updated",
		Results: message,
	})
}

func (controller *Schedule) Delete(c *fiber.Ctx) error {
	id, err := parseScheduleID(c)
	utils.PanicIfNeeded(err)

	err = controller.Service.Delete(c.UserContext(), id)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Scheduled message deleted",
	})
}

func (controller *Schedule) RunNow(c *fiber.Ctx) error {
	id, err := parseScheduleID(c)
	utils.PanicIfNeeded(err)

	message, err := controller.Service.RunNow(c.UserContext(), id)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Scheduled message dispatched",
		Results: message,
	})
}

func parseScheduleID(c *fiber.Ctx) (int64, error) {
	idStr := strings.TrimSpace(c.Params("id"))
	if idStr == "" {
		return 0, pkgError.ValidationError("scheduled message id is required")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, pkgError.ValidationError("invalid scheduled message id")
	}

	return id, nil
}

func parseStatuses(raw string) ([]domainSchedule.Status, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	statuses := make([]domainSchedule.Status, 0, len(parts))
	for _, part := range parts {
		trimmed := domainSchedule.Status(strings.TrimSpace(part))
		if trimmed == "" {
			continue
		}

		if _, ok := allowedScheduleStatuses[trimmed]; !ok {
			return nil, pkgError.ValidationError("invalid status filter provided")
		}

		statuses = append(statuses, trimmed)
	}

	return statuses, nil
}

func parseQueryInt(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	v, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return v
}
