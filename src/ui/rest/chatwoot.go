package rest

import (
	"context"
	"fmt"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type ChatwootHandler struct {
	AppUsecase      domainApp.IAppUsecase
	SendUsecase     domainSend.ISendUsecase
	DeviceManager   *whatsapp.DeviceManager
	ChatStorageRepo domainChatStorage.IChatStorageRepository
}

func NewChatwootHandler(
	appUsecase domainApp.IAppUsecase,
	sendUsecase domainSend.ISendUsecase,
	dm *whatsapp.DeviceManager,
	chatStorageRepo domainChatStorage.IChatStorageRepository,
) *ChatwootHandler {
	return &ChatwootHandler{
		AppUsecase:      appUsecase,
		SendUsecase:     sendUsecase,
		DeviceManager:   dm,
		ChatStorageRepo: chatStorageRepo,
	}
}

func (h *ChatwootHandler) HandleWebhook(c *fiber.Ctx) error {
	logrus.Debugf("Chatwoot Webhook raw body: %s", string(c.Body()))

	// Parse the payload first so we can route by inbox_id (multi-device).
	var payload chatwoot.WebhookPayload
	if err := c.BodyParser(&payload); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}

	// Filter events BEFORE resolving a device so ignored events are acknowledged
	// cheaply. Only the typing indicator and outgoing, non-private, non-echo
	// message_created events are processed; everything else gets a 200 OK.
	isTyping := payload.Event == "conversation_typing_on" || payload.Event == "conversation_typing_off"
	if !isTyping {
		if payload.Event != "message_created" {
			return c.SendStatus(fiber.StatusOK)
		}
		if payload.MessageType != "outgoing" {
			return c.SendStatus(fiber.StatusOK)
		}
		if payload.Private {
			return c.SendStatus(fiber.StatusOK)
		}
		if chatwoot.IsMessageSentByUs(payload.ID) {
			logrus.Debugf("Chatwoot Webhook: Skipping echo message %d (created by our API)", payload.ID)
			return c.SendStatus(fiber.StatusOK)
		}
	}

	// Resolve device only for events we actually process. Resolution order:
	// the device mapped to this webhook's inbox, then the device_id carried on
	// the webhook URL (?device_id=<JID>, set at inbox registration), then the
	// global CHATWOOT_DEVICE_ID env default.
	var (
		instance   *whatsapp.DeviceInstance
		resolvedID string
		err        error
	)
	if reg := chatwoot.GetGlobalRegistry(); reg != nil {
		if _, deviceID, lookupErr := reg.GetClientForInbox(payload.Account.ID, payload.Conversation.InboxID); lookupErr == nil && deviceID != "" {
			instance, resolvedID, err = h.DeviceManager.ResolveDevice(deviceID)
		}
	}
	if instance == nil {
		if queryDeviceID := c.Query("device_id"); queryDeviceID != "" {
			instance, resolvedID, err = h.DeviceManager.ResolveDevice(queryDeviceID)
		}
	}
	if instance == nil {
		instance, resolvedID, err = h.DeviceManager.ResolveDevice(config.ChatwootDeviceID)
	}
	if err != nil || instance == nil {
		logrus.Errorf("Chatwoot Webhook: Failed to resolve device: %v", err)
		return c.Status(fiber.StatusServiceUnavailable).JSON(utils.ResponseData{
			Status:  fiber.StatusServiceUnavailable,
			Code:    "DEVICE_NOT_AVAILABLE",
			Message: fmt.Sprintf("No device available for Chatwoot: %v. Configure CHATWOOT_DEVICE_ID or a per-inbox mapping.", err),
		})
	}
	logrus.Debugf("Chatwoot Webhook: Using device %s (inbox %d)", resolvedID, payload.Conversation.InboxID)

	// Set device context for send operations
	c.SetUserContext(whatsapp.ContextWithDevice(c.UserContext(), instance))

	// Typing indicator: agent typing in Chatwoot -> "typing…" presence in WhatsApp,
	// emulating WhatsApp Web.
	if isTyping {
		h.handleTypingPresence(c, payload)
		return c.SendStatus(fiber.StatusOK)
	}

	// While a history sync is running for this device, it is the sole source of
	// outgoing messages (it creates them in Chatwoot, which echoes them back here).
	// Skip them to avoid re-sending — and duplicating — synced media/voice notes.
	// Keyed by JID, identical to the sync's progress key (see SyncHistory handler).
	syncDeviceID := instance.JID()
	if syncDeviceID == "" {
		syncDeviceID = resolvedID
	}
	if chatwoot.IsSyncInProgress(syncDeviceID) {
		logrus.Infof("Chatwoot Webhook: Skipping outgoing message %d — history sync in progress for device %s", payload.ID, syncDeviceID)
		return c.SendStatus(fiber.StatusOK)
	}

	contact := payload.Conversation.Meta.Sender
	logrus.Debugf("Chatwoot Webhook: event=%s message_type=%s contact_id=%d contact_phone=%s",
		payload.Event, payload.MessageType, contact.ID, contact.PhoneNumber)

	destination, isGroup := resolveWhatsAppDestination(contact)
	if destination == "" {
		logrus.Warnf("Chatwoot Webhook: No destination phone for contact ID %d", contact.ID)
		return c.SendStatus(fiber.StatusOK)
	}

	logrus.Debugf("Chatwoot Webhook: Sending to destination=%s isGroup=%v", destination, isGroup)

	// Handle attachments if present
	if len(payload.Attachments) > 0 {
		for _, attachment := range payload.Attachments {
			waMsgID, err := h.handleAttachment(c, destination, attachment, payload.Content)
			if err != nil {
				logrus.Errorf("Chatwoot Webhook: Failed to send attachment %d: %v", attachment.ID, err)
				continue
			}
			// Map the sent WhatsApp message to this Chatwoot message so later
			// delivery/read receipts can update its status (the ✓/✓✓/read ticks).
			if waMsgID != "" {
				chatwoot.TrackOutgoingMessage(waMsgID, payload.Conversation.ID, payload.ID)
			}
		}
		// Return early after sending attachments - caption was already included
		return c.SendStatus(fiber.StatusOK)
	}

	// If content is present (and not just an attachment caption), send it as text
	if payload.Content != "" {
		req := domainSend.MessageRequest{
			Message: payload.Content,
		}
		req.Phone = destination

		resp, err := h.SendUsecase.SendText(c.UserContext(), req)
		if err != nil {
			// Log with more context but still return 200 to prevent Chatwoot retries
			logrus.WithFields(logrus.Fields{
				"destination": destination,
				"is_group":    isGroup,
				"error":       err.Error(),
			}).Error("Chatwoot Webhook: Failed to send message (returning 200 to prevent retry)")
			return c.SendStatus(fiber.StatusOK)
		}
		// Track for delivery/read status sync via WhatsApp receipts.
		if resp.MessageID != "" {
			chatwoot.TrackOutgoingMessage(resp.MessageID, payload.Conversation.ID, payload.ID)
		}
		logrus.Infof("Chatwoot Webhook: Sent text message to %s", destination)
	}

	return c.SendStatus(fiber.StatusOK)
}

// handleAttachment sends one Chatwoot attachment to WhatsApp and returns the
// resulting WhatsApp message ID (for delivery/read status tracking).
func (h *ChatwootHandler) handleAttachment(c *fiber.Ctx, phone string, att chatwoot.Attachment, caption string) (string, error) {
	switch att.FileType {
	case "image":
		req := domainSend.ImageRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			Caption:     caption,
			ImageURL:    &att.DataURL,
		}
		resp, err := h.SendUsecase.SendImage(c.UserContext(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent image attachment to %s", phone)
		}
		return resp.MessageID, err

	case "audio":
		req := domainSend.AudioRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			AudioURL:    &att.DataURL,
			PTT:         true, // Send as PTT (Voice Note) for better mobile experience
		}
		resp, err := h.SendUsecase.SendAudio(c.UserContext(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent audio attachment to %s", phone)
			return resp.MessageID, nil
		}

		logrus.Warnf("Chatwoot Webhook: Failed to send as audio (%v), retrying as file...", err)
		// Fallback to sending as file
		reqFile := domainSend.FileRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			FileURL:     &att.DataURL,
			Caption:     caption,
		}
		fileResp, err := h.SendUsecase.SendFile(c.UserContext(), reqFile)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent audio as file attachment to %s", phone)
		}
		return fileResp.MessageID, err

	case "video":
		req := domainSend.VideoRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			Caption:     caption,
			VideoURL:    &att.DataURL,
		}
		resp, err := h.SendUsecase.SendVideo(c.UserContext(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent video attachment to %s", phone)
		}
		return resp.MessageID, err

	default:
		// Default to file for other types
		req := domainSend.FileRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			FileURL:     &att.DataURL,
			Caption:     caption,
		}
		resp, err := h.SendUsecase.SendFile(c.UserContext(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent file attachment to %s", phone)
		}
		return resp.MessageID, err
	}
}

// resolveWhatsAppDestination derives the WhatsApp send target from a Chatwoot
// contact: the waha_whatsapp_jid custom attribute (preferred) or phone number,
// cleaned for WhatsApp. Returns ("", false) when no usable destination exists.
func resolveWhatsAppDestination(contact chatwoot.Contact) (string, bool) {
	var destination string
	if val, ok := contact.CustomAttributes["waha_whatsapp_jid"]; ok {
		if strVal, ok := val.(string); ok {
			destination = strVal
		}
	}
	if destination == "" {
		destination = contact.PhoneNumber
	}
	if destination == "" {
		return "", false
	}

	isGroup := utils.IsGroupJID(destination)
	destination = utils.CleanPhoneForWhatsApp(destination)
	if !isGroup {
		destination = utils.ExtractPhoneFromJID(destination)
	}
	return destination, isGroup
}

// handleTypingPresence mirrors a Chatwoot agent's typing state to WhatsApp as a
// chat presence (composing/paused), so the contact sees "typing…" like on
// WhatsApp Web. Best-effort: failures are logged, never surfaced. Skips private
// notes (which must not notify the contact) and group conversations.
func (h *ChatwootHandler) handleTypingPresence(c *fiber.Ctx, payload chatwoot.WebhookPayload) {
	if payload.IsPrivate {
		return
	}
	destination, isGroup := resolveWhatsAppDestination(payload.Conversation.Meta.Sender)
	if destination == "" || isGroup {
		return
	}

	action := "start"
	if payload.Event == "conversation_typing_off" {
		action = "stop"
	}

	req := domainSend.ChatPresenceRequest{Phone: destination, Action: action}
	if _, err := h.SendUsecase.SendChatPresence(c.UserContext(), req); err != nil {
		logrus.Debugf("Chatwoot Webhook: failed to send typing presence to %s: %v", destination, err)
	}
}

// SyncHistory triggers a message history sync to Chatwoot
// POST /chatwoot/sync
func (h *ChatwootHandler) SyncHistory(c *fiber.Ctx) error {
	// Parse request body
	var req chatwoot.SyncRequest
	if err := c.BodyParser(&req); err != nil {
		// Try query parameters as fallback
		req.DeviceID = c.Query("device_id", config.ChatwootDeviceID)
		req.DaysLimit = c.QueryInt("days", config.ChatwootDaysLimitImportMessages)
		req.IncludeMedia = c.QueryBool("media", true)
		req.IncludeGroups = c.QueryBool("groups", true)
	}

	// Default values
	if req.DeviceID == "" {
		req.DeviceID = config.ChatwootDeviceID
	}
	if req.DaysLimit <= 0 {
		req.DaysLimit = config.ChatwootDaysLimitImportMessages
	}

	// Resolve device
	instance, resolvedID, err := h.DeviceManager.ResolveDevice(req.DeviceID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  fiber.StatusBadRequest,
			Code:    "DEVICE_NOT_FOUND",
			Message: fmt.Sprintf("Failed to resolve device: %v", err),
		})
	}

	waClient := instance.GetClient()

	// Use JID as the storage device ID since chats are stored with the full JID
	// (e.g. "628xxx@s.whatsapp.net"), not the user-assigned device alias (e.g. "busine").
	storageDeviceID := instance.JID()
	if storageDeviceID == "" {
		storageDeviceID = resolvedID
	}

	// Resolve the Chatwoot client for this device (per-inbox), falling back to
	// the env-var default singleton when no per-device config exists.
	cwClient := chatwoot.GetDefaultClient()
	if reg := chatwoot.GetGlobalRegistry(); reg != nil && storageDeviceID != "" {
		client, lookupErr := reg.GetClientForDevice(storageDeviceID)
		switch {
		case lookupErr != nil:
			// Lookup failed; keep the env-var default client as a fallback.
		case client == nil:
			// Device has an explicit config that is disabled: respect it instead
			// of falling back to the default client.
			return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
				Status:  fiber.StatusBadRequest,
				Code:    "CHATWOOT_DISABLED",
				Message: "Chatwoot is disabled for this device.",
			})
		default:
			cwClient = client
		}
	}
	if !cwClient.IsConfigured() {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  fiber.StatusBadRequest,
			Code:    "CHATWOOT_NOT_CONFIGURED",
			Message: "Chatwoot is not configured. Set CHATWOOT_URL, CHATWOOT_API_TOKEN, CHATWOOT_ACCOUNT_ID, and CHATWOOT_INBOX_ID.",
		})
	}

	// Get or create sync service (the client is passed per SyncHistory call)
	syncService := chatwoot.GetSyncService(h.ChatStorageRepo)

	// Check if already running
	if syncService.IsRunning(storageDeviceID) {
		progress := syncService.GetProgress(storageDeviceID)
		return c.Status(fiber.StatusConflict).JSON(utils.ResponseData{
			Status:  fiber.StatusConflict,
			Code:    "SYNC_ALREADY_RUNNING",
			Message: "A sync is already in progress for this device",
			Results: map[string]any{
				"progress": progress,
			},
		})
	}

	// Build sync options
	opts := chatwoot.DefaultSyncOptions()
	opts.DaysLimit = req.DaysLimit
	opts.IncludeMedia = req.IncludeMedia
	opts.IncludeGroups = req.IncludeGroups

	// Start async sync
	go func() {
		ctx := context.Background()
		progress, err := syncService.SyncHistory(ctx, storageDeviceID, cwClient, waClient, opts)
		if err != nil {
			logrus.Errorf("Chatwoot Sync: Failed for device %s: %v", storageDeviceID, err)
		} else {
			logrus.Infof("Chatwoot Sync: Completed for device %s - %d/%d messages synced",
				storageDeviceID, progress.SyncedMessages, progress.TotalMessages)
		}
	}()

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SYNC_STARTED",
		Message: "History sync initiated in background",
		Results: map[string]any{
			"device_id":      resolvedID,
			"days_limit":     opts.DaysLimit,
			"include_media":  opts.IncludeMedia,
			"include_groups": opts.IncludeGroups,
		},
	})
}

// SyncStatus returns the current sync progress
// GET /chatwoot/sync/status
func (h *ChatwootHandler) SyncStatus(c *fiber.Ctx) error {
	deviceID := c.Query("device_id", config.ChatwootDeviceID)

	instance, resolvedID, err := h.DeviceManager.ResolveDevice(deviceID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  fiber.StatusBadRequest,
			Code:    "DEVICE_NOT_FOUND",
			Message: fmt.Sprintf("Failed to resolve device: %v", err),
		})
	}

	storageDeviceID := instance.JID()
	if storageDeviceID == "" {
		storageDeviceID = resolvedID
	}

	syncService := chatwoot.GetDefaultSyncService()
	if syncService == nil {
		return c.JSON(utils.ResponseData{
			Status:  200,
			Code:    "SUCCESS",
			Message: "No sync has been initiated yet",
			Results: map[string]any{
				"device_id": resolvedID,
				"status":    "idle",
			},
		})
	}

	progress := syncService.GetProgress(storageDeviceID)
	if progress == nil {
		return c.JSON(utils.ResponseData{
			Status:  200,
			Code:    "SUCCESS",
			Message: "No sync progress found for this device",
			Results: map[string]any{
				"device_id": resolvedID,
				"status":    "idle",
			},
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Sync status retrieved",
		Results: progress,
	})
}
