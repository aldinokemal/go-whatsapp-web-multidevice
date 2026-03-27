package rest

import (
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
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

	ctx := whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))

	response, err := controller.Service.Info(ctx, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get user info",
		Results: response,
	})
}

func (controller *User) UserAvatar(c *fiber.Ctx) error {
	var request domainUser.AvatarRequest
	err := c.QueryParser(&request)
	utils.PanicIfNeeded(err)

	utils.SanitizePhone(&request.Phone)

	ctx := whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))

	response, err := controller.Service.Avatar(ctx, request)
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

	ctx := whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))

	err = controller.Service.ChangeAvatar(ctx, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success change avatar",
	})
}

func (controller *User) UserMyPrivacySetting(c *fiber.Ctx) error {
	ctx := whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))
	response, err := controller.Service.MyPrivacySetting(ctx)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get privacy",
		Results: response,
	})
}

func (controller *User) UserMyListGroups(c *fiber.Ctx) error {
	deviceVal := c.Locals("device")
	ctx := c.UserContext()
	if device, ok := deviceVal.(*whatsapp.DeviceInstance); ok {
		ctx = whatsapp.ContextWithDevice(ctx, device)
	}

	response, err := controller.Service.MyListGroups(ctx)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list groups",
		Results: response,
	})
}

func (controller *User) UserMyListNewsletter(c *fiber.Ctx) error {
	ctx := whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))
	response, err := controller.Service.MyListNewsletter(ctx)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get list newsletter",
		Results: response,
	})
}

func (controller *User) UserMyListContacts(c *fiber.Ctx) error {
	ctx := whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))
	response, err := controller.Service.MyListContacts(ctx)
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

	ctx := whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))

	response, err := controller.Service.BusinessProfile(ctx, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get business profile",
		Results: response,
	})
}

func getDeviceFromCtx(c *fiber.Ctx) *whatsapp.DeviceInstance {
	if c == nil {
		return nil
	}
	if device, ok := c.Locals("device").(*whatsapp.DeviceInstance); ok {
		return device
	}
	return nil
}
