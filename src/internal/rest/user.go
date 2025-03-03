package rest

import (
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"github.com/gofiber/fiber/v2"
)

type User struct {
	Service domainUser.IUserService
}

func InitRestUser(app *fiber.App, service domainUser.IUserService) User {
	rest := User{Service: service}
	app.Get("/user/info", rest.UserInfo)
	app.Get("/user/avatar", rest.UserAvatar)
	app.Post("/user/avatar", rest.UserChangeAvatar)
	app.Get("/user/my/privacy", rest.UserMyPrivacySetting)
	app.Get("/user/my/groups", rest.UserMyListGroups)
	app.Get("/user/my/newsletters", rest.UserMyListNewsletter)
	app.Get("/user/my/contacts", rest.UserMyListContacts)

	return rest
}

func (controller *User) UserInfo(c *fiber.Ctx) error {
	var request domainUser.InfoRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.Info(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get user info",
		Results: response.Data[0],
	})
}

func (controller *User) UserAvatar(c *fiber.Ctx) error {
	var request domainUser.AvatarRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	whatsapp.SanitizePhone(&request.Phone)

	response, err := controller.Service.Avatar(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get avatar",
		Results: response,
	})
}

func (controller *User) UserChangeAvatar(c *fiber.Ctx) error {
	var request domainUser.ChangeAvatarRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.Avatar, err = c.FormFile("avatar")
	utils.PanicIfNeeded(err)

	err = controller.Service.ChangeAvatar(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success change avatar",
	})
}

func (controller *User) UserMyPrivacySetting(c *fiber.Ctx) error {
	response, err := controller.Service.MyPrivacySetting(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get privacy",
		Results: response,
	})
}

func (controller *User) UserMyListGroups(c *fiber.Ctx) error {
	response, err := controller.Service.MyListGroups(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list groups",
		Results: response,
	})
}

func (controller *User) UserMyListNewsletter(c *fiber.Ctx) error {
	response, err := controller.Service.MyListNewsletter(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list newsletter",
		Results: response,
	})
}

func (controller *User) UserMyListContacts(c *fiber.Ctx) error {
	response, err := controller.Service.MyListContacts(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list contacts",
		Results: response,
	})
}
