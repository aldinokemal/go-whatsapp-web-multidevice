package services

import (
	"context"
	"errors"
	"fmt"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"time"
)

type userService struct {
	WaCli *whatsmeow.Client
}

func NewUserService(waCli *whatsmeow.Client) domainUser.IUserService {
	return &userService{
		WaCli: waCli,
	}
}

func (service userService) Info(ctx context.Context, request domainUser.InfoRequest) (response domainUser.InfoResponse, err error) {
	err = validations.ValidateUserInfo(ctx, request)
	if err != nil {
		return response, err
	}
	var jids []types.JID
	dataWaRecipient, err := whatsapp.ValidateJidWithLogin(service.WaCli, request.Phone)
	if err != nil {
		return response, err
	}

	jids = append(jids, dataWaRecipient)
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
				Device: whatsapp.GetPlatformName(int(j.Device)),
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

func (service userService) Avatar(ctx context.Context, request domainUser.AvatarRequest) (response domainUser.AvatarResponse, err error) {

	chanResp := make(chan domainUser.AvatarResponse)
	chanErr := make(chan error)
	waktu := time.Now()

	go func() {
		err = validations.ValidateUserAvatar(ctx, request)
		if err != nil {
			chanErr <- err
		}
		dataWaRecipient, err := whatsapp.ValidateJidWithLogin(service.WaCli, request.Phone)
		if err != nil {
			chanErr <- err
		}
		pic, err := service.WaCli.GetProfilePictureInfo(dataWaRecipient, &whatsmeow.GetProfilePictureParams{
			Preview:     request.IsPreview,
			IsCommunity: request.IsCommunity,
		})
		if err != nil {
			chanErr <- err
		} else if pic == nil {
			chanErr <- errors.New("no avatar found")
		} else {
			response.URL = pic.URL
			response.ID = pic.ID
			response.Type = pic.Type

			chanResp <- response
		}
	}()

	for {
		select {
		case err := <-chanErr:
			return response, err
		case response := <-chanResp:
			return response, nil
		default:
			if waktu.Add(2 * time.Second).Before(time.Now()) {
				return response, pkgError.ContextError("Error timeout get avatar !")
			}
		}
	}

}

func (service userService) MyListGroups(_ context.Context) (response domainUser.MyListGroupsResponse, err error) {
	whatsapp.MustLogin(service.WaCli)

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
	whatsapp.MustLogin(service.WaCli)

	resp, err := service.WaCli.TryFetchPrivacySettings(true)
	if err != nil {
		return
	}

	response.GroupAdd = string(resp.GroupAdd)
	response.Status = string(resp.Status)
	response.ReadReceipts = string(resp.ReadReceipts)
	response.Profile = string(resp.Profile)
	return response, nil
}
