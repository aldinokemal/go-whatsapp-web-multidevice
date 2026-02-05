package whatsapp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// handler is the main event handler for WhatsApp events, scoped to a device instance.
func handler(ctx context.Context, instance *DeviceInstance, rawEvt any) {
	if instance == nil {
		return
	}

	// Ensure downstream handlers see the device context (used for device-scoped storage).
	ctx = ContextWithDevice(ctx, instance)

	chatStorageRepo := instance.GetChatStorage()
	client := instance.GetClient()

	switch evt := rawEvt.(type) {
	case *events.DeleteForMe:
		handleDeleteForMe(ctx, evt, chatStorageRepo, instance.JID(), client)
	case *events.AppStateSyncComplete:
		handleAppStateSyncComplete(ctx, client, evt)
	case *events.PairSuccess:
		handlePairSuccess(ctx, evt)
	case *events.LoggedOut:
		handleLoggedOut(ctx, instance, chatStorageRepo)
	case *events.Connected, *events.PushNameSetting:
		handleConnectionEvents(ctx, client, instance)
	case *events.StreamReplaced:
		handleStreamReplaced(ctx)
	case *events.Message:
		handleMessage(ctx, evt, chatStorageRepo, client)
	case *events.Receipt:
		handleReceipt(ctx, evt, instance.JID(), client)
	case *events.Presence:
		handlePresence(ctx, evt)
	case *events.HistorySync:
		handleHistorySync(ctx, evt, chatStorageRepo, client)
	case *events.AppState:
		handleAppState(ctx, evt)
	case *events.GroupInfo:
		handleGroupInfo(ctx, evt, instance.JID(), client)
	case *events.JoinedGroup:
		handleJoinedGroup(ctx, evt, instance.JID(), client)
	case *events.NewsletterJoin:
		handleNewsletterJoin(ctx, evt, instance.JID(), client)
	case *events.NewsletterLeave:
		handleNewsletterLeave(ctx, evt, instance.JID(), client)
	case *events.NewsletterLiveUpdate:
		handleNewsletterLiveUpdate(ctx, evt, instance.JID(), client)
	case *events.NewsletterMuteChange:
		handleNewsletterMuteChange(ctx, evt, instance.JID(), client)
	case *events.CallOffer:
		handleCallOffer(ctx, evt, instance.JID(), client)
	}

	instance.UpdateStateFromClient()
}

func handleDeleteForMe(ctx context.Context, evt *events.DeleteForMe, chatStorageRepo domainChatStorage.IChatStorageRepository, deviceID string, client *whatsmeow.Client) {
	log.Infof("Deleted message %s for %s", evt.MessageID, evt.SenderJID.String())

	// Find the message to get its chat JID
	message, err := chatStorageRepo.GetMessageByID(evt.MessageID)
	if err != nil {
		log.Errorf("Failed to find message %s for deletion: %v", evt.MessageID, err)
		return
	}

	if message == nil {
		log.Warnf("Message %s not found in database, skipping deletion", evt.MessageID)
		return
	}

	// Delete the message from database
	if err := chatStorageRepo.DeleteMessage(evt.MessageID, message.ChatJID); err != nil {
		log.Errorf("Failed to delete message %s from database: %v", evt.MessageID, err)
	} else {
		log.Infof("Successfully deleted message %s from database", evt.MessageID)
	}

	// Send webhook notification for delete event
	if len(config.WhatsappWebhook) > 0 {
		go func(c *whatsmeow.Client) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardDeleteToWebhook(webhookCtx, evt, message, deviceID, c); err != nil {
				log.Errorf("Failed to forward delete event to webhook: %v", err)
			}
		}(client)
	}
}

func handleAppStateSyncComplete(_ context.Context, client *whatsmeow.Client, evt *events.AppStateSyncComplete) {
	if client == nil {
		return
	}
	if len(client.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
		if err := client.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
			log.Warnf("Failed to send available presence: %v", err)
		} else {
			log.Infof("Marked self as available")
		}
	}
}

func handlePairSuccess(ctx context.Context, evt *events.PairSuccess) {
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "LOGIN_SUCCESS",
		Message: fmt.Sprintf("Successfully pair with %s", evt.ID.String()),
	}
	primaryDB, secondaryDB := getStoreContainers()
	syncKeysDevice(ctx, primaryDB, secondaryDB)
}

func handleLoggedOut(ctx context.Context, instance *DeviceInstance, chatStorageRepo domainChatStorage.IChatStorageRepository) {
	logrus.Warnf("[REMOTE_LOGOUT] Received LoggedOut event for device %s - user logged out from phone", instance.ID())

	if client := instance.GetClient(); client != nil {
		client.Disconnect()
	}
	instance.SetState(domainDevice.DeviceStateDisconnected)

	if chatStorageRepo != nil {
		if err := chatStorageRepo.TruncateAllDataWithLogging("REMOTE_LOGOUT"); err != nil {
			logrus.Errorf("[REMOTE_LOGOUT] Failed to truncate chat storage: %v", err)
		}
	}

	deviceID := instance.ID()

	instance.TriggerLoggedOut()

	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "LOGOUT_COMPLETE",
		Message: "Remote logout cleanup completed - device removed from server",
		Result:  map[string]string{"device_id": deviceID},
	}
}

func handleConnectionEvents(_ context.Context, client *whatsmeow.Client, instance *DeviceInstance) {
	if client == nil {
		return
	}
	if instance != nil {
		instance.UpdateStateFromClient()

		// Persist updated JID/DisplayName to database after successful connection
		// Skip if instance.ID looks like a JID (auto-created device) to avoid recreating deleted duplicates
		if repo := instance.GetChatStorage(); repo != nil && !strings.Contains(instance.ID(), "@") {
			jid := instance.JID()
			displayName := instance.DisplayName()
			if jid != "" {
				if err := repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
					DeviceID:    instance.ID(),
					DisplayName: displayName,
					JID:         jid,
					CreatedAt:   instance.CreatedAt(),
				}); err != nil {
					log.Warnf("Failed to persist device record for %s: %v", instance.ID(), err)
				}
			}
		}
	}
	if len(client.Store.PushName) == 0 {
		return
	}

	// Send presence available when connecting and when the pushname is changed.
	// This makes sure that outgoing messages always have the right pushname.
	if err := client.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
		log.Warnf("Failed to send available presence: %v", err)
	} else {
		log.Infof("Marked self as available")
	}
}

func handleStreamReplaced(_ context.Context) {
	os.Exit(0)
}

func handleReceipt(ctx context.Context, evt *events.Receipt, deviceID string, client *whatsmeow.Client) {
	sendReceipt := false
	switch evt.Type {
	case types.ReceiptTypeRead, types.ReceiptTypeReadSelf:
		sendReceipt = true
		log.Infof("%v was read by %s at %s: %+v", evt.MessageIDs, evt.SourceString(), evt.Timestamp, evt)
	case types.ReceiptTypeDelivered:
		sendReceipt = true
		log.Infof("%s was delivered to %s at %s: %+v", evt.MessageIDs[0], evt.SourceString(), evt.Timestamp, evt)
	}

	// Forward receipt (ack) event to webhook if configured
	// Note: Receipt events are not rate limited as they are critical for message delivery status
	if len(config.WhatsappWebhook) > 0 && sendReceipt {
		go func(e *events.Receipt, c *whatsmeow.Client) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardReceiptToWebhook(webhookCtx, e, deviceID, c); err != nil {
				logrus.Errorf("Failed to forward ack event to webhook: %v", err)
			}
		}(evt, client)
	}
}

func handlePresence(_ context.Context, evt *events.Presence) {
	if evt.Unavailable {
		if evt.LastSeen.IsZero() {
			log.Infof("%s is now offline", evt.From)
		} else {
			log.Infof("%s is now offline (last seen: %s)", evt.From, evt.LastSeen)
		}
	} else {
		log.Infof("%s is now online", evt.From)
	}
}

func handleAppState(_ context.Context, evt *events.AppState) {
	log.Debugf("App state event: %+v / %+v", evt.Index, evt.SyncActionValue)
}

func handleGroupInfo(ctx context.Context, evt *events.GroupInfo, deviceID string, client *whatsmeow.Client) {
	// Only process events that have actual changes
	hasChanges := len(evt.Join) > 0 || len(evt.Leave) > 0 || len(evt.Promote) > 0 || len(evt.Demote) > 0 ||
		evt.Name != nil || evt.Topic != nil || evt.Locked != nil || evt.Announce != nil

	if !hasChanges {
		return
	}

	// Log group events for debugging
	if len(evt.Join) > 0 {
		log.Infof("Group %s: %d users joined at %s", evt.JID, len(evt.Join), evt.Timestamp)
	}
	if len(evt.Leave) > 0 {
		log.Infof("Group %s: %d users left at %s", evt.JID, len(evt.Leave), evt.Timestamp)
	}
	if len(evt.Promote) > 0 {
		log.Infof("Group %s: %d users promoted at %s", evt.JID, len(evt.Promote), evt.Timestamp)
	}
	if len(evt.Demote) > 0 {
		log.Infof("Group %s: %d users demoted at %s", evt.JID, len(evt.Demote), evt.Timestamp)
	}

	// Forward group info event to webhook if configured
	if len(config.WhatsappWebhook) > 0 {
		go func(e *events.GroupInfo, c *whatsmeow.Client) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardGroupInfoToWebhook(webhookCtx, e, deviceID, c); err != nil {
				logrus.Errorf("Failed to forward group info event to webhook: %v", err)
			}
		}(evt, client)
	}
}
