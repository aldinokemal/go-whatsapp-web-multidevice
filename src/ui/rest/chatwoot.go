package rest

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type ChatwootHandler struct {
	AppUsecase      domainApp.IAppUsecase
	MessageUsecase  domainMessage.IMessageUsecase
	SendUsecase     domainSend.ISendUsecase
	DeviceManager   *whatsapp.DeviceManager
	ChatStorageRepo domainChatStorage.IChatStorageRepository
}

func NewChatwootHandler(
	appUsecase domainApp.IAppUsecase,
	sendUsecase domainSend.ISendUsecase,
	messageUsecase domainMessage.IMessageUsecase,
	dm *whatsapp.DeviceManager,
	chatStorageRepo domainChatStorage.IChatStorageRepository,
) *ChatwootHandler {
	return &ChatwootHandler{
		AppUsecase:      appUsecase,
		MessageUsecase:  messageUsecase,
		SendUsecase:     sendUsecase,
		DeviceManager:   dm,
		ChatStorageRepo: chatStorageRepo,
	}
}

// composeOutgoingText prepares a Chatwoot agent reply for WhatsApp delivery:
// it translates Chatwoot/GFM markdown (**bold**, *italic*, ~~strike~~) into
// WhatsApp's syntax, and — when CHATWOOT_SIGN_MSG is enabled — prefixes the
// agent's name (joined by CHATWOOT_SIGN_DELIMITER, with literal "\n" escapes
// expanded). Returns "" when the reply has no text body.
func composeOutgoingText(payload chatwoot.WebhookPayload) string {
	text := utils.ChatwootToWhatsAppMarkdown(payload.Content)
	if config.ChatwootSignMsg && text != "" {
		if agent := strings.TrimSpace(payload.Sender.Name); agent != "" {
			delimiter := strings.ReplaceAll(config.ChatwootSignDelimiter, "\\n", "\n")
			text = "*" + agent + "*" + delimiter + text
		}
	}
	return text
}

// isEchoOfForwardedMessage reports whether a Chatwoot webhook message is one
// this app created when mirroring an outgoing WhatsApp message. The live
// forwarder stamps such messages with source_id "WAID:<id>" (see
// webhook_forward.go), so they must never be sent back to WhatsApp. This guard
// is timing-independent: the source_id is set in the create request itself,
// unlike the in-memory sent-message cache which is only populated after the
// create call returns — so it closes the race where Chatwoot delivers the
// message_created webhook before that cache entry exists.
func isEchoOfForwardedMessage(payload chatwoot.WebhookPayload) bool {
	return strings.HasPrefix(payload.SourceID, "WAID:")
}

func chatwootPayloadDeleted(payload chatwoot.WebhookPayload) bool {
	raw, ok := payload.ContentAttributes["deleted"]
	if !ok {
		return false
	}
	switch deleted := raw.(type) {
	case bool:
		return deleted
	case string:
		return strings.EqualFold(strings.TrimSpace(deleted), "true")
	default:
		return false
	}
}

func chatwootSecretMatches(expected, candidate string) bool {
	expected = strings.TrimSpace(expected)
	candidate = strings.TrimSpace(candidate)
	if expected == "" || candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(candidate)) == 1
}

func chatwootWebhookAuthorized(c *fiber.Ctx) bool {
	secret := strings.TrimSpace(config.ChatwootWebhookSecret)
	if secret == "" {
		return true
	}

	for _, candidate := range []string{
		c.Get("X-Chatwoot-Webhook-Secret"),
		c.Get("X-Gowa-Chatwoot-Secret"),
		c.Query("secret"),
	} {
		if chatwootSecretMatches(secret, candidate) {
			return true
		}
	}

	signature := strings.TrimSpace(c.Get("X-Hub-Signature-256"))
	if signature == "" {
		return false
	}

	digest, err := utils.GetMessageDigestOrSignature(c.Body(), []byte(secret))
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to compute webhook signature: %v", err)
		return false
	}
	return chatwootSecretMatches("sha256="+digest, signature)
}

func chatwootLinkChatJID(destination string) string {
	clean := utils.CleanPhoneForWhatsApp(destination)
	if clean == "" {
		return ""
	}
	if strings.Contains(clean, "@") {
		return clean
	}
	return clean + config.WhatsappTypeUser
}

// resolveSendDestination computes the WhatsApp send target from a Chatwoot
// contact's stored destination (a JID or phone number). Group (@g.us) and
// privacy-masked (@lid) JIDs are returned verbatim so the send layer's JID
// parsing (ParseJID) and LID resolution (ValidateJidWithLogin) can handle them;
// a plain @s.whatsapp.net JID is reduced to its bare phone number, and a bare
// phone number passes through unchanged.
//
// Stripping the suffix for every non-group JID (the previous behavior) turned
// an @lid destination into a bare LID id, which ParseJID then misrouted into
// the @s.whatsapp.net space — never reaching ResolveLIDToPhone and breaking
// every agent reply to an @lid contact.
func resolveSendDestination(destination string) (sendDestination string, isGroup bool) {
	isGroup = utils.IsGroupJID(destination)
	sendDestination = utils.CleanPhoneForWhatsApp(destination)
	if isGroup {
		return sendDestination, true
	}
	if strings.HasSuffix(sendDestination, config.WhatsappTypeUser) {
		sendDestination = utils.ExtractPhoneFromJID(sendDestination)
	}
	return sendDestination, false
}

func chatwootStorageDeviceID(instance *whatsapp.DeviceInstance, fallback string) string {
	if instance == nil {
		return fallback
	}
	if jid := instance.JID(); jid != "" {
		return jid
	}
	return fallback
}

type chatwootWebhookRoute struct {
	DeviceID    string
	Destination string
}

func chatwootContactAttrString(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	val, ok := attrs[key]
	if !ok {
		return ""
	}
	strVal, ok := val.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(strVal)
}

func (h *ChatwootHandler) resolveChatwootWebhookRoute(payload chatwoot.WebhookPayload) chatwootWebhookRoute {
	if h != nil && h.ChatStorageRepo != nil && payload.Conversation.ID != 0 {
		link, err := h.ChatStorageRepo.GetLatestChatwootMessageLinkByConversation(payload.Conversation.ID)
		if err != nil {
			logrus.Errorf("Chatwoot Webhook: Failed to lookup conversation route %d: %v", payload.Conversation.ID, err)
		} else if link != nil && strings.TrimSpace(link.DeviceID) != "" && strings.TrimSpace(link.WhatsAppChatJID) != "" {
			return chatwootWebhookRoute{
				DeviceID:    strings.TrimSpace(link.DeviceID),
				Destination: strings.TrimSpace(link.WhatsAppChatJID),
			}
		}
	}

	contact := payload.Conversation.Meta.Sender
	route := chatwootWebhookRoute{
		DeviceID:    config.ChatwootDeviceID,
		Destination: chatwootContactAttrString(contact.CustomAttributes, "gowa_whatsapp_jid"),
	}
	if deviceID := chatwootContactAttrString(contact.CustomAttributes, "gowa_device_id"); deviceID != "" {
		route.DeviceID = deviceID
	}
	if route.Destination == "" {
		route.Destination = contact.PhoneNumber
	}
	return route
}

func (h *ChatwootHandler) HandleWebhook(c *fiber.Ctx) error {
	if !chatwootWebhookAuthorized(c) {
		logrus.Warn("Chatwoot Webhook: Rejected request with invalid secret")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	logrus.Debugf("Chatwoot Webhook raw body: %s", string(c.Body()))

	var payload chatwoot.WebhookPayload
	if err := c.BodyParser(&payload); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}

	contact := payload.Conversation.Meta.Sender
	logrus.Debugf("Chatwoot Webhook: event=%s message_type=%s contact_id=%d contact_phone=%s",
		payload.Event, payload.MessageType, contact.ID, contact.PhoneNumber)

	if payload.Event != "message_created" {
		if payload.Event == "message_updated" && chatwootPayloadDeleted(payload) {
			return h.handleDeletedChatwootMessage(c, payload)
		}
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

	// Belt-and-suspenders echo guard: our live forwarder stamps mirrored
	// outgoing messages with source_id "WAID:<id>". Skipping them here closes
	// the race where Chatwoot delivers this webhook before MarkMessageAsSent
	// has recorded the new message id in the in-memory cache above.
	if isEchoOfForwardedMessage(payload) {
		logrus.Debugf("Chatwoot Webhook: Skipping echo message %d (source_id=%s)", payload.ID, payload.SourceID)
		return c.SendStatus(fiber.StatusOK)
	}

	// Resolve device only after the webhook is known to be a real Chatwoot
	// agent reply. Non-message/status webhooks should not fail just because the
	// WhatsApp device is offline.
	route := h.resolveChatwootWebhookRoute(payload)
	if h.DeviceManager == nil {
		err := fmt.Errorf("device manager not initialized")
		logrus.Errorf("Chatwoot Webhook: Failed to resolve device: %v", err)
		h.notifySendFailure(payload, err)
		return c.SendStatus(fiber.StatusOK)
	}
	instance, resolvedID, err := h.DeviceManager.ResolveDevice(route.DeviceID)
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to resolve device: %v", err)
		h.notifySendFailure(payload, fmt.Errorf("no WhatsApp device available: %w", err))
		return c.SendStatus(fiber.StatusOK)
	}
	logrus.Debugf("Chatwoot Webhook: Using device %s", resolvedID)
	storageDeviceID := chatwootStorageDeviceID(instance, resolvedID)

	// Build the device-bearing context once and reuse it for the send
	// operations below. The send usecases resolve the WhatsApp client from this
	// context (whatsapp.ClientFromContext), so without it the reply goes out
	// from the global default device instead of the routed one — a cross-account
	// mis-delivery in multi-device deployments. It is also stored on the fiber
	// user-context because the read/revoke paths read c.UserContext().
	ctx := whatsapp.ContextWithDevice(c.UserContext(), instance)
	c.SetUserContext(ctx)

	destination := route.Destination
	if destination == "" {
		logrus.Warnf("Chatwoot Webhook: No destination phone for contact ID %d", contact.ID)
		return c.SendStatus(fiber.StatusOK)
	}

	linkChatJID := chatwootLinkChatJID(destination)

	// Resolve the WhatsApp send target. Groups (@g.us) and privacy-masked (@lid)
	// JIDs are preserved verbatim so the send layer's JID parsing and LID
	// resolution can handle them; only a plain @s.whatsapp.net JID is reduced to
	// its bare phone number.
	sendDestination, isGroup := resolveSendDestination(destination)

	logrus.Debugf("Chatwoot Webhook: Sending to destination=%s isGroup=%v", sendDestination, isGroup)

	// Translate Chatwoot markdown to WhatsApp formatting and apply the optional
	// agent signature once, so both the attachment caption and the text path use
	// the same composed body.
	outgoingText := composeOutgoingText(payload)

	// Handle attachments if present
	if len(payload.Attachments) > 0 {
		sentAny := false
		for i, attachment := range payload.Attachments {
			// The caption belongs to the message as a whole, so it rides on the
			// first attachment only — otherwise a multi-file reply repeats the
			// same caption under every file.
			caption := ""
			if i == 0 {
				caption = outgoingText
			}
			resp, err := h.handleAttachment(ctx, sendDestination, attachment, caption)
			if err != nil {
				logrus.Errorf("Chatwoot Webhook: Failed to send attachment %d: %v", attachment.ID, err)
				h.notifySendFailure(payload, fmt.Errorf("attachment %d failed: %w", attachment.ID, err))
				continue
			}
			sentAny = true
			h.storeChatwootOutboundLink(storageDeviceID, linkChatJID, payload, resp.MessageID)
		}
		if sentAny {
			h.markLatestInboundAsRead(c, storageDeviceID, linkChatJID)
		}
		// Return early after sending attachments - caption was already included
		return c.SendStatus(fiber.StatusOK)
	}

	// If content is present (and not just an attachment caption), send it as text
	if outgoingText != "" {
		req := domainSend.MessageRequest{
			Message: outgoingText,
		}
		req.Phone = sendDestination

		resp, err := h.SendUsecase.SendText(ctx, req)
		if err != nil {
			// Log with more context but still return 200 to prevent Chatwoot retries
			logrus.WithFields(logrus.Fields{
				"destination": sendDestination,
				"is_group":    isGroup,
				"error":       err.Error(),
			}).Error("Chatwoot Webhook: Failed to send message (returning 200 to prevent retry)")
			h.notifySendFailure(payload, err)
			return c.SendStatus(fiber.StatusOK)
		}
		logrus.Infof("Chatwoot Webhook: Sent text message to %s", sendDestination)
		h.storeChatwootOutboundLink(storageDeviceID, linkChatJID, payload, resp.MessageID)
		h.markLatestInboundAsRead(c, storageDeviceID, linkChatJID)
	}

	return c.SendStatus(fiber.StatusOK)
}

func (h *ChatwootHandler) handleDeletedChatwootMessage(c *fiber.Ctx, payload chatwoot.WebhookPayload) error {
	if !config.ChatwootMessageDelete {
		return c.SendStatus(fiber.StatusOK)
	}
	if h.MessageUsecase == nil || h.ChatStorageRepo == nil || h.DeviceManager == nil {
		logrus.Warn("Chatwoot Webhook: Cannot handle deleted message without message usecase, storage, and device manager")
		return c.SendStatus(fiber.StatusOK)
	}

	route := h.resolveChatwootWebhookRoute(payload)
	instance, resolvedID, err := h.DeviceManager.ResolveDevice(route.DeviceID)
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to resolve device for delete: %v", err)
		return c.SendStatus(fiber.StatusOK)
	}

	storageDeviceID := chatwootStorageDeviceID(instance, resolvedID)
	link, err := h.ChatStorageRepo.GetChatwootMessageLinkByChatwootID(storageDeviceID, payload.ID)
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to lookup deleted Chatwoot message %d: %v", payload.ID, err)
		return c.SendStatus(fiber.StatusOK)
	}
	if link == nil {
		return c.SendStatus(fiber.StatusOK)
	}
	if link.Direction != "outgoing" {
		logrus.Debugf("Chatwoot Webhook: Not revoking inbound WhatsApp message %s for Chatwoot delete %d", link.WhatsAppMessageID, payload.ID)
		return c.SendStatus(fiber.StatusOK)
	}

	ctx := whatsapp.ContextWithDevice(c.UserContext(), instance)
	if _, err := h.MessageUsecase.RevokeMessage(ctx, domainMessage.RevokeRequest{
		MessageID: link.WhatsAppMessageID,
		Phone:     link.WhatsAppChatJID,
	}); err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to revoke WhatsApp message %s for Chatwoot delete %d: %v", link.WhatsAppMessageID, payload.ID, err)
		h.notifySendFailure(payload, err)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *ChatwootHandler) storeChatwootOutboundLink(deviceID, chatJID string, payload chatwoot.WebhookPayload, waMessageID string) {
	if h.ChatStorageRepo == nil || deviceID == "" || chatJID == "" || payload.ID == 0 || waMessageID == "" {
		return
	}
	if err := h.ChatStorageRepo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID:                     deviceID,
		WhatsAppMessageID:            waMessageID,
		WhatsAppChatJID:              chatJID,
		ChatwootMessageID:            payload.ID,
		ChatwootConversationID:       payload.Conversation.ID,
		ChatwootInboxID:              config.ChatwootInboxID,
		ChatwootContactInboxSourceID: chatJID,
		SourceID:                     payload.SourceID,
		Direction:                    "outgoing",
		IsRead:                       true,
		CreatedAt:                    time.Now(),
	}); err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to store outbound message link for Chatwoot %d / WhatsApp %s: %v", payload.ID, waMessageID, err)
	}
}

func (h *ChatwootHandler) markLatestInboundAsRead(c *fiber.Ctx, deviceID, chatJID string) {
	if !config.ChatwootMessageRead || h.MessageUsecase == nil || h.ChatStorageRepo == nil || chatJID == "" {
		return
	}

	link, err := h.ChatStorageRepo.GetLatestUnreadChatwootMessageLinkByChat(deviceID, chatJID)
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to lookup latest unread message for %s: %v", chatJID, err)
		return
	}
	if link == nil {
		return
	}

	if _, err := h.MessageUsecase.MarkAsRead(c.UserContext(), domainMessage.MarkAsReadRequest{
		MessageID: link.WhatsAppMessageID,
		Phone:     link.WhatsAppChatJID,
	}); err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to mark WhatsApp message %s read: %v", link.WhatsAppMessageID, err)
		return
	}
	link.IsRead = true
	if err := h.ChatStorageRepo.UpsertChatwootMessageLink(link); err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to persist read state for %s: %v", link.WhatsAppMessageID, err)
	}
}

func chatwootSendFailureContent(err error) string {
	content := "Message was not sent to WhatsApp."
	if err != nil {
		content += "\n\nError: " + err.Error()
	}
	return content
}

func (h *ChatwootHandler) notifySendFailure(payload chatwoot.WebhookPayload, sendErr error) {
	conversationID := payload.Conversation.ID
	if conversationID == 0 {
		logrus.Warn("Chatwoot Webhook: Cannot create send-failure note without conversation id")
		return
	}

	cwClient := chatwoot.GetDefaultClient()
	if !cwClient.IsConfigured() {
		logrus.Warn("Chatwoot Webhook: Cannot create send-failure note because Chatwoot client is not configured")
		return
	}

	if _, err := cwClient.CreateMessage(conversationID, chatwootSendFailureContent(sendErr), "outgoing", nil, chatwoot.MessageOptions{Private: true}); err != nil {
		logrus.Warnf("Chatwoot Webhook: Failed to create send-failure note: %v", err)
	}
}

func (h *ChatwootHandler) handleAttachment(ctx context.Context, phone string, att chatwoot.Attachment, caption string) (domainSend.GenericResponse, error) {
	switch att.FileType {
	case "image":
		req := domainSend.ImageRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			Caption:     caption,
			ImageURL:    &att.DataURL,
		}
		resp, err := h.SendUsecase.SendImage(ctx, req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent image attachment to %s", phone)
		}
		return resp, err

	case "audio":
		req := domainSend.AudioRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			AudioURL:    &att.DataURL,
			PTT:         true, // Send as PTT (Voice Note) for better mobile experience
		}
		resp, err := h.SendUsecase.SendAudio(ctx, req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent audio attachment to %s", phone)
			return resp, nil
		}

		logrus.Warnf("Chatwoot Webhook: Failed to send as audio (%v), retrying as file...", err)
		// Fallback to sending as file
		reqFile := domainSend.FileRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			FileURL:     &att.DataURL,
			Caption:     caption,
		}
		resp, err = h.SendUsecase.SendFile(ctx, reqFile)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent audio as file attachment to %s", phone)
		}
		return resp, err

	case "video":
		req := domainSend.VideoRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			Caption:     caption,
			VideoURL:    &att.DataURL,
		}
		resp, err := h.SendUsecase.SendVideo(ctx, req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent video attachment to %s", phone)
		}
		return resp, err

	default:
		// Default to file for other types
		req := domainSend.FileRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			FileURL:     &att.DataURL,
			Caption:     caption,
		}
		resp, err := h.SendUsecase.SendFile(ctx, req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent file attachment to %s", phone)
		}
		return resp, err
	}
}

// SyncHistory triggers a message history sync to Chatwoot
// POST /chatwoot/sync
func (h *ChatwootHandler) SyncHistory(c *fiber.Ctx) error {
	var req chatwoot.SyncRequest
	if strings.TrimSpace(string(c.Body())) != "" {
		if err := c.BodyParser(&req); err != nil {
			return utils.ResponseError(c, "Invalid payload")
		}
	}

	if req.DeviceID == "" {
		req.DeviceID = c.Query("device_id", config.ChatwootDeviceID)
	}
	if req.DaysLimit <= 0 {
		req.DaysLimit = c.QueryInt("days", config.ChatwootDaysLimitImportMessages)
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

	// Get Chatwoot client
	cwClient := chatwoot.GetDefaultClient()
	if !cwClient.IsConfigured() {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  fiber.StatusBadRequest,
			Code:    "CHATWOOT_NOT_CONFIGURED",
			Message: "Chatwoot is not configured. Set CHATWOOT_URL, CHATWOOT_API_TOKEN, CHATWOOT_ACCOUNT_ID, and CHATWOOT_INBOX_ID.",
		})
	}

	// Get or create sync service
	syncService := chatwoot.GetSyncService(cwClient, h.ChatStorageRepo)
	waClient := instance.GetClient()

	// Use JID as the storage device ID since chats are stored with the full JID
	// (e.g. "628xxx@s.whatsapp.net"), not the user-assigned device alias (e.g. "busine").
	storageDeviceID := instance.JID()
	if storageDeviceID == "" {
		storageDeviceID = resolvedID
	}

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
	if req.IncludeMedia != nil {
		opts.IncludeMedia = *req.IncludeMedia
	} else if c.Query("media") != "" {
		opts.IncludeMedia = c.QueryBool("media", opts.IncludeMedia)
	}
	if req.IncludeGroups != nil {
		opts.IncludeGroups = *req.IncludeGroups
	} else if c.Query("groups") != "" {
		opts.IncludeGroups = c.QueryBool("groups", opts.IncludeGroups)
	}

	// Start async sync
	go func() {
		ctx := context.Background()
		progress, err := syncService.SyncHistory(ctx, storageDeviceID, waClient, opts)
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
