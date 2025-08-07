package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"time"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/disintegration/imaging"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

type serviceUser struct {
	// Remove the WaCli field - we'll use the global client instead
}

func NewUserService() domainUser.IUserUsecase {
	return &serviceUser{}
}

func (service serviceUser) Info(ctx context.Context, request domainUser.InfoRequest) (response domainUser.InfoResponse, err error) {
	err = validations.ValidateUserInfo(ctx, request)
	if err != nil {
		return response, err
	}
	var jids []types.JID
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	jids = append(jids, dataWaRecipient)
	resp, err := whatsapp.GetClient().GetUserInfo(jids)
	if err != nil {
		return response, err
	}

	for _, userInfo := range resp {
		var device []domainUser.InfoResponseDataDevice
		for _, j := range userInfo.Devices {
			device = append(device, domainUser.InfoResponseDataDevice{
				User:   j.User,
				Agent:  j.RawAgent,
				Device: utils.GetPlatformName(int(j.Device)),
				Server: j.Server,
				AD:     j.ADString(),
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

func (service serviceUser) Avatar(ctx context.Context, request domainUser.AvatarRequest) (response domainUser.AvatarResponse, err error) {

	chanResp := make(chan domainUser.AvatarResponse)
	chanErr := make(chan error)
	waktu := time.Now()

	go func() {
		err = validations.ValidateUserAvatar(ctx, request)
		if err != nil {
			chanErr <- err
		}
		dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
		if err != nil {
			chanErr <- err
		}
		pic, err := whatsapp.GetClient().GetProfilePictureInfo(dataWaRecipient, &whatsmeow.GetProfilePictureParams{
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

func (service serviceUser) MyListGroups(_ context.Context) (response domainUser.MyListGroupsResponse, err error) {
	utils.MustLogin(whatsapp.GetClient())

	groups, err := whatsapp.GetClient().GetJoinedGroups()
	if err != nil {
		return
	}

	for _, group := range groups {
		response.Data = append(response.Data, *group)
	}
	return response, nil
}

func (service serviceUser) MyListNewsletter(_ context.Context) (response domainUser.MyListNewsletterResponse, err error) {
	utils.MustLogin(whatsapp.GetClient())

	datas, err := whatsapp.GetClient().GetSubscribedNewsletters()
	if err != nil {
		return
	}

	for _, data := range datas {
		response.Data = append(response.Data, *data)
	}
	return response, nil
}

func (service serviceUser) MyPrivacySetting(ctx context.Context) (response domainUser.MyPrivacySettingResponse, err error) {
	utils.MustLogin(whatsapp.GetClient())

	resp, err := whatsapp.GetClient().TryFetchPrivacySettings(ctx, true)
	if err != nil {
		return
	}

	response.GroupAdd = string(resp.GroupAdd)
	response.Status = string(resp.Status)
	response.ReadReceipts = string(resp.ReadReceipts)
	response.Profile = string(resp.Profile)
	return response, nil
}

func (service serviceUser) MyListContacts(ctx context.Context) (response domainUser.MyListContactsResponse, err error) {
	utils.MustLogin(whatsapp.GetClient())

	contacts, err := whatsapp.GetClient().Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return
	}

	for jid, contact := range contacts {
		response.Data = append(response.Data, domainUser.MyListContactsResponseData{
			JID:  jid,
			Name: contact.FullName,
		})
	}

	return response, nil
}

func (service serviceUser) ChangeAvatar(ctx context.Context, request domainUser.ChangeAvatarRequest) (err error) {
	utils.MustLogin(whatsapp.GetClient())

	file, err := request.Avatar.Open()
	if err != nil {
		return err
	}
	defer file.Close()

	// Read original image
	srcImage, err := imaging.Decode(file)
	if err != nil {
		return fmt.Errorf("failed to decode image: %v", err)
	}

	// Get original dimensions
	bounds := srcImage.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Calculate new dimensions for 1:1 aspect ratio
	size := width
	if height < width {
		size = height
	}
	if size > 640 {
		size = 640
	}

	// Create a square crop from the center
	left := (width - size) / 2
	top := (height - size) / 2
	croppedImage := imaging.Crop(srcImage, image.Rect(left, top, left+size, top+size))

	// Resize if needed
	if size > 640 {
		croppedImage = imaging.Resize(croppedImage, 640, 640, imaging.Lanczos)
	}

	// Convert to bytes
	var buf bytes.Buffer
	err = imaging.Encode(&buf, croppedImage, imaging.JPEG, imaging.JPEGQuality(80))
	if err != nil {
		return fmt.Errorf("failed to encode image: %v", err)
	}

	_, err = whatsapp.GetClient().SetGroupPhoto(types.JID{}, buf.Bytes())
	if err != nil {
		return err
	}

	return nil
}

func (service serviceUser) ChangePushName(ctx context.Context, request domainUser.ChangePushNameRequest) (err error) {
	utils.MustLogin(whatsapp.GetClient())

	err = whatsapp.GetClient().SendAppState(ctx, appstate.BuildSettingPushName(request.PushName))
	if err != nil {
		return err
	}
	return nil
}

func (service serviceUser) IsOnWhatsApp(ctx context.Context, request domainUser.CheckRequest) (response domainUser.CheckResponse, err error) {
	utils.MustLogin(whatsapp.GetClient())

	utils.SanitizePhone(&request.Phone)

	response.IsOnWhatsApp = utils.IsOnWhatsapp(whatsapp.GetClient(), request.Phone)

	return response, nil
}

func (service serviceUser) BusinessProfile(ctx context.Context, request domainUser.BusinessProfileRequest) (response domainUser.BusinessProfileResponse, err error) {
	err = validations.ValidateBusinessProfile(ctx, request)
	if err != nil {
		return response, err
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	profile, err := whatsapp.GetClient().GetBusinessProfile(dataWaRecipient)
	if err != nil {
		return response, err
	}

	// Convert profile to response format
	response.JID = dataWaRecipient.String()
	response.Email = profile.Email
	response.Address = profile.Address

	// Convert categories
	for _, category := range profile.Categories {
		response.Categories = append(response.Categories, domainUser.BusinessProfileCategory{
			ID:   category.ID,
			Name: category.Name,
		})
	}

	// Convert profile options
	if profile.ProfileOptions != nil {
		response.ProfileOptions = make(map[string]string)
		for key, value := range profile.ProfileOptions {
			response.ProfileOptions[key] = value
		}
	}

	response.BusinessHoursTimeZone = profile.BusinessHoursTimeZone

	// Convert business hours
	for _, hours := range profile.BusinessHours {
		response.BusinessHours = append(response.BusinessHours, domainUser.BusinessProfileHoursConfig{
			DayOfWeek: hours.DayOfWeek,
			Mode:      hours.Mode,
			OpenTime:  utils.FormatBusinessHourTime(hours.OpenTime),
			CloseTime: utils.FormatBusinessHourTime(hours.CloseTime),
		})
	}

	return response, nil
}

func (service serviceUser) MyGroupsMetadata(ctx context.Context) (response domainUser.MyGroupsMetadataResponse, err error) {
	utils.MustLogin(whatsapp.GetClient())

	groups, err := whatsapp.GetClient().GetJoinedGroups()
	if err != nil {
		return
	}

	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	results := make([]domainUser.GroupMeta, len(groups))

	type groupWork struct {
		index int
		group *types.GroupInfo
	}

	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	work := make(chan groupWork, len(groups))
	done := make(chan error, maxParallel)

	for i := 0; i < maxParallel; i++ {
		go func() {
			for w := range work {
				select {
				case <-ctx.Done():
					done <- ctx.Err()
					return
				default:
				}

				sem <- struct{}{}

				_, cancelGroup := context.WithTimeout(ctx, 3*time.Second)
				info, err := whatsapp.GetClient().GetGroupInfo(w.group.JID)
				cancelGroup()

				if err != nil {
					<-sem
					done <- err
					return
				}

				var admins, members []string
				isAdmin := false
				currentUserJID := whatsapp.GetClient().Store.ID.User

				for _, p := range info.Participants {
					members = append(members, p.JID.String())
					if p.IsAdmin {
						admins = append(admins, p.JID.String())
						if p.JID.User == currentUserJID {
							isAdmin = true
						}
					}
				}

				var link string
				if isAdmin {
					link, _ = whatsapp.GetClient().GetGroupInviteLink(info.JID, false)
				}

				results[w.index] = domainUser.GroupMeta{
					GroupID:     info.JID.String(),
					Name:        info.Name,
					IsAdmin:     isAdmin,
					AdminCount:  len(admins),
					MemberCount: len(members),
					InviteLink:  link,
					Locked:      info.IsLocked,
					Announce:    info.IsAnnounce,
					Ephemeral:   info.IsEphemeral,
				}

				<-sem
				done <- nil
			}
		}()
	}

	for i, group := range groups {
		work <- groupWork{index: i, group: group}
	}
	close(work)

	for i := 0; i < len(groups); i++ {
		if err := <-done; err != nil {
			return response, err
		}
	}

	response.Data = results
	return response, nil
}
