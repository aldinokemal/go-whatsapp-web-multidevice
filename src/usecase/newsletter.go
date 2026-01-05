package usecase

import (
	"context"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
)

type serviceNewsletter struct{}

func NewNewsletterService() domainNewsletter.INewsletterUsecase {
	return &serviceNewsletter{}
}

func (service serviceNewsletter) Unfollow(ctx context.Context, request domainNewsletter.UnfollowRequest) (err error) {
	if err = validations.ValidateUnfollowNewsletter(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	JID, err := utils.ValidateJidWithLogin(client, request.NewsletterID)
	if err != nil {
		return err
	}

	return client.UnfollowNewsletter(ctx, JID)
}
