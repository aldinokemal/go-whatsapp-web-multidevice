package services

import (
	"context"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"go.mau.fi/whatsmeow"
)

type groupService struct {
	WaCli *whatsmeow.Client
}

func NewGroupService(waCli *whatsmeow.Client) domainGroup.IGroupService {
	return &groupService{
		WaCli: waCli,
	}
}

func (service groupService) JoinGroupWithLink(_ context.Context, request domainGroup.JoinGroupWithLinkRequest) (groupID string, err error) {
	whatsapp.MustLogin(service.WaCli)

	jid, err := service.WaCli.JoinGroupWithLink(request.Link)
	if err != nil {
		return
	}
	return jid.String(), nil
}

func (service groupService) LeaveGroup(_ context.Context, groupID string) (err error) {
	JID, err := whatsapp.ValidateJidWithLogin(service.WaCli, groupID)
	if err != nil {
		return err
	}

	return service.WaCli.LeaveGroup(JID)
}
