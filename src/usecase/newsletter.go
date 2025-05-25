package usecase

import (
	"context"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
)

type serviceNewsletter struct {
	WaCli *whatsmeow.Client
}

func NewNewsletterService(waCli *whatsmeow.Client) domainNewsletter.INewsletterUsecase {
	return &serviceNewsletter{
		WaCli: waCli,
	}
}

func (service serviceNewsletter) Unfollow(ctx context.Context, request domainNewsletter.UnfollowRequest) (err error) {
	if err = validations.ValidateUnfollowNewsletter(ctx, request); err != nil {
		return err
	}

	JID, err := whatsapp.ValidateJidWithLogin(service.WaCli, request.NewsletterID)
	if err != nil {
		return err
	}

	return service.WaCli.UnfollowNewsletter(JID)
}
