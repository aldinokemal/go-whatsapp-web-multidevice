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
	if !service.WaCli.IsLoggedIn() {
		panic(utils.AuthError{Message: "you are not loggin"})
	}
	var jids []types.JID
	jid, ok := utils.ParseJID(request.PhoneNumber)
	if !ok {
		return response, errors.New("invalid JID " + request.PhoneNumber)
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
				Device: j.Device,
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
	if !service.WaCli.IsLoggedIn() {
		panic(utils.AuthError{Message: "you are not loggin"})
	}
	jid, ok := utils.ParseJID(request.PhoneNumber)
	if !ok {
		return response, errors.New("invalid JID " + request.PhoneNumber)
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
