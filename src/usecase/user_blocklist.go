package usecase

import (
	"context"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func (service serviceUser) MyBlocklist(ctx context.Context) (response domainUser.BlocklistResponse, err error) {
	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	list, err := client.GetBlocklist(ctx, "")
	if err != nil {
		return response, err
	}
	return toBlocklistResponse(list), nil
}

func (service serviceUser) Block(ctx context.Context, request domainUser.BlockRequest) (domainUser.BlocklistResponse, error) {
	return service.updateBlocklist(ctx, request, events.BlocklistChangeActionBlock)
}

func (service serviceUser) Unblock(ctx context.Context, request domainUser.BlockRequest) (domainUser.BlocklistResponse, error) {
	return service.updateBlocklist(ctx, request, events.BlocklistChangeActionUnblock)
}

func (service serviceUser) updateBlocklist(ctx context.Context, request domainUser.BlockRequest, action events.BlocklistChangeAction) (response domainUser.BlocklistResponse, err error) {
	if err = validations.ValidateBlock(ctx, request); err != nil {
		return response, err
	}
	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	jid, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	list, err := client.UpdateBlocklist(ctx, jid, action, "")
	if err != nil {
		return response, err
	}
	return toBlocklistResponse(list), nil
}

func toBlocklistResponse(list *types.Blocklist) domainUser.BlocklistResponse {
	response := domainUser.BlocklistResponse{Data: []domainUser.BlocklistItem{}}
	if list == nil {
		return response
	}
	for _, item := range list.Items {
		entry := domainUser.BlocklistItem{LID: item.LID.String()}
		if !item.PN.IsEmpty() {
			entry.PN = item.PN.String()
		}
		response.Data = append(response.Data, entry)
	}
	return response
}
