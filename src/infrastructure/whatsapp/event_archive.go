package whatsapp

import (
	"context"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func handleArchive(ctx context.Context, evt *events.Archive, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) {
	if evt == nil {
		return
	}
	if chatStorageRepo == nil {
		return
	}

	// Derive device ID from client (strips device number suffix like :11)
	// This matches how history_sync and event_message derive device IDs for DB lookups
	deviceID := client.Store.ID.ToNonAD().String()
	if deviceID == "" {
		return
	}

	// Normalize JID from LID format (@lid) to regular format (@s.whatsapp.net)
	normalizedJID := NormalizeJIDFromLID(ctx, evt.JID, client)
	jidStr := normalizedJID.String()

	chat, err := chatStorageRepo.GetChatByDevice(deviceID, jidStr)
	if err != nil || chat == nil {
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
		logrus.WithError(err).WithField("device_id", deviceID).WithField("jid", jidStr).Error("Failed to update chat archive status")
	}
}
