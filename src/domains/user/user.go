package user

import (
	"github.com/gofiber/fiber/v2"
)

type IUserService interface {
	Info(c *fiber.Ctx, request InfoRequest) (response InfoResponse, err error)
	Avatar(c *fiber.Ctx, request AvatarRequest) (response AvatarResponse, err error)
	MyListGroups(c *fiber.Ctx) (response MyListGroupsResponse, err error)
	MyPrivacySetting(c *fiber.Ctx) (response MyPrivacySettingResponse, err error)
}
