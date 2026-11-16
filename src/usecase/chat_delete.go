package usecase

import (
	"context"
	"fmt"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types"
)

func (service serviceChat) DeleteChat(ctx context.Context, request domainChat.DeleteChatRequest) (response domainChat.DeleteChatResponse, err error) {
	if err = validations.ValidateDeleteChat(ctx, &request); err != nil {
		return response, err
	}
	deviceID := storageDeviceID(ctx)
	if deviceID == "" {
		return response, pkgError.ValidationError("device identification required")
	}
	client := whatsapp.ClientFromContext(ctx)
	target, _ := types.ParseJID(request.ChatJID) // Validated above.
	target = target.ToNonAD()
	jids, err := whatsapp.ChatDeleteJIDs(ctx, client, target)
	if err != nil {
		return response, err
	}
	deleteMedia := request.DeleteMedia == nil || *request.DeleteMedia
	if !request.LocalOnly {
		var latest *domainChatStorage.Message
		for _, jid := range jids {
			messages, err := service.chatStorageRepo.GetMessages(&domainChatStorage.MessageFilter{DeviceID: deviceID, ChatJID: jid, Limit: 1})
			if err != nil {
				return response, err
			}
			if len(messages) > 0 && (latest == nil || messages[0].Timestamp.After(latest.Timestamp)) {
				latest = messages[0]
			}
		}
		if latest == nil || latest.ID == "" || latest.Timestamp.IsZero() {
			return response, pkgError.ValidationError("chat has no stored message to anchor deletion")
		}
		if client == nil {
			return response, pkgError.ErrWaCLI
		}
		target = whatsapp.NormalizeJIDFromLID(ctx, target, client).ToNonAD()
		key, err := deleteChatMessageKey(ctx, client, target, latest)
		if err != nil {
			return response, err
		}
		sender := service.sendDeleteChatPatch
		if sender == nil {
			if !client.IsConnected() {
				return response, pkgError.ErrNotConnected
			}
			if !client.IsLoggedIn() {
				return response, pkgError.ErrNotLoggedIn
			}
			sender = client.SendAppState
		}
		if err := sender(ctx, appstate.BuildDeleteChat(target, latest.Timestamp, key, deleteMedia)); err != nil {
			return response, err
		}
	}
	if err := whatsapp.PurgeLocalChat(service.chatStorageRepo, deviceID, jids, deleteMedia); err != nil {
		if request.LocalOnly {
			return response, pkgError.InternalServerError(fmt.Sprintf("local chat deletion failed: %v", err))
		}
		return response, pkgError.LocalDeleteFailed(fmt.Sprintf("chat was deleted on WhatsApp but the local copy was not fully removed: %v", err))
	}
	message := "Chat deleted on WhatsApp and locally"
	if request.LocalOnly {
		message = "Local chat copy deleted"
	}
	return domainChat.DeleteChatResponse{Status: "success", Message: message, ChatJID: request.ChatJID, LocalOnly: request.LocalOnly}, nil
}

func deleteChatMessageKey(ctx context.Context, client *whatsmeow.Client, target types.JID, message *domainChatStorage.Message) (*waCommon.MessageKey, error) {
	key := &waCommon.MessageKey{RemoteJID: new(target.String()), FromMe: new(message.IsFromMe), ID: new(message.ID)}
	if target.Server == types.GroupServer && !message.IsFromMe {
		sender, err := types.ParseJID(message.Sender)
		if err != nil || sender.User == "" {
			return nil, pkgError.ValidationError("stored group message has no valid sender")
		}
		key.Participant = new(whatsapp.NormalizeJIDFromLID(ctx, sender, client).ToNonAD().String())
	}
	return key, nil
}
