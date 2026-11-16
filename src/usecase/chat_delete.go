package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
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
	var bound *time.Time
	if !request.LocalOnly {
		var latest *domainChatStorage.Message
		// shortcut: sample 20 text-sorted rows per alias, use instant-ordered storage if a wider range is needed
		for _, jid := range jids {
			messages, err := service.chatStorageRepo.GetMessages(&domainChatStorage.MessageFilter{DeviceID: deviceID, ChatJID: jid, Limit: 20})
			if err != nil {
				return response, err
			}
			for _, message := range messages {
				if latest == nil || message.Timestamp.After(latest.Timestamp) {
					latest = message
				}
			}
		}
		if latest == nil || latest.ID == "" || latest.Timestamp.IsZero() {
			return response, pkgError.ValidationError("chat has no stored message to anchor deletion")
		}
		bound = new(latest.Timestamp)
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
			// shortcut: matches whatsmeow's untyped error text at the pinned version, switch to a typed error if upstream adds one
			if !strings.HasPrefix(err.Error(), "failed to fetch app state after sending update") {
				return response, err
			}
			logrus.Warnf("Chat deletion accepted, but app state refresh failed: %v", err)
		}
	}
	if err := whatsapp.PurgeLocalChat(service.chatStorageRepo, deviceID, jids, deleteMedia, bound); err != nil {
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
