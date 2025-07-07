package rest

import (
	"fmt"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type User struct {
	Service domainUser.IUserUsecase
}

func InitRestUser(app *fiber.App, service domainUser.IUserUsecase) User {
	rest := User{Service: service}
	app.Get("/user/info", rest.UserInfo)
	app.Get("/user/avatar", rest.UserAvatar)
	app.Post("/user/avatar", rest.UserChangeAvatar)
	app.Post("/user/pushname", rest.UserChangePushName)
	app.Get("/user/my/privacy", rest.UserMyPrivacySetting)
	app.Get("/user/my/groups", rest.UserMyListGroups)
	app.Get("/user/my/newsletters", rest.UserMyListNewsletter)
	app.Get("/user/my/contacts", rest.UserMyListContacts)
	app.Get("/user/check", rest.UserCheck)

	return rest
}

func (controller *User) UserInfo(c *fiber.Ctx) error {
	var request domainUser.InfoRequest
	if err := c.QueryParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse query parameters",
		})
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.Info(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "USER_INFO_FAILED",
			Message: fmt.Sprintf("Failed to get user info: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get user info",
		Results: response.Data[0],
	})
}

func (controller *User) UserAvatar(c *fiber.Ctx) error {
	var request domainUser.AvatarRequest
	if err := c.QueryParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse query parameters",
		})
	}

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.Avatar(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "USER_AVATAR_FAILED",
			Message: fmt.Sprintf("Failed to get user avatar: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get avatar",
		Results: response,
	})
}

func (controller *User) UserChangeAvatar(c *fiber.Ctx) error {
	var request domainUser.ChangeAvatarRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	avatar, err := c.FormFile("avatar")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "AVATAR_REQUIRED",
			Message: "Avatar file is required",
		})
	}
	request.Avatar = avatar

	if err := controller.Service.ChangeAvatar(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "CHANGE_AVATAR_FAILED",
			Message: fmt.Sprintf("Failed to change avatar: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success change avatar",
	})
}

func (controller *User) UserMyPrivacySetting(c *fiber.Ctx) error {
	response, err := controller.Service.MyPrivacySetting(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "PRIVACY_SETTINGS_FAILED",
			Message: fmt.Sprintf("Failed to get privacy settings: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get privacy",
		Results: response,
	})
}

func (controller *User) UserMyListGroups(c *fiber.Ctx) error {
	response, err := controller.Service.MyListGroups(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "LIST_GROUPS_FAILED",
			Message: fmt.Sprintf("Failed to get groups list: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list groups",
		Results: response,
	})
}

func (controller *User) UserMyListNewsletter(c *fiber.Ctx) error {
	response, err := controller.Service.MyListNewsletter(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "LIST_NEWSLETTER_FAILED",
			Message: fmt.Sprintf("Failed to get newsletter list: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list newsletter",
		Results: response,
	})
}

func (controller *User) UserMyListContacts(c *fiber.Ctx) error {
	response, err := controller.Service.MyListContacts(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "LIST_CONTACTS_FAILED",
			Message: fmt.Sprintf("Failed to get contacts list: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list contacts",
		Results: response,
	})
}

func (controller *User) UserChangePushName(c *fiber.Ctx) error {
	var request domainUser.ChangePushNameRequest
	if err := c.BodyParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse request body",
		})
	}

	if err := controller.Service.ChangePushName(c.UserContext(), request); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "CHANGE_PUSHNAME_FAILED",
			Message: fmt.Sprintf("Failed to change push name: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success change push name",
	})
}

func (controller *User) UserCheck(c *fiber.Ctx) error {
	var request domainUser.CheckRequest
	if err := c.QueryParser(&request); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "INVALID_REQUEST",
			Message: "Failed to parse query parameters",
		})
	}

	response, err := controller.Service.IsOnWhatsApp(c.UserContext(), request)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "USER_CHECK_FAILED",
			Message: fmt.Sprintf("Failed to check user: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success check user",
		Results: response,
	})
}
