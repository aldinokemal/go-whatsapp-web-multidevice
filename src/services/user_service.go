package services

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/gofiber/fiber/v2"
)

type UserService interface {
	UserInfo(c *fiber.Ctx, request structs.UserInfoRequest) (response structs.UserInfoResponse, err error)
	UserAvatar(c *fiber.Ctx, request structs.UserAvatarRequest) (response structs.UserAvatarResponse, err error)
	UserMyListGroups(c *fiber.Ctx) (response structs.UserMyListGroupsResponse, err error)
	UserMyPrivacySetting(c *fiber.Ctx) (response structs.UserMyPrivacySettingResponse, err error)
}
