package controllers

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/gofiber/fiber/v2"
)

type UserController struct {
	Service services.UserService
}

func NewUserController(service services.UserService) UserController {
	return UserController{Service: service}
}

func (controller *UserController) Route(app *fiber.App) {
	app.Get("/user/info", controller.UserInfo)
	app.Get("/user/avatar", controller.UserAvatar)
	app.Get("/user/my/privacy", controller.UserMyPrivacySetting)
	app.Get("/user/my/groups", controller.UserMyListGroups)
}

func (controller *UserController) UserInfo(c *fiber.Ctx) error {
	var request structs.UserInfoRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	validations.ValidateUserInfo(request)

	request.Phone = request.Phone + "@s.whatsapp.net"
	response, err := controller.Service.UserInfo(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success",
		Results: response.Data[0],
	})
}

func (controller *UserController) UserAvatar(c *fiber.Ctx) error {
	var request structs.UserAvatarRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	validations.ValidateUserAvatar(request)

	request.Phone = request.Phone + "@s.whatsapp.net"
	response, err := controller.Service.UserAvatar(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success get avatar",
		Results: response,
	})
}

func (controller *UserController) UserMyPrivacySetting(c *fiber.Ctx) error {
	response, err := controller.Service.UserMyPrivacySetting(c)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success get privacy",
		Results: response,
	})
}

func (controller *UserController) UserMyListGroups(c *fiber.Ctx) error {
	response, err := controller.Service.UserMyListGroups(c)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success get list groups",
		Results: response,
	})
}
