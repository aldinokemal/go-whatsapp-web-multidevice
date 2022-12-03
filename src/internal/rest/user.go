package rest

import (
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/gofiber/fiber/v2"
)

type User struct {
	Service domainUser.IUserService
}

func InitRestUser(app *fiber.App, service domainUser.IUserService) User {
	rest := User{Service: service}
	app.Get("/user/info", rest.UserInfo)
	app.Get("/user/avatar", rest.UserAvatar)
	app.Get("/user/my/privacy", rest.UserMyPrivacySetting)
	app.Get("/user/my/groups", rest.UserMyListGroups)

	return rest
}

func (controller *User) Route(app *fiber.App) {
	app.Get("/user/info", controller.UserInfo)
	app.Get("/user/avatar", controller.UserAvatar)
	app.Get("/user/my/privacy", controller.UserMyPrivacySetting)
	app.Get("/user/my/groups", controller.UserMyListGroups)
}

func (controller *User) UserInfo(c *fiber.Ctx) error {
	var request domainUser.InfoRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	whatsapp.SanitizePhone(&request.Phone)
	err = validations.ValidateUserInfo(request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.Info(c.Context(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Message: "Success",
		Results: response.Data[0],
	})
}

func (controller *User) UserAvatar(c *fiber.Ctx) error {
	var request domainUser.AvatarRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	whatsapp.SanitizePhone(&request.Phone)
	err = validations.ValidateUserAvatar(request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.Avatar(c.Context(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Message: "Success get avatar",
		Results: response,
	})
}

func (controller *User) UserMyPrivacySetting(c *fiber.Ctx) error {
	response, err := controller.Service.MyPrivacySetting(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Message: "Success get privacy",
		Results: response,
	})
}

func (controller *User) UserMyListGroups(c *fiber.Ctx) error {
	response, err := controller.Service.MyListGroups(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Message: "Success get list groups",
		Results: response,
	})
}
