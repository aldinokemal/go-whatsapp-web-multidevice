package rest

import (
	"fmt"

	"github.com/sirupsen/logrus"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"go.mau.fi/whatsmeow"
)

type Group struct {
	Service domainGroup.IGroupUsecase
}

func InitRestGroup(app *fiber.App, service domainGroup.IGroupUsecase) Group {
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
	app.Post("/group/photo", rest.SetGroupPhoto)
	app.Post("/group/name", rest.SetGroupName)
	app.Post("/group/locked", rest.SetGroupLocked)
	app.Post("/group/announce", rest.SetGroupAnnounce)
	app.Post("/group/topic", rest.SetGroupTopic)
	return rest
}

func (controller *Group) JoinGroupWithLink(c *fiber.Ctx) error {
	var request domainGroup.JoinGroupWithLinkRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	response, err := controller.Service.JoinGroupWithLink(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "JOIN_GROUP_FAILED",
			Message: fmt.Sprintf("Failed to join group: %v", err),
		})
	}

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
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	if err := controller.Service.LeaveGroup(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "LEAVE_GROUP_FAILED",
			Message: fmt.Sprintf("Failed to leave group: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success leave group",
	})
}

func (controller *Group) CreateGroup(c *fiber.Ctx) error {
	var request domainGroup.CreateGroupRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	groupID, err := controller.Service.CreateGroup(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "CREATE_GROUP_FAILED",
			Message: fmt.Sprintf("Failed to create group: %v", err),
		})
	}

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
	if err := c.QueryParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse query parameters",
		})
	}

	if request.GroupID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_GROUP_ID",
			Message: "Group ID cannot be empty",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	result, err := controller.Service.GetGroupRequestParticipants(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "GET_PARTICIPANTS_FAILED",
			Message: fmt.Sprintf("Failed to get participant requests: %v", err),
		})
	}

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
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)
	request.Action = action

	result, err := controller.Service.ManageParticipant(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "MANAGE_PARTICIPANTS_FAILED",
			Message: fmt.Sprintf("Failed to manage participants: %v", err),
		})
	}

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
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)
	request.Action = action

	result, err := controller.Service.ManageGroupRequestParticipants(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "HANDLE_PARTICIPANT_REQUESTS_FAILED",
			Message: fmt.Sprintf("Failed to handle participant requests: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: successMsg,
		Results: result,
	})
}

func (controller *Group) SetGroupPhoto(c *fiber.Ctx) error {
	var request domainGroup.SetGroupPhotoRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	file, err := c.FormFile("photo")
	if err == nil {
		logrus.Printf("INFO: Received group photo - Filename: %s, Size: %d bytes, ContentType: %s",
			file.Filename, file.Size, file.Header.Get("Content-Type"))

		// Basic validation only - processing will be done in usecase
		if err := utils.ValidateGroupPhotoFormat(file); err != nil {
			logrus.Printf("ERROR: Group photo validation failed - %v", err)
			return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
				Status:  400,
				Code:    "INVALID_IMAGE_FORMAT",
				Message: fmt.Sprintf("Image validation failed: %v", err),
			})
		}

		request.Photo = file
	} else {
		logrus.Printf("DEBUG: No photo file provided - Error: %v", err)
	}

	pictureID, err := controller.Service.SetGroupPhoto(c.UserContext(), request)
	if err != nil {
		logrus.Printf("ERROR: WhatsApp service failed to set group photo - %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SET_GROUP_PHOTO_FAILED",
			Message: fmt.Sprintf("Failed to set group photo: %v", err),
		})
	}

	message := "Success update group photo"
	if request.Photo == nil {
		message = "Success remove group photo"
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: message,
		Results: domainGroup.SetGroupPhotoResponse{
			PictureID: pictureID,
			Message:   message,
		},
	})
}

func (controller *Group) SetGroupName(c *fiber.Ctx) error {
	var request domainGroup.SetGroupNameRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	if err := controller.Service.SetGroupName(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SET_GROUP_NAME_FAILED",
			Message: fmt.Sprintf("Failed to set group name: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: fmt.Sprintf("Success update group name to '%s'", request.Name),
	})
}

func (controller *Group) SetGroupLocked(c *fiber.Ctx) error {
	var request domainGroup.SetGroupLockedRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	if err := controller.Service.SetGroupLocked(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SET_GROUP_LOCKED_FAILED",
			Message: fmt.Sprintf("Failed to set group locked status: %v", err),
		})
	}

	message := "Success set group as unlocked"
	if request.Locked {
		message = "Success set group as locked"
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: message,
	})
}

func (controller *Group) SetGroupAnnounce(c *fiber.Ctx) error {
	var request domainGroup.SetGroupAnnounceRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	if err := controller.Service.SetGroupAnnounce(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SET_GROUP_ANNOUNCE_FAILED",
			Message: fmt.Sprintf("Failed to set group announce mode: %v", err),
		})
	}

	message := "Success disable announce mode"
	if request.Announce {
		message = "Success enable announce mode"
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: message,
	})
}

func (controller *Group) SetGroupTopic(c *fiber.Ctx) error {
	var request domainGroup.SetGroupTopicRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	whatsapp.SanitizePhone(&request.GroupID)

	if err := controller.Service.SetGroupTopic(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "SET_GROUP_TOPIC_FAILED",
			Message: fmt.Sprintf("Failed to set group topic: %v", err),
		})
	}

	message := "Success update group topic"
	if request.Topic == "" {
		message = "Success remove group topic"
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: message,
	})
}
