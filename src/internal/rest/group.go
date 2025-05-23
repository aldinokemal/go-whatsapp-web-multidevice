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
	app.Get("/group/participant-requests", rest.ListParticipantRequests)
	app.Post("/group/participant-requests/approve", rest.ApproveParticipantRequests)
	app.Post("/group/participant-requests/reject", rest.RejectParticipantRequests)
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
	return controller.manageParticipants(c, whatsmeow.ParticipantChangeAdd, "Success add participants")
}

func (controller *Group) DeleteParticipants(c *fiber.Ctx) error {
	return controller.manageParticipants(c, whatsmeow.ParticipantChangeRemove, "Success delete participants")
}

func (controller *Group) PromoteParticipants(c *fiber.Ctx) error {
	return controller.manageParticipants(c, whatsmeow.ParticipantChangePromote, "Success promote participants")
}

func (controller *Group) DemoteParticipants(c *fiber.Ctx) error {
	return controller.manageParticipants(c, whatsmeow.ParticipantChangeDemote, "Success demote participants")
}

func (controller *Group) ListParticipantRequests(c *fiber.Ctx) error {
	var request domainGroup.GetGroupRequestParticipantsRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	if request.GroupID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_GROUP_ID",
			Message: "Group ID cannot be empty",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	result, err := controller.Service.GetGroupRequestParticipants(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success getting list requested participants",
		Results: result,
	})
}

func (controller *Group) ApproveParticipantRequests(c *fiber.Ctx) error {
	return controller.handleRequestedParticipants(c, whatsmeow.ParticipantChangeApprove, "Success approve requested participants")
}

func (controller *Group) RejectParticipantRequests(c *fiber.Ctx) error {
	return controller.handleRequestedParticipants(c, whatsmeow.ParticipantChangeReject, "Success reject requested participants")
}

// Generalized participant management handler
func (controller *Group) manageParticipants(c *fiber.Ctx, action whatsmeow.ParticipantChange, successMsg string) error {
	var request domainGroup.ParticipantRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)
	whatsapp.SanitizePhone(&request.GroupID)
	request.Action = action
	result, err := controller.Service.ManageParticipant(c.UserContext(), request)
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: successMsg,
		Results: result,
	})
}

// Generalized requested participants handler
func (controller *Group) handleRequestedParticipants(c *fiber.Ctx, action whatsmeow.ParticipantRequestChange, successMsg string) error {
	var request domainGroup.GroupRequestParticipantsRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)
	whatsapp.SanitizePhone(&request.GroupID)
	request.Action = action
	result, err := controller.Service.ManageGroupRequestParticipants(c.UserContext(), request)
	utils.PanicIfNeeded(err)
	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: successMsg,
		Results: result,
	})
}
