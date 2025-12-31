package usecase

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
)

type serviceGroup struct{}

func NewGroupService() domainGroup.IGroupUsecase {
	return &serviceGroup{}
}

func (service serviceGroup) JoinGroupWithLink(ctx context.Context, request domainGroup.JoinGroupWithLinkRequest) (groupID string, err error) {
	if err = validations.ValidateJoinGroupWithLink(ctx, request); err != nil {
		return groupID, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return groupID, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	jid, err := client.JoinGroupWithLink(ctx, request.Link)
	if err != nil {
		return
	}
	return jid.String(), nil
}

func (service serviceGroup) LeaveGroup(ctx context.Context, request domainGroup.LeaveGroupRequest) (err error) {
	if err = validations.ValidateLeaveGroup(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	JID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return err
	}

	return client.LeaveGroup(ctx, JID)
}

func (service serviceGroup) CreateGroup(ctx context.Context, request domainGroup.CreateGroupRequest) (groupID string, err error) {
	if err = validations.ValidateCreateGroup(ctx, request); err != nil {
		return groupID, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return groupID, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	participantsJID, err := service.participantToJID(ctx, request.Participants)
	if err != nil {
		return
	}

	groupConfig := whatsmeow.ReqCreateGroup{
		Name:              request.Title,
		Participants:      participantsJID,
		GroupParent:       types.GroupParent{},
		GroupLinkedParent: types.GroupLinkedParent{},
	}

	groupInfo, err := client.CreateGroup(ctx, groupConfig)
	if err != nil {
		return
	}

	return groupInfo.JID.String(), nil
}

func (service serviceGroup) GetGroupInfoFromLink(ctx context.Context, request domainGroup.GetGroupInfoFromLinkRequest) (response domainGroup.GetGroupInfoFromLinkResponse, err error) {
	if err = validations.ValidateGetGroupInfoFromLink(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	groupInfo, err := client.GetGroupInfoFromLink(ctx, request.Link)
	if err != nil {
		return response, err
	}

	response = domainGroup.GetGroupInfoFromLinkResponse{
		GroupID:          groupInfo.JID.String(),
		Name:             groupInfo.Name,
		Topic:            groupInfo.Topic,
		CreatedAt:        groupInfo.GroupCreated,
		ParticipantCount: len(groupInfo.Participants),
		IsLocked:         groupInfo.IsLocked,
		IsAnnounce:       groupInfo.IsAnnounce,
		IsEphemeral:      groupInfo.IsEphemeral,
		Description:      groupInfo.Topic, // Topic serves as description
	}

	return response, nil
}

func (service serviceGroup) ManageParticipant(ctx context.Context, request domainGroup.ParticipantRequest) (result []domainGroup.ParticipantStatus, err error) {
	if err = validations.ValidateParticipant(ctx, request); err != nil {
		return result, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return result, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return result, err
	}

	participantsJID, err := service.participantToJID(ctx, request.Participants)
	if err != nil {
		return result, err
	}

	participants, err := client.UpdateGroupParticipants(ctx, groupJID, participantsJID, request.Action)
	if err != nil {
		return result, err
	}

	for _, participant := range participants {
		if participant.Error == 403 && participant.AddRequest != nil {
			result = append(result, domainGroup.ParticipantStatus{
				Participant: participant.JID.String(),
				Status:      "error",
				Message:     "Failed to add participant",
			})
		} else {
			result = append(result, domainGroup.ParticipantStatus{
				Participant: participant.JID.String(),
				Status:      "success",
				Message:     "Action success",
			})
		}
	}

	return result, nil
}

func (service serviceGroup) GetGroupParticipants(ctx context.Context, request domainGroup.GetGroupParticipantsRequest) (response domainGroup.GetGroupParticipantsResponse, err error) {
	if err = validations.ValidateGetGroupParticipants(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return response, err
	}

	groupInfo, err := client.GetGroupInfo(ctx, groupJID)
	if err != nil {
		return response, err
	}

	response.GroupID = groupJID.String()
	if groupInfo != nil {
		response.Name = groupInfo.GroupName.Name
		response.Participants = make([]domainGroup.GroupParticipant, 0, len(groupInfo.Participants))

		// Collect JIDs for batch GetUserInfo call
		jids := make([]types.JID, 0, len(groupInfo.Participants))
		for _, p := range groupInfo.Participants {
			jids = append(jids, p.JID)
		}

		// Fetch user info for verified business names (ignore errors)
		userInfoMap := make(map[types.JID]types.UserInfo)
		if len(jids) > 0 {
			userInfoMap, _ = client.GetUserInfo(ctx, jids)
		}

		for _, participant := range groupInfo.Participants {
			displayName := participant.DisplayName

			// Try contact store first (for known contacts)
			if displayName == "" {
				if contact, err := client.Store.Contacts.GetContact(ctx, participant.JID); err == nil && contact.FullName != "" {
					displayName = contact.FullName
				} else if info, ok := userInfoMap[participant.JID]; ok && info.VerifiedName != nil && info.VerifiedName.Details != nil {
					// Fall back to verified business name
					displayName = info.VerifiedName.Details.GetVerifiedName()
				}
			}

			// Use JID.User for clean phone number, fallback to PhoneNumber.String()
			phoneNumber := participant.JID.User
			if phoneNumber == "" {
				phoneNumber = participant.PhoneNumber.String()
			}

			participantData := domainGroup.GroupParticipant{
				JID:          participant.JID.String(),
				PhoneNumber:  phoneNumber,
				LID:          participant.LID.String(),
				DisplayName:  displayName,
				IsAdmin:      participant.IsAdmin,
				IsSuperAdmin: participant.IsSuperAdmin,
			}

			response.Participants = append(response.Participants, participantData)
		}
	}

	return response, nil
}

func (service serviceGroup) GetGroupRequestParticipants(ctx context.Context, request domainGroup.GetGroupRequestParticipantsRequest) (result []domainGroup.GetGroupRequestParticipantsResponse, err error) {
	if err = validations.ValidateGetGroupRequestParticipants(ctx, request); err != nil {
		return result, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return result, pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return result, err
	}

	participants, err := client.GetGroupRequestParticipants(ctx, groupJID)
	if err != nil {
		return result, err
	}

	// Collect JIDs for batch GetUserInfo call
	jids := make([]types.JID, 0, len(participants))
	for _, p := range participants {
		jids = append(jids, p.JID)
	}

	// Fetch user info for verified business names (ignore errors)
	userInfoMap := make(map[types.JID]types.UserInfo)
	if len(jids) > 0 {
		userInfoMap, _ = client.GetUserInfo(ctx, jids)
	}

	for _, participant := range participants {
		displayName := ""

		// Try contact store first (for known contacts)
		if contact, err := client.Store.Contacts.GetContact(ctx, participant.JID); err == nil && contact.FullName != "" {
			displayName = contact.FullName
		} else if info, ok := userInfoMap[participant.JID]; ok && info.VerifiedName != nil && info.VerifiedName.Details != nil {
			// Fall back to verified business name
			displayName = info.VerifiedName.Details.GetVerifiedName()
		}

		result = append(result, domainGroup.GetGroupRequestParticipantsResponse{
			JID:         participant.JID.String(),
			PhoneNumber: participant.JID.User,
			DisplayName: displayName,
			RequestedAt: participant.RequestedAt,
		})
	}

	return result, nil
}

func (service serviceGroup) ManageGroupRequestParticipants(ctx context.Context, request domainGroup.GroupRequestParticipantsRequest) (result []domainGroup.ParticipantStatus, err error) {
	if err = validations.ValidateManageGroupRequestParticipants(ctx, request); err != nil {
		return result, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return result, pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return result, err
	}

	participantsJID, err := service.participantToJID(ctx, request.Participants)
	if err != nil {
		return result, err
	}

	participants, err := client.UpdateGroupRequestParticipants(ctx, groupJID, participantsJID, request.Action)
	if err != nil {
		return result, err
	}

	for _, participant := range participants {
		if participant.Error != 0 {
			result = append(result, domainGroup.ParticipantStatus{
				Participant: participant.JID.String(),
				Status:      "error",
				Message:     fmt.Sprintf("Action %s failed (code %d)", request.Action, participant.Error),
			})
		} else {
			result = append(result, domainGroup.ParticipantStatus{
				Participant: participant.JID.String(),
				Status:      "success",
				Message:     fmt.Sprintf("Action %s success", request.Action),
			})
		}
	}

	return result, nil
}

func (service serviceGroup) participantToJID(ctx context.Context, participants []string) ([]types.JID, error) {
	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return nil, pkgError.ErrWaCLI
	}

	var participantsJID []types.JID
	for _, participant := range participants {
		formattedParticipant := participant + config.WhatsappTypeUser

		if !utils.IsOnWhatsapp(client, formattedParticipant) {
			return nil, pkgError.ErrUserNotRegistered
		}

		if participantJID, err := types.ParseJID(formattedParticipant); err == nil {
			participantsJID = append(participantsJID, participantJID)
		}
	}
	return participantsJID, nil
}

func (service serviceGroup) SetGroupPhoto(ctx context.Context, request domainGroup.SetGroupPhotoRequest) (pictureID string, err error) {
	if err = validations.ValidateSetGroupPhoto(ctx, request); err != nil {
		return pictureID, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pictureID, pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return pictureID, err
	}

	var photoBytes []byte
	if request.Photo != nil {
		// Process the image for WhatsApp group photo requirements
		logrus.Printf("Processing group photo: %s (size: %d bytes)", request.Photo.Filename, request.Photo.Size)

		processedImageBuffer, err := utils.ProcessGroupPhoto(request.Photo)
		if err != nil {
			logrus.Printf("Failed to process group photo: %v", err)
			return pictureID, err
		}

		logrus.Printf("Successfully processed group photo: %d bytes -> %d bytes",
			request.Photo.Size, processedImageBuffer.Len())

		// Convert buffer to byte slice
		photoBytes = processedImageBuffer.Bytes()
	}

	pictureID, err = client.SetGroupPhoto(ctx, groupJID, photoBytes)
	if err != nil {
		logrus.Printf("Failed to set group photo: %v", err)
		return pictureID, err
	}

	return pictureID, nil
}

func (service serviceGroup) SetGroupName(ctx context.Context, request domainGroup.SetGroupNameRequest) (err error) {
	if err = validations.ValidateSetGroupName(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return err
	}

	return client.SetGroupName(ctx, groupJID, request.Name)
}

func (service serviceGroup) SetGroupLocked(ctx context.Context, request domainGroup.SetGroupLockedRequest) (err error) {
	if err = validations.ValidateSetGroupLocked(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return err
	}

	return client.SetGroupLocked(ctx, groupJID, request.Locked)
}

func (service serviceGroup) SetGroupAnnounce(ctx context.Context, request domainGroup.SetGroupAnnounceRequest) (err error) {
	if err = validations.ValidateSetGroupAnnounce(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return err
	}

	return client.SetGroupAnnounce(ctx, groupJID, request.Announce)
}

func (service serviceGroup) SetGroupTopic(ctx context.Context, request domainGroup.SetGroupTopicRequest) (err error) {
	if err = validations.ValidateSetGroupTopic(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return err
	}

	// SetGroupTopic with auto-generated IDs (previousID and newID will be handled automatically)
	return client.SetGroupTopic(ctx, groupJID, "", "", request.Topic)
}

// GroupInfo retrieves detailed information about a WhatsApp group
func (service serviceGroup) GroupInfo(ctx context.Context, request domainGroup.GroupInfoRequest) (response domainGroup.GroupInfoResponse, err error) {
	// Validate the incoming request
	if err = validations.ValidateGroupInfo(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	// Ensure we are logged in
	utils.MustLogin(client)

	// Validate and parse the provided group JID / ID
	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return response, err
	}

	// Fetch group information from WhatsApp
	groupInfo, err := client.GetGroupInfo(ctx, groupJID)
	if err != nil {
		return response, err
	}

	// Map the response
	if groupInfo != nil {
		response.Data = *groupInfo
	}

	return response, nil
}

func (service serviceGroup) GetGroupInviteLink(ctx context.Context, request domainGroup.GetGroupInviteLinkRequest) (response domainGroup.GetGroupInviteLinkResponse, err error) {
	if err = validations.ValidateGetGroupInviteLink(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	groupJID, err := utils.ValidateJidWithLogin(client, request.GroupID)
	if err != nil {
		return response, err
	}

	inviteLink, err := client.GetGroupInviteLink(ctx, groupJID, request.Reset)
	if err != nil {
		return response, err
	}

	response = domainGroup.GetGroupInviteLinkResponse{
		InviteLink: inviteLink,
		GroupID:    request.GroupID,
	}

	return response, nil
}
