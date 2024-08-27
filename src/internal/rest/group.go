package rest

import (
	"fmt"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"github.com/gofiber/fiber/v2"
	"go.mau.fi/whatsmeow"
)

type Group struct {
	Service domainGroup.IGroupService
}

func InitRestGroup(app *fiber.App, service domainGroup.IGroupService) Group {
	rest := Group{Service: service}
	app.Post("/group", rest.CreateGroup)
	app.Post("/group/join-with-link", rest.JoinGroupWithLink)
	app.Post("/group/leave", rest.LeaveGroup)
	app.Post("/group/participants", rest.AddParticipants)
	app.Post("/group/participants/remove", rest.DeleteParticipants)
	app.Post("/group/participants/promote", rest.PromoteParticipants)
	app.Post("/group/participants/demote", rest.DemoteParticipants)
	return rest
}

func (controller *Group) JoinGroupWithLink(c *fiber.Ctx) error {
	var request domainGroup.JoinGroupWithLinkRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.JoinGroupWithLink(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success joined group",
		Results: map[string]string{
			"group_id": response,
		},
	})
}

func (controller *Group) LeaveGroup(c *fiber.Ctx) error {
	var request domainGroup.LeaveGroupRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.GroupID)

	err = controller.Service.LeaveGroup(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success leave group",
	})
}

func (controller *Group) CreateGroup(c *fiber.Ctx) error {
	var request domainGroup.CreateGroupRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	groupID, err := controller.Service.CreateGroup(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: fmt.Sprintf("Success created group with id %s", groupID),
		Results: map[string]string{
			"group_id": groupID,
		},
	})
}

func (controller *Group) AddParticipants(c *fiber.Ctx) error {
	var request domainGroup.ParticipantRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.GroupID)

	request.Action = whatsmeow.ParticipantChangeAdd

	result, err := controller.Service.ManageParticipant(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success add participants",
		Results: result,
	})
}

func (controller *Group) DeleteParticipants(c *fiber.Ctx) error {
	var request domainGroup.ParticipantRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.GroupID)

	request.Action = whatsmeow.ParticipantChangeRemove

	result, err := controller.Service.ManageParticipant(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success delete participants",
		Results: result,
	})
}

func (controller *Group) PromoteParticipants(c *fiber.Ctx) error {
	var request domainGroup.ParticipantRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.GroupID)

	request.Action = whatsmeow.ParticipantChangePromote

	result, err := controller.Service.ManageParticipant(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success promote participants",
		Results: result,
	})
}

func (controller *Group) DemoteParticipants(c *fiber.Ctx) error {
	var request domainGroup.ParticipantRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.GroupID)

	request.Action = whatsmeow.ParticipantChangeDemote

	result, err := controller.Service.ManageParticipant(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success demote participants",
		Results: result,
	})
}
