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
	"github.com/gofiber/fiber/v3"
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

func chatwootWebhookAuthorized(c fiber.Ctx) bool {
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
	// Scope of the Chatwoot side this reply belongs to, used to store the
	// outbound link with the right config/account so future reverse lookups are
	// account-scoped. ConfigID 0 means the legacy/env config.
	ConfigID  int64
	AccountID int
	InboxID   int
	// Unroutable marks a payload that resolved to no device in per-device mode.
	// Delivery must be dropped (unless a forced per-device route overrides it):
	// an empty DeviceID would otherwise fall through to
	// DeviceManager.ResolveDevice's default-device fallback and send the reply
	// from an arbitrary device — the cross-account mis-delivery fail-fast exists
	// to prevent.
	Unroutable bool
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

// resolveChatwootWebhookRoute resolves which WhatsApp device (and destination)
// a Chatwoot agent reply should be sent from. Resolution order, safest first:
//  1. Account-scoped conversation link (handles replies to existing conversations).
//  2. Contact custom attribute gowa_device_id (explicit operator override).
//  3. Inbox+account reverse map (agent-initiated conversations).
//  4. Env config.ChatwootDeviceID — ONLY while no per-device config rows exist
//     (legacy single-device mode). Otherwise the device is left empty so an
//     unmapped conversation fails-fast instead of misrouting to the wrong inbox.
func (h *ChatwootHandler) resolveChatwootWebhookRoute(payload chatwoot.WebhookPayload, forced *chatwootWebhookRoute) chatwootWebhookRoute {
	route := chatwootWebhookRoute{
		AccountID: payload.Account.ID,
		InboxID:   payload.Conversation.InboxID,
	}

	// Legacy single-account mode also unlocks the account-id=0 wildcard for the
	// conversation lookup below (and the env device fallback in step 4). In
	// per-device mode it must stay false, or a colliding conversation id from
	// another account could match a legacy (account 0) link and misroute.
	legacy := h.legacyChatwootMode()

	// 1) Account-scoped conversation link. On the per-device endpoint the link
	// must additionally belong to the device's own config: two separate Chatwoot
	// servers can collide on (conversation_id, account_id) — fresh installs all
	// start at account 1, conversation 1 — and an unscoped match would take the
	// destination chat JID from the other server's conversation.
	if h != nil && h.ChatStorageRepo != nil && payload.Conversation.ID != 0 {
		var scopeConfigID int64
		if forced != nil {
			scopeConfigID = forced.ConfigID
		}
		link, err := h.ChatStorageRepo.GetLatestChatwootMessageLinkByConversation(payload.Conversation.ID, payload.Account.ID, legacy, scopeConfigID)
		if err != nil {
			logrus.Errorf("Chatwoot Webhook: Failed to lookup conversation route %d: %v", payload.Conversation.ID, err)
		} else if link != nil && strings.TrimSpace(link.DeviceID) != "" && strings.TrimSpace(link.WhatsAppChatJID) != "" {
			route.DeviceID = strings.TrimSpace(link.DeviceID)
			route.Destination = strings.TrimSpace(link.WhatsAppChatJID)
			route.ConfigID = link.ChatwootConfigID
			if link.ChatwootAccountID != 0 {
				route.AccountID = link.ChatwootAccountID
			}
			return route
		}
	}

	// Destination is needed regardless of how the device is resolved below.
	contact := payload.Conversation.Meta.Sender
	route.Destination = chatwootContactAttrString(contact.CustomAttributes, "gowa_whatsapp_jid")
	if route.Destination == "" {
		if contact.PhoneNumber != "" {
			route.Destination = contact.PhoneNumber
		} else if contact.Identifier != "" {
			route.Destination = contact.Identifier
		}
	}

	// 2) Explicit contact override. In per-device mode the named device must be
	// bound to the payload's own account+inbox: contact attributes are editable
	// by any agent of that Chatwoot account, so an unchecked override would let
	// account A send from a device belonging to account B.
	if deviceID := chatwootContactAttrString(contact.CustomAttributes, "gowa_device_id"); deviceID != "" {
		if legacy || h.deviceMatchesPayloadScope(deviceID, payload) {
			route.DeviceID = deviceID
			return route
		}
		logrus.Warnf("Chatwoot Webhook: ignoring gowa_device_id %q: device is not configured for account %d inbox %d",
			deviceID, payload.Account.ID, payload.Conversation.InboxID)
	}

	// 3) Inbox+account reverse map (agent-initiated conversation with no link yet).
	if reg := chatwoot.GetClientRegistry(); reg != nil {
		if rc, err := reg.ResolveByInbox(payload.Account.ID, payload.Conversation.InboxID); err != nil {
			logrus.Errorf("Chatwoot Webhook: inbox reverse lookup failed (account=%d inbox=%d): %v", payload.Account.ID, payload.Conversation.InboxID, err)
		} else if rc != nil {
			route.DeviceID = rc.DeviceID
			route.ConfigID = rc.ConfigID
			return route
		}
	}

	// 4) Legacy env fallback, only while there are no per-device configs.
	if legacy {
		route.DeviceID = config.ChatwootDeviceID
	} else {
		logrus.Warnf("Chatwoot Webhook: no device mapping for conversation %d (account=%d inbox=%d); failing fast to avoid cross-inbox delivery",
			payload.Conversation.ID, payload.Account.ID, payload.Conversation.InboxID)
		route.Unroutable = true
	}
	return route
}

// deviceMatchesPayloadScope reports whether deviceID has a per-device config
// bound to the payload's account and inbox. Used to validate the
// gowa_device_id contact-attribute override in per-device mode.
func (h *ChatwootHandler) deviceMatchesPayloadScope(deviceID string, payload chatwoot.WebhookPayload) bool {
	rc := h.resolveChatwootForDevice(deviceID)
	return rc != nil && rc.Client != nil &&
		rc.Client.AccountID == payload.Account.ID && rc.Client.InboxID == payload.Conversation.InboxID
}

// legacyChatwootMode reports whether the integration is in single-device env
// mode (no per-device config rows). Only then may env config be used at routing
// time. On a storage error it conservatively returns false (fail-fast).
func (h *ChatwootHandler) legacyChatwootMode() bool {
	if h == nil || h.ChatStorageRepo == nil {
		return true
	}
	count, err := h.ChatStorageRepo.CountChatwootDeviceConfigs()
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: failed to count device configs: %v", err)
		return false
	}
	return count == 0
}

// resolveChatwootForDevice resolves the Chatwoot client/config for a device id
// (or JID) via the registry. Returns nil when the registry is uninitialized or
// the device has no usable config.
func (h *ChatwootHandler) resolveChatwootForDevice(deviceID string) *chatwoot.ResolvedConfig {
	reg := chatwoot.GetClientRegistry()
	if reg == nil {
		return nil
	}
	rc, err := reg.Resolve(deviceID)
	if err != nil {
		logrus.Errorf("Chatwoot: failed to resolve client for device %s: %v", deviceID, err)
		return nil
	}
	return rc
}

// HandleWebhook is the shared (legacy / single-webhook) Chatwoot endpoint. The
// device is resolved from the payload (conversation link, contact attrs, inbox
// map, or env fallback).
func (h *ChatwootHandler) HandleWebhook(c fiber.Ctx) error {
	if !chatwootWebhookAuthorized(c) {
		logrus.Warn("Chatwoot Webhook: Rejected request with invalid secret")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	logrus.Debugf("Chatwoot Webhook raw body: %s", string(c.Body()))

	var payload chatwoot.WebhookPayload
	if err := c.Bind().Body(&payload); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}
	return h.processChatwootWebhook(c, payload, nil)
}

// HandleDeviceWebhook is the per-device endpoint (/chatwoot/webhook/:device_id)
// provisioned on each device's Chatwoot inbox. It is route-BY-config, not
// trust-by-path: in addition to the shared secret it verifies that the payload's
// account and inbox match the device's configured account and inbox, so a
// webhook for one device cannot be delivered through another device's path.
func (h *ChatwootHandler) HandleDeviceWebhook(c fiber.Ctx) error {
	if !chatwootWebhookAuthorized(c) {
		logrus.Warn("Chatwoot Webhook: Rejected per-device request with invalid secret")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	var payload chatwoot.WebhookPayload
	if err := c.Bind().Body(&payload); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}

	// Only message events carry the account/inbox fields the route-by-config
	// check below relies on. Everything else (conversation_*, webhook
	// verification pings, ...) is ignored by processChatwootWebhook anyway, so
	// acknowledge it here instead of 401-ing on missing fields.
	if payload.Event != "message_created" && payload.Event != "message_updated" {
		return c.SendStatus(fiber.StatusOK)
	}

	// Resolve the path device id against known devices (it may be an alias or a
	// JID). Unknown ids are acknowledged without processing — this endpoint can
	// be reached unauthenticated, so it must neither leak which device ids exist
	// nor grow the client-registry cache with arbitrary identifiers.
	deviceID := strings.TrimSpace(c.Params("device_id"))
	if h.DeviceManager != nil {
		resolved, ok := h.resolveConfigDeviceID(c)
		if !ok {
			logrus.Warnf("Chatwoot Webhook: unknown device %q on per-device webhook", deviceID)
			return c.SendStatus(fiber.StatusOK)
		}
		deviceID = resolved
	}
	rc := h.resolveChatwootForDevice(deviceID)
	if rc == nil || rc.Client == nil {
		logrus.Warnf("Chatwoot Webhook: no Chatwoot config for device %q on per-device webhook", deviceID)
		return c.SendStatus(fiber.StatusOK)
	}

	// Route-by-config check: reject payloads whose account/inbox do not match
	// this device's configured destination.
	if payload.Account.ID != rc.Client.AccountID || payload.Conversation.InboxID != rc.Client.InboxID {
		logrus.Warnf("Chatwoot Webhook: payload account/inbox (%d/%d) does not match device %q config (%d/%d); rejecting",
			payload.Account.ID, payload.Conversation.InboxID, deviceID, rc.Client.AccountID, rc.Client.InboxID)
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	forced := &chatwootWebhookRoute{
		DeviceID:  rc.DeviceID,
		ConfigID:  rc.ConfigID,
		AccountID: rc.Client.AccountID,
		InboxID:   rc.Client.InboxID,
	}
	return h.processChatwootWebhook(c, payload, forced)
}

// processChatwootWebhook performs the event gating shared by both webhook
// endpoints, then delivers an agent reply using either the forced route (when
// set by the per-device endpoint) or one resolved from the payload.
func (h *ChatwootHandler) processChatwootWebhook(c fiber.Ctx, payload chatwoot.WebhookPayload, forced *chatwootWebhookRoute) error {
	contact := payload.Conversation.Meta.Sender
	logrus.Debugf("Chatwoot Webhook: event=%s message_type=%s contact_id=%d contact_phone=%s",
		payload.Event, payload.MessageType, contact.ID, contact.PhoneNumber)

	if payload.Event != "message_created" {
		if payload.Event == "message_updated" && chatwootPayloadDeleted(payload) {
			return h.handleDeletedChatwootMessage(c, payload, forced)
		}
		return c.SendStatus(fiber.StatusOK)
	}

	if payload.MessageType != "outgoing" {
		return c.SendStatus(fiber.StatusOK)
	}

	if payload.Private {
		return c.SendStatus(fiber.StatusOK)
	}

	if chatwoot.IsMessageSentByUs(payload.Account.ID, payload.ID) {
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

	// Resolve the destination from the payload, then let the forced route (if
	// any) override the device/config scope.
	route := h.resolveChatwootWebhookRoute(payload, forced)
	if forced != nil {
		route.DeviceID = forced.DeviceID
		route.ConfigID = forced.ConfigID
		route.AccountID = forced.AccountID
		route.InboxID = forced.InboxID
		route.Unroutable = false
	}
	if route.Unroutable {
		// Fail-fast for real: without this, the empty DeviceID would resolve to
		// the default device below and the reply would go out from the wrong
		// WhatsApp account.
		return c.SendStatus(fiber.StatusOK)
	}
	return h.deliverChatwootReply(c, payload, route, contact)
}

func (h *ChatwootHandler) deliverChatwootReply(c fiber.Ctx, payload chatwoot.WebhookPayload, route chatwootWebhookRoute, contact chatwoot.Contact) error {
	if h.DeviceManager == nil {
		err := fmt.Errorf("device manager not initialized")
		logrus.Errorf("Chatwoot Webhook: Failed to resolve device: %v", err)
		h.notifySendFailure(payload, route, err)
		return c.SendStatus(fiber.StatusOK)
	}
	instance, resolvedID, err := h.DeviceManager.ResolveDevice(route.DeviceID)
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to resolve device: %v", err)
		h.notifySendFailure(payload, route, fmt.Errorf("no WhatsApp device available: %w", err))
		return c.SendStatus(fiber.StatusOK)
	}
	logrus.Debugf("Chatwoot Webhook: Using device %s", resolvedID)
	storageDeviceID := chatwootStorageDeviceID(instance, resolvedID)

	// Build the device-bearing context once and reuse it for the send
	// operations below. The send usecases resolve the WhatsApp client from this
	// context (whatsapp.ClientFromContext), so without it the reply goes out
	// from the global default device instead of the routed one — a cross-account
	// mis-delivery in multi-device deployments. It is also stored on the Fiber
	// request context because the read/revoke paths read c.Context().
	ctx := whatsapp.ContextWithDevice(c.Context(), instance)
	c.SetContext(ctx)

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
				h.notifySendFailure(payload, route, fmt.Errorf("attachment %d failed: %w", attachment.ID, err))
				continue
			}
			sentAny = true
			h.storeChatwootOutboundLink(route, storageDeviceID, linkChatJID, payload, resp.MessageID)
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
			h.notifySendFailure(payload, route, err)
			return c.SendStatus(fiber.StatusOK)
		}
		logrus.Infof("Chatwoot Webhook: Sent text message to %s", sendDestination)
		h.storeChatwootOutboundLink(route, storageDeviceID, linkChatJID, payload, resp.MessageID)
		h.markLatestInboundAsRead(c, storageDeviceID, linkChatJID)
	}

	return c.SendStatus(fiber.StatusOK)
}

func (h *ChatwootHandler) handleDeletedChatwootMessage(c fiber.Ctx, payload chatwoot.WebhookPayload, forced *chatwootWebhookRoute) error {
	if !config.ChatwootMessageDelete {
		return c.SendStatus(fiber.StatusOK)
	}
	if h.MessageUsecase == nil || h.ChatStorageRepo == nil || h.DeviceManager == nil {
		logrus.Warn("Chatwoot Webhook: Cannot handle deleted message without message usecase, storage, and device manager")
		return c.SendStatus(fiber.StatusOK)
	}

	route := h.resolveChatwootWebhookRoute(payload, forced)
	if forced != nil {
		// The delete path only needs the device to revoke from; the per-device
		// endpoint forces it so revokes route to the same device as sends.
		route.DeviceID = forced.DeviceID
		route.Unroutable = false
	}
	if route.Unroutable {
		return c.SendStatus(fiber.StatusOK)
	}
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

	ctx := whatsapp.ContextWithDevice(c.Context(), instance)
	if _, err := h.MessageUsecase.RevokeMessage(ctx, domainMessage.RevokeRequest{
		MessageID: link.WhatsAppMessageID,
		Phone:     link.WhatsAppChatJID,
	}); err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to revoke WhatsApp message %s for Chatwoot delete %d: %v", link.WhatsAppMessageID, payload.ID, err)
		h.notifySendFailure(payload, route, err)
	}
	return c.SendStatus(fiber.StatusOK)
}

func (h *ChatwootHandler) storeChatwootOutboundLink(route chatwootWebhookRoute, deviceID, chatJID string, payload chatwoot.WebhookPayload, waMessageID string) {
	if h.ChatStorageRepo == nil || deviceID == "" || chatJID == "" || payload.ID == 0 || waMessageID == "" {
		return
	}
	// Record the Chatwoot scope (config/account/inbox) this reply belongs to so
	// future reverse lookups are account-scoped. Fall back to the payload's inbox
	// when the route did not carry one (legacy single-account mode).
	inboxID := route.InboxID
	if inboxID == 0 {
		inboxID = payload.Conversation.InboxID
	}
	if err := h.ChatStorageRepo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID:                     deviceID,
		WhatsAppMessageID:            waMessageID,
		WhatsAppChatJID:              chatJID,
		ChatwootMessageID:            payload.ID,
		ChatwootConversationID:       payload.Conversation.ID,
		ChatwootInboxID:              inboxID,
		ChatwootContactInboxSourceID: chatJID,
		SourceID:                     payload.SourceID,
		Direction:                    "outgoing",
		IsRead:                       true,
		CreatedAt:                    time.Now(),
		ChatwootConfigID:             route.ConfigID,
		ChatwootAccountID:            route.AccountID,
	}); err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to store outbound message link for Chatwoot %d / WhatsApp %s: %v", payload.ID, waMessageID, err)
	}
}

func (h *ChatwootHandler) markLatestInboundAsRead(c fiber.Ctx, deviceID, chatJID string) {
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

	if _, err := h.MessageUsecase.MarkAsRead(c.Context(), domainMessage.MarkAsReadRequest{
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

// chatwootClientForPayload returns the Chatwoot client that owns the payload's
// account/inbox, so notes are posted to the correct account in multi-account
// setups. Falls back to the env client only in legacy mode (no per-device
// configs). Returns nil when no client can be safely chosen.
func (h *ChatwootHandler) chatwootClientForPayload(payload chatwoot.WebhookPayload) *chatwoot.Client {
	if reg := chatwoot.GetClientRegistry(); reg != nil {
		if rc, err := reg.ResolveByInbox(payload.Account.ID, payload.Conversation.InboxID); err == nil && rc != nil && rc.Client != nil {
			return rc.Client
		}
	}
	if h.legacyChatwootMode() {
		return chatwoot.NewClient()
	}
	return nil
}

func (h *ChatwootHandler) notifySendFailure(payload chatwoot.WebhookPayload, route chatwootWebhookRoute, sendErr error) {
	conversationID := payload.Conversation.ID
	if conversationID == 0 {
		logrus.Warn("Chatwoot Webhook: Cannot create send-failure note without conversation id")
		return
	}

	// Prefer the routed device's own client: when the reply came through the
	// per-device endpoint the payload's (account, inbox) may be ambiguous across
	// configs (ResolveByInbox returns nil on ambiguity), which is exactly the
	// deployment shape the per-device route exists for.
	var cwClient *chatwoot.Client
	if route.DeviceID != "" {
		if rc := h.resolveChatwootForDevice(route.DeviceID); rc != nil && rc.Client != nil &&
			rc.Client.AccountID == payload.Account.ID {
			cwClient = rc.Client
		}
	}
	if cwClient == nil {
		cwClient = h.chatwootClientForPayload(payload)
	}
	if cwClient == nil || !cwClient.IsConfigured() {
		logrus.Warn("Chatwoot Webhook: Cannot create send-failure note because no Chatwoot client is configured for this account/inbox")
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
func (h *ChatwootHandler) SyncHistory(c fiber.Ctx) error {
	var req chatwoot.SyncRequest
	if strings.TrimSpace(string(c.Body())) != "" {
		if err := c.Bind().Body(&req); err != nil {
			return utils.ResponseError(c, "Invalid payload")
		}
	}

	if req.DeviceID == "" {
		req.DeviceID = c.Query("device_id", config.ChatwootDeviceID)
	}
	if req.DaysLimit <= 0 {
		req.DaysLimit = fiber.Query[int](c, "days", config.ChatwootDaysLimitImportMessages)
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

	// Resolve the per-device Chatwoot client (legacy/env client when the config
	// table is empty). Sync runs against this device's own Chatwoot destination.
	resolved := h.resolveChatwootForDevice(resolvedID)
	if resolved == nil || resolved.Client == nil || !resolved.Client.IsConfigured() {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  fiber.StatusBadRequest,
			Code:    "CHATWOOT_NOT_CONFIGURED",
			Message: "Chatwoot is not configured for this device. Set per-device config via /devices/:device_id/chatwoot/config or the CHATWOOT_* env vars.",
		})
	}

	// Get or create the per-device sync service.
	syncService := chatwoot.GetSyncServiceForDevice(chatwoot.SyncServiceKeyFor(resolved), resolved.Client, h.ChatStorageRepo, resolved.ConfigID == 0, resolved.ConfigID)
	waClient := instance.GetClient()

	// Use JID as the storage device ID since chats are stored with the full JID
	// (e.g. "628xxx@s.whatsapp.net"), not the user-assigned device alias (e.g. "busine").
	storageDeviceID := instance.JID()
	if storageDeviceID == "" {
		// resolvedID may alias the request buffer (it derives from the request
		// body/params), and this id outlives the request as the sync progress-map
		// key — copy it so the key doesn't mutate when fasthttp recycles the buffer.
		storageDeviceID = strings.Clone(resolvedID)
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
		opts.IncludeMedia = fiber.Query[bool](c, "media", opts.IncludeMedia)
	}
	if req.IncludeGroups != nil {
		opts.IncludeGroups = *req.IncludeGroups
	} else if c.Query("groups") != "" {
		opts.IncludeGroups = fiber.Query[bool](c, "groups", opts.IncludeGroups)
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
func (h *ChatwootHandler) SyncStatus(c fiber.Ctx) error {
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

	// Look up this device's sync service (legacy key when no per-device config).
	syncKey := chatwoot.SyncServiceKeyFor(h.resolveChatwootForDevice(resolvedID))
	syncService := chatwoot.LookupSyncServiceForDevice(syncKey)
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
