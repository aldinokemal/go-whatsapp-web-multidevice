package services

import (
	"context"
	"errors"
	"fmt"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type userService struct {
	WaCli *whatsmeow.Client
}

func NewUserService(waCli *whatsmeow.Client) domainUser.IUserService {
	return &userService{
		WaCli: waCli,
	}
}

func (service userService) Info(_ context.Context, request domainUser.InfoRequest) (response domainUser.InfoResponse, err error) {
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
		var device []domainUser.InfoResponseDataDevice
		for _, j := range userInfo.Devices {
			device = append(device, domainUser.InfoResponseDataDevice{
				User:   j.User,
				Agent:  j.Agent,
				Device: utils.GetPlatformName(int(j.Device)),
				Server: j.Server,
				AD:     j.AD,
			})
		}

		data := domainUser.InfoResponseData{
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

func (service userService) Avatar(_ context.Context, request domainUser.AvatarRequest) (response domainUser.AvatarResponse, err error) {
	utils.MustLogin(service.WaCli)

	jid, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}
	pic, err := service.WaCli.GetProfilePictureInfo(jid, false, "")
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

func (service userService) MyListGroups(_ context.Context) (response domainUser.MyListGroupsResponse, err error) {
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

func (service userService) MyPrivacySetting(_ context.Context) (response domainUser.MyPrivacySettingResponse, err error) {
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
