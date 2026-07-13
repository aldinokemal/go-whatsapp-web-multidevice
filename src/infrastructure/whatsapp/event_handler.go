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
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
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
		instance.ClearPasskeyState()
		handlePairSuccess(ctx, evt)
	case *events.PairPasskeyRequest:
		handlePairPasskeyRequest(instance, evt)
	case *events.PairPasskeyConfirmation:
		handlePairPasskeyConfirmation(instance, evt)
	case *events.PairPasskeyError:
		handlePairPasskeyError(instance, evt)
	case *events.LoggedOut:
		handleLoggedOut(instance)
	case *events.Connected, *events.PushNameSetting:
		handleConnectionEvents(ctx, client, instance)
	case *events.StreamReplaced:
		handleStreamReplaced(ctx)
	case *events.Message:
		handleMessage(ctx, evt, chatStorageRepo, client)
	case *events.Receipt:
		handleReceipt(ctx, evt, instance.JID(), client)
	case *events.Archive:
		handleArchive(ctx, evt, chatStorageRepo, client)
	case *events.Presence:
		handlePresence(ctx, evt)
	case *events.ChatPresence:
		handleChatPresence(ctx, evt, instance.JID(), client)
	case *events.HistorySync:
		handleHistorySync(ctx, evt, chatStorageRepo, client)
	case *events.AppState:
		handleAppState(ctx, evt, instance.JID(), client)
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
		handleCallOffer(ctx, evt, chatStorageRepo, instance.JID(), client)
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
	go func(c *whatsmeow.Client) {
		webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := forwardDeleteToWebhook(webhookCtx, evt, message, deviceID, c); err != nil {
			log.Errorf("Failed to forward delete event to webhook: %v", err)
		}
	}(client)
}

func resolvePresenceOnConnect() (types.Presence, bool) {
	switch config.WhatsappPresenceOnConnect {
	case "available":
		return types.PresenceAvailable, false
	case "none":
		return "", true
	default:
		return types.PresenceUnavailable, false
	}
}

func sendConfiguredPresence(ctx context.Context, client *whatsmeow.Client) {
	presence, skip := resolvePresenceOnConnect()
	if skip {
		log.Infof("Skipping presence on connect (configured: none)")
		return
	}
	if err := client.SendPresence(ctx, presence); err != nil {
		log.Warnf("Failed to send %s presence: %v", presence, err)
	} else {
		log.Infof("Marked self as %s", presence)
	}
}

func handleAppStateSyncComplete(_ context.Context, client *whatsmeow.Client, evt *events.AppStateSyncComplete) {
	if client == nil {
		return
	}
	if len(client.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
		sendConfiguredPresence(context.Background(), client)
	}
}

func handlePairSuccess(ctx context.Context, evt *events.PairSuccess) {
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "LOGIN_SUCCESS",
		Message: fmt.Sprintf("Successfully pair with %s", evt.ID.String()),
	}
	primaryDB, secondaryDB := getStoreContainers()
	syncKeysDevice(ctx, primaryDB, secondaryDB, evt.ID)
}

func handlePairPasskeyRequest(instance *DeviceInstance, evt *events.PairPasskeyRequest) {
	instance.SetPasskeyChallenge(evt.PublicKey)
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "PASSKEY_REQUEST",
		Message: "Passkey pairing requested; submit the WebAuthn assertion via POST /app/passkey/response",
		Result: map[string]any{
			"device_id": instance.ID(),
			"challenge": evt.PublicKey,
		},
	}
}

func handlePairPasskeyConfirmation(instance *DeviceInstance, evt *events.PairPasskeyConfirmation) {
	instance.SetPasskeyConfirmation(evt.Code, evt.SkipHandoffUX)
	message := fmt.Sprintf("Passkey pairing code %s: verify it matches the code on your phone, then confirm via POST /app/passkey/confirm", evt.Code)
	if evt.SkipHandoffUX {
		message = "Passkey pairing verified, finishing automatically"
	}
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "PASSKEY_CONFIRMATION",
		Message: message,
		Result: map[string]any{
			"device_id":       instance.ID(),
			"code":            evt.Code,
			"skip_handoff_ux": evt.SkipHandoffUX,
		},
	}
}

func handlePairPasskeyError(instance *DeviceInstance, evt *events.PairPasskeyError) {
	logrus.Warnf("[PASSKEY][%s] pairing error (continuation=%t): %v", instance.ID(), evt.Continuation, evt.Error)
	instance.ClearPasskeyState()
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "PASSKEY_ERROR",
		Message: evt.Error.Error(),
		Result: map[string]any{
			"device_id":    instance.ID(),
			"continuation": evt.Continuation,
		},
	}
}

func handleLoggedOut(instance *DeviceInstance) {
	logrus.Warnf("[REMOTE_LOGOUT] Received LoggedOut event for device %s - user logged out from phone", instance.ID())
	instance.ClearPasskeyState()

	if client := instance.GetClient(); client != nil {
		client.Disconnect()
	}
	instance.SetState(domainDevice.DeviceStateDisconnected)

	// Chat history is intentionally preserved on remote logout (it is only cleared on
	// a full purge via DELETE). A remote logout keeps the device slot, so truncating
	// here would contradict the keep-slot semantics and lose the conversation history.

	deviceID := instance.ID()

	// TriggerLoggedOut fires the manager's keep-slot callback (resets the in-memory
	// client + clears the persisted JID, but keeps the slot id and display name).
	instance.TriggerLoggedOut()

	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "DEVICE_LOGGED_OUT",
		Message: "Device logged out (slot kept)",
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
					ADJID:       instance.ADJID(),
					CreatedAt:   instance.CreatedAt(),
				}); err != nil {
					log.Warnf("Failed to persist device record for %s: %v", instance.ID(), err)
				}

				// Keep the Chatwoot device config's JID current. The forward path
				// resolves configs by JID, so a config created before the device
				// paired (empty device_jid) — or one gone stale after a re-pair —
				// would otherwise silently never match and every message would be
				// skipped.
				if config.ChatwootEnabled {
					changed, err := repo.UpdateChatwootDeviceConfigJID(instance.ID(), jid)
					if err != nil {
						log.Warnf("Failed to update Chatwoot config JID for %s: %v", instance.ID(), err)
					} else if changed {
						if reg := chatwoot.GetClientRegistry(); reg != nil {
							reg.Invalidate(instance.ID())
						}
					}
				}
			}
		}
	}
	// Start Chatwoot history auto-sync on first connect for this device when
	// CHATWOOT_IMPORT_MESSAGES is enabled. TriggerAutoSync self-guards on config,
	// login state, and a once-per-device latch, so it is safe to call on every
	// connect — placed before the pushname early-return because a freshly paired
	// device may connect before its pushname is known.
	if instance != nil {
		if repo := instance.GetChatStorage(); repo != nil {
			chatwoot.TriggerAutoSync(repo, client)
		}
	}

	if len(client.Store.PushName) == 0 {
		return
	}

	// Send configured presence when connecting and when the pushname is changed.
	// This makes sure that outgoing messages always have the right pushname.
	sendConfiguredPresence(context.Background(), client)
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

	// Forward receipt (ack) event to webhook or Chatwoot if configured
	// Note: Receipt events are not rate limited as they are critical for message delivery status
	if sendReceipt {
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

func handleAppState(_ context.Context, evt *events.AppState, deviceID string, client *whatsmeow.Client) {
	log.Debugf("App state event: %+v / %+v", evt.Index, evt.SyncActionValue)

	if isLabelAppState(evt) {
		go func(e *events.AppState, c *whatsmeow.Client) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardLabelAppStateToWebhook(webhookCtx, e, deviceID, c); err != nil {
				logrus.Errorf("Failed to forward label appstate event to webhook: %v", err)
			}
		}(evt, client)
	}
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

	// Forward group info event to webhook
	go func(e *events.GroupInfo, c *whatsmeow.Client) {
		webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := forwardGroupInfoToWebhook(webhookCtx, e, deviceID, c); err != nil {
			logrus.Errorf("Failed to forward group info event to webhook: %v", err)
		}
	}(evt, client)
}
