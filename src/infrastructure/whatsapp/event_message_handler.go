package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func handleMessage(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) {
	// Log message metadata
	metaParts := buildMessageMetaParts(evt)
	log.Infof("Received message %s from %s (%s): %+v",
		evt.Info.ID,
		evt.Info.SourceString(),
		strings.Join(metaParts, ", "),
		evt.Message,
	)

	if isReactionMessage(evt) {
		if err := chatStorageRepo.CreateReaction(ctx, evt); err != nil {
			log.Errorf("Failed to store incoming reaction %s: %v", evt.Info.ID, err)
		}

		handleWebhookForward(ctx, evt, client)
		return
	}

	if err := chatStorageRepo.CreateMessage(ctx, evt); err != nil {
		// Log storage errors to avoid silent failures that could lead to data loss
		log.Errorf("Failed to store incoming message %s: %v", evt.Info.ID, err)
	}

	// Update unread count based on message direction
	handleUnreadCountUpdate(ctx, evt, chatStorageRepo, client)

	// Handle image message if present
	handleImageMessage(ctx, evt, client)

	// Auto-mark message as read if configured
	handleAutoMarkRead(ctx, evt, client)

	// Handle auto-reply if configured
	handleAutoReply(ctx, evt, chatStorageRepo, client)

	// Forward to webhook if configured
	handleWebhookForward(ctx, evt, client)
}

func buildMessageMetaParts(evt *events.Message) []string {
	metaParts := []string{
		fmt.Sprintf("pushname: %s", evt.Info.PushName),
		fmt.Sprintf("timestamp: %s", evt.Info.Timestamp),
	}
	if evt.Info.Type != "" {
		metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
	}
	if evt.Info.Category != "" {
		metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
	}
	if evt.IsViewOnce {
		metaParts = append(metaParts, "view once")
	}
	return metaParts
}

func handleImageMessage(ctx context.Context, evt *events.Message, client *whatsmeow.Client) {
	if !config.WhatsappAutoDownloadMedia {
		return
	}
	if client == nil {
		return
	}
	if img := evt.Message.GetImageMessage(); img != nil {
		if extracted, err := utils.ExtractMedia(ctx, client, config.PathStorages, img); err != nil {
			log.Errorf("Failed to download image: %v", err)
		} else {
			log.Infof("Image downloaded to %s", extracted.MediaPath)
		}
	}
}

func handleAutoMarkRead(ctx context.Context, evt *events.Message, client *whatsmeow.Client) {
	// Only mark read if auto-mark read is enabled and message is incoming
	if !config.WhatsappAutoMarkRead || evt.Info.IsFromMe {
		return
	}

	if client == nil {
		return
	}

	// Mark the message as read
	messageIDs := []types.MessageID{evt.Info.ID}
	timestamp := time.Now()
	chat := evt.Info.Chat
	sender := evt.Info.Sender

	if err := client.MarkRead(ctx, messageIDs, timestamp, chat, sender); err != nil {
		log.Warnf("Failed to mark message %s as read: %v", evt.Info.ID, err)
	} else {
		log.Debugf("Marked message %s as read", evt.Info.ID)
	}
}

func handleUnreadCountUpdate(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) {
	deviceID := ""
	inst, hasDevice := DeviceFromContext(ctx)
	if hasDevice && inst != nil {
		deviceID = inst.JID()
		if deviceID == "" {
			deviceID = inst.ID()
		}
	}
	// Fallback: resolve deviceID from client store when context lookup fails
	if deviceID == "" && client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}
	if deviceID == "" {
		log.Warnf("handleUnreadCountUpdate: could not resolve deviceID (hasDevice=%v) for message %s", hasDevice, evt.Info.ID)
		return
	}

	chatJID := NormalizeJIDFromLID(ctx, evt.Info.Chat, client).ToNonAD().String()

	if evt.Info.IsFromMe {
		log.Infof("Resetting unread count for chat %s (message from me)", chatJID)
		if err := chatStorageRepo.ResetChatUnreadCount(deviceID, chatJID); err != nil {
			log.Warnf("Failed to reset unread count for chat %s: %v", chatJID, err)
		}
		if err := chatStorageRepo.MarkMessagesAsRead(deviceID, chatJID); err != nil {
			log.Warnf("Failed to mark messages as read for chat %s: %v", chatJID, err)
		}
	} else {
		log.Infof("Incrementing unread count for chat %s (incoming message)", chatJID)
		if err := chatStorageRepo.IncrementChatUnreadCount(deviceID, chatJID); err != nil {
			log.Warnf("Failed to increment unread count for chat %s: %v", chatJID, err)
		}
	}
}

func handleWebhookForward(ctx context.Context, evt *events.Message, client *whatsmeow.Client) {
	// Skip webhook for protocol messages that are internal sync messages
	if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		protocolType := protocolMessage.GetType().String()
		// Only allow REVOKE and MESSAGE_EDIT through - skip all other protocol messages
		// (HISTORY_SYNC_NOTIFICATION, APP_STATE_SYNC_KEY_SHARE, EPHEMERAL_SYNC_RESPONSE, etc.)
		switch protocolType {
		case "REVOKE", "MESSAGE_EDIT":
			// These are meaningful user actions, allow webhook
		default:
			log.Debugf("Skipping webhook for protocol message type: %s", protocolType)
			return
		}
	}

	if (len(config.WhatsappWebhook) > 0 || config.ChatwootEnabled) &&
		!strings.Contains(evt.Info.SourceString(), "broadcast") {
		go func(e *events.Message, c *whatsmeow.Client) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardMessageToWebhook(webhookCtx, c, e); err != nil {
				logrus.Error("Failed forward to webhook: ", err)
			}
		}(evt, client)
	}
}
