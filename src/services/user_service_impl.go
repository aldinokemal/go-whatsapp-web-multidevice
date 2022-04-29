package services

import (
	"errors"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type UserServiceImpl struct {
	WaCli *whatsmeow.Client
}

func NewUserService(waCli *whatsmeow.Client) UserService {
	return &UserServiceImpl{
		WaCli: waCli,
	}
}

func (service UserServiceImpl) UserInfo(_ *fiber.Ctx, request structs.UserInfoRequest) (response structs.UserInfoResponse, err error) {
	utils.MustLogin(service.WaCli)

	var jids []types.JID
	jid, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}

	jids = append(jids, jid)
	resp, err := service.WaCli.GetUserInfo(jids)
	if err != nil {
		return response, err
	}

	for _, userInfo := range resp {
		var device []structs.UserInfoResponseDataDevice
		for _, j := range userInfo.Devices {
			device = append(device, structs.UserInfoResponseDataDevice{
				User:   j.User,
				Agent:  j.Agent,
				Device: utils.GetPlatformName(int(j.Device)),
				Server: j.Server,
				AD:     j.AD,
			})
		}

		data := structs.UserInfoResponseData{
			Status:    userInfo.Status,
			PictureID: userInfo.PictureID,
			Devices:   device,
		}
		if userInfo.VerifiedName != nil {
			data.VerifiedName = fmt.Sprintf("%v", *userInfo.VerifiedName)
		}
		response.Data = append(response.Data, data)
	}

	return response, nil
}

func (service UserServiceImpl) UserAvatar(_ *fiber.Ctx, request structs.UserAvatarRequest) (response structs.UserAvatarResponse, err error) {
	utils.MustLogin(service.WaCli)

	jid, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}
	pic, err := service.WaCli.GetProfilePictureInfo(jid, false)
	if err != nil {
		return response, err
	} else if pic == nil {
		return response, errors.New("no avatar found")
	} else {
		response.URL = pic.URL
		response.ID = pic.ID
		response.Type = pic.Type

		return response, nil
	}
}

func (service UserServiceImpl) UserMyListGroups(_ *fiber.Ctx) (response structs.UserMyListGroupsResponse, err error) {
	utils.MustLogin(service.WaCli)

	groups, err := service.WaCli.GetJoinedGroups()
	if err != nil {
		return
	}
	fmt.Printf("%+v\n", groups)
	if groups != nil {
		for _, group := range groups {
			response.Data = append(response.Data, *group)
		}
	}
	return response, nil
}

func (service UserServiceImpl) UserMyPrivacySetting(_ *fiber.Ctx) (response structs.UserMyPrivacySettingResponse, err error) {
	utils.MustLogin(service.WaCli)

	resp, err := service.WaCli.TryFetchPrivacySettings(false)
	if err != nil {
		return
	}

	response.GroupAdd = string(resp.GroupAdd)
	response.Status = string(resp.Status)
	response.ReadReceipts = string(resp.ReadReceipts)
	response.Profile = string(resp.Profile)
	return response, nil
}
