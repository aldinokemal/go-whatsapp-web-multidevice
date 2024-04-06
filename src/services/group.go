package services

import (
	"context"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type groupService struct {
	WaCli *whatsmeow.Client
}

func NewGroupService(waCli *whatsmeow.Client) domainGroup.IGroupService {
	return &groupService{
		WaCli: waCli,
	}
}

func (service groupService) JoinGroupWithLink(ctx context.Context, request domainGroup.JoinGroupWithLinkRequest) (groupID string, err error) {
	if err = validations.ValidateJoinGroupWithLink(ctx, request); err != nil {
		return groupID, err
	}
	whatsapp.MustLogin(service.WaCli)

	jid, err := service.WaCli.JoinGroupWithLink(request.Link)
	if err != nil {
		return
	}
	return jid.String(), nil
}

func (service groupService) LeaveGroup(ctx context.Context, request domainGroup.LeaveGroupRequest) (err error) {
	if err = validations.ValidateLeaveGroup(ctx, request); err != nil {
		return err
	}

	JID, err := whatsapp.ValidateJidWithLogin(service.WaCli, request.GroupID)
	if err != nil {
		return err
	}

	return service.WaCli.LeaveGroup(JID)
}

func (service groupService) CreateGroup(ctx context.Context, request domainGroup.CreateGroupRequest) (groupID string, err error) {
	if err = validations.ValidateCreateGroup(ctx, request); err != nil {
		return groupID, err
	}
	whatsapp.MustLogin(service.WaCli)

	var participantsJID []types.JID
	for _, participant := range request.Participants {
		formattedParticipant := participant + config.WhatsappTypeUser

		if !whatsapp.IsOnWhatsapp(service.WaCli, formattedParticipant) {
			return "", pkgError.ErrUserNotRegistered
		}

		if participantJID, err := types.ParseJID(formattedParticipant); err == nil {
			participantsJID = append(participantsJID, participantJID)
		}
	}

	groupConfig := whatsmeow.ReqCreateGroup{
		Name:              request.Title,
		Participants:      participantsJID,
		GroupParent:       types.GroupParent{},
		GroupLinkedParent: types.GroupLinkedParent{},
	}

	groupInfo, err := service.WaCli.CreateGroup(groupConfig)
	if err != nil {
		return
	}

	return groupInfo.JID.String(), nil
}
