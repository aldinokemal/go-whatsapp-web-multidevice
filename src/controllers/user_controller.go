package controllers

import (
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/gofiber/fiber/v2"
)

type UserController struct {
	Service domainUser.IUserService
}

func NewUserController(service domainUser.IUserService) UserController {
	return UserController{Service: service}
}

func (controller *UserController) Route(app *fiber.App) {
	app.Get("/user/info", controller.UserInfo)
	app.Get("/user/avatar", controller.UserAvatar)
	app.Get("/user/my/privacy", controller.UserMyPrivacySetting)
	app.Get("/user/my/groups", controller.UserMyListGroups)
}

func (controller *UserController) UserInfo(c *fiber.Ctx) error {
	var request domainUser.InfoRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	validations.ValidateUserInfo(request)

	request.Phone = request.Phone + "@s.whatsapp.net"
	response, err := controller.Service.Info(c.Context(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success",
		Results: response.Data[0],
	})
}

func (controller *UserController) UserAvatar(c *fiber.Ctx) error {
	var request domainUser.AvatarRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	validations.ValidateUserAvatar(request)

	request.Phone = request.Phone + "@s.whatsapp.net"
	response, err := controller.Service.Avatar(c.Context(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success get avatar",
		Results: response,
	})
}

func (controller *UserController) UserMyPrivacySetting(c *fiber.Ctx) error {
	response, err := controller.Service.MyPrivacySetting(c.Context())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success get privacy",
		Results: response,
	})
}

func (controller *UserController) UserMyListGroups(c *fiber.Ctx) error {
	response, err := controller.Service.MyListGroups(c.Context())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success get list groups",
		Results: response,
	})
}
