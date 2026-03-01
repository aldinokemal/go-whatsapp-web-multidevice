package whatsapp

import (
	"context"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

func handleArchive(ctx context.Context, evt *events.Archive, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) {
	if evt == nil || chatStorageRepo == nil {
		return
	}
	if evt.Action == nil || evt.Action.Archived == nil {
		return
	}

	// Derive device ID from client (strips device number suffix like :11)
	deviceID := client.Store.ID.ToNonAD().String()
	if deviceID == "" {
		return
	}

	// Normalize JID from LID format (@lid) to regular format (@s.whatsapp.net)
	jidStr := NormalizeJIDFromLID(ctx, evt.JID, client).String()

	logFields := logrus.Fields{"device_id": deviceID, "jid": jidStr}

	chat, err := chatStorageRepo.GetChatByDevice(deviceID, jidStr)
	if err != nil {
		logrus.WithError(err).WithFields(logFields).Debug("Failed to get chat in handleArchive")
		return
	}
	if chat == nil {
		logrus.WithFields(logFields).Debug("Chat not found in handleArchive, skipping")
		return
	}

	chat.Archived = *evt.Action.Archived
	if err = chatStorageRepo.StoreChat(chat); err != nil {
		logrus.WithError(err).WithFields(logFields).Error("Failed to update chat archive status")
	}
}
