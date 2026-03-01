package whatsapp

import (
	"context"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func handleArchive(ctx context.Context, evt *events.Archive, chatStorageRepo domainChatStorage.IChatStorageRepository, deviceID string, client *whatsmeow.Client) {
	_ = ctx
	_ = client
	if evt == nil {
		return
	}
	if chatStorageRepo == nil {
		return
	}
	
	if deviceID == "" {
		return
	}

	jidStr := evt.JID.String()

	chat, err := chatStorageRepo.GetChatByDevice(deviceID, jidStr)
	if err != nil || chat == nil {
		// Just log and exit if chat doesn't exist, as it will be synced later
		if err != nil {
			logrus.WithError(err).WithField("device_id", deviceID).WithField("jid", jidStr).Debug("Failed to get chat by device in handleArchive")
		} else {
			logrus.WithField("device_id", deviceID).WithField("jid", jidStr).Debug("Chat not found in handleArchive, skipping archive status update")
		}
		return
	}
	
	if evt.Action == nil || evt.Action.Archived == nil {
		return
	}

	isArchived := *evt.Action.Archived

	chat.Archived = isArchived
	err = chatStorageRepo.StoreChat(chat)
	if err != nil {
		logrus.WithError(err).Error("Failed to update chat archive status")
	}
}
