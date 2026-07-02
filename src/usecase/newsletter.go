package usecase

import (
	"context"
	"time"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
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

func (service serviceNewsletter) GetMessages(ctx context.Context, request domainNewsletter.GetMessagesRequest) (response domainNewsletter.GetMessagesResponse, err error) {
	if err = validations.ValidateGetNewsletterMessages(ctx, &request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	JID, err := utils.ValidateJidWithLogin(client, request.NewsletterID)
	if err != nil {
		return response, err
	}

	params := &whatsmeow.GetNewsletterMessagesParams{
		Count: request.Count,
	}
	if request.Before != 0 {
		params.Before = types.MessageServerID(request.Before)
	}

	messages, err := client.GetNewsletterMessages(ctx, JID, params)
	if err != nil {
		return response, err
	}

	response.Data = make([]domainNewsletter.Message, 0, len(messages))
	for _, msg := range messages {
		response.Data = append(response.Data, domainNewsletter.Message{
			ServerID:       int(msg.MessageServerID),
			MessageID:      string(msg.MessageID),
			Type:           msg.Type,
			Timestamp:      msg.Timestamp.Format(time.RFC3339),
			ViewsCount:     msg.ViewsCount,
			ReactionCounts: msg.ReactionCounts,
			Text:           utils.ExtractMessageTextFromProto(msg.Message),
		})
	}

	return response, nil
}
