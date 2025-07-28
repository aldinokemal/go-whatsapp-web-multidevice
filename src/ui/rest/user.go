package rest

import (
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type User struct {
	Service domainUser.IUserUsecase
}

func InitRestUser(app fiber.Router, service domainUser.IUserUsecase) User {
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
	app.Get("/user/business-profile", rest.UserBusinessProfile)

	return rest
}

func (controller *User) UserInfo(c *fiber.Ctx) error {
	var request domainUser.InfoRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

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

	utils.SanitizePhone(&request.Phone)

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

func (controller *User) UserChangePushName(c *fiber.Ctx) error {
	var request domainUser.ChangePushNameRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	err = controller.Service.ChangePushName(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success change push name",
	})
}

func (controller *User) UserCheck(c *fiber.Ctx) error {
	var request domainUser.CheckRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.IsOnWhatsApp(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success check user",
		Results: response,
	})
}

func (controller *User) UserBusinessProfile(c *fiber.Ctx) error {
	var request domainUser.BusinessProfileRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.BusinessProfile(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get business profile",
		Results: response,
	})
}
