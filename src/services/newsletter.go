package services

import (
	"context"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
)

type newsletterService struct {
	WaCli *whatsmeow.Client
}

func NewNewsletterService(waCli *whatsmeow.Client) domainNewsletter.INewsletterService {
	return &newsletterService{
		WaCli: waCli,
	}
}

func (service newsletterService) Unfollow(ctx context.Context, request domainNewsletter.UnfollowRequest) (err error) {
	if err = validations.ValidateUnfollowNewsletter(ctx, request); err != nil {
		return err
	}

	JID, err := whatsapp.ValidateJidWithLogin(service.WaCli, request.NewsletterID)
	if err != nil {
		return err
	}

	return service.WaCli.UnfollowNewsletter(JID)
}
