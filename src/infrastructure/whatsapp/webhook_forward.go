package whatsapp

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainWebhook "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	webhookregistry "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/webhook"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
)

var (
	submitWebhookFn       = submitWebhook
	submitDeviceWebhookFn = submitWebhookWithOptions
)

// mutexShardCount is the number of mutex shards for contact synchronization.
// Using a fixed array avoids memory growth from sync.Map while still providing
// reasonable concurrency (64 shards means max 64 concurrent contact operations).
const mutexShardCount = 64

// contactMutexShards provides sharded locks to prevent race conditions when creating Chatwoot contacts.
// This approach prevents memory leaks that would occur with a sync.Map that grows indefinitely.
var contactMutexShards [mutexShardCount]sync.Mutex

// groupNameCacheEntry holds cached group name with expiration time
type groupNameCacheEntry struct {
	name      string
	expiresAt time.Time
}

var (
	// groupNameCache provides TTL-based caching for group names to reduce WhatsApp API calls
	groupNameCache    sync.Map
	groupNameCacheTTL = 5 * time.Minute
)

// getCachedGroupName retrieves group name from cache if not expired.
// Returns empty string and false if not cached or expired.
func getCachedGroupName(groupJID string) (string, bool) {
	if entry, ok := groupNameCache.Load(groupJID); ok {
		cached := entry.(groupNameCacheEntry)
		if time.Now().Before(cached.expiresAt) {
			return cached.name, true
		}
		// Entry expired, delete it
		groupNameCache.Delete(groupJID)
	}
	return "", false
}

// setCachedGroupName stores group name in cache with TTL.
func setCachedGroupName(groupJID, name string) {
	groupNameCache.Store(groupJID, groupNameCacheEntry{
		name:      name,
		expiresAt: time.Now().Add(groupNameCacheTTL),
	})
}

// getContactMutex returns a mutex for the given phone number to serialize contact operations.
// Uses FNV-1a hash to distribute phones across shards for balanced lock contention.
func getContactMutex(phone string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(phone))
	return &contactMutexShards[h.Sum32()%mutexShardCount]
}

// forwardPayloadToConfiguredWebhooks attempts to deliver the provided payload to every configured webhook URL.
// It only returns an error when all webhook deliveries fail. Partial failures are logged and suppressed so
// successful targets still receive the event.
func forwardPayloadToConfiguredWebhooks(ctx context.Context, payload map[string]any, eventName string) error {
	// When the receiving device owns per-device webhooks, enter the webhook path
	// regardless of the global event whitelist so per-webhook event filters are
	// authoritative for that device.
	hasDeviceWebhooks := len(deviceWebhooksForPayload(payload)) > 0
	webhookAllowed := hasDeviceWebhooks || len(config.WhatsappWebhookEvents) == 0 || isEventWhitelisted(eventName)
	chatwootAllowed := config.ChatwootEnabled && shouldForwardEventToChatwoot(eventName) && isEventWhitelistedForChatwoot(eventName)

	if !webhookAllowed && !chatwootAllowed {
		logrus.Debugf("Skipping event %s - not allowed for webhooks or Chatwoot", eventName)
		return nil
	}

	var err error
	if webhookAllowed {
		err = forwardToWebhooks(ctx, payload, eventName)
	} else {
		logrus.Debugf("Skipping event %s for configured webhooks, but allowing Chatwoot", eventName)
	}

	if chatwootAllowed {
		go forwardToChatwoot(ctx, payload, eventName)
	}

	return err
}

func forwardToWebhooks(ctx context.Context, payload map[string]any, eventName string) error {
	// Per-device webhooks take precedence: a device with its own config is routed
	// only to its targets. Devices without per-device config fall back to the
	// global WHATSAPP_WEBHOOK list below (backward compatible).
	if cfgs := deviceWebhooksForPayload(payload); len(cfgs) > 0 {
		return forwardToDeviceWebhooks(ctx, payload, eventName, cfgs)
	}

	total := len(config.WhatsappWebhook)
	logrus.Infof("Forwarding %s to %d configured webhook(s)", eventName, total)

	if total == 0 {
		return nil
	}

	var (
		failed    []string
		successes int
	)
	for _, url := range config.WhatsappWebhook {
		if err := submitWebhookFn(ctx, payload, url); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", url, err))
			logrus.Warnf("Failed forwarding %s to %s: %v", eventName, url, err)
			continue
		}
		successes++
	}

	if len(failed) > 0 {
		logrus.Warnf("Some webhook URLs failed for %s (succeeded: %d/%d): %s", eventName, successes, total, strings.Join(failed, "; "))
		// Return error only if ALL webhooks failed
		if successes == 0 {
			return fmt.Errorf("all %d webhook(s) failed for %s", total, eventName)
		}
	} else {
		logrus.Infof("%s forwarded to all webhook(s)", eventName)
	}

	return nil
}

// deviceWebhooksForPayload returns the per-device webhook configs bound to the
// device that received this event, or nil when there is no registry, no
// device_id in the payload, or no configs for the device.
func deviceWebhooksForPayload(payload map[string]any) []domainWebhook.DeviceWebhookConfig {
	reg := webhookregistry.GetGlobalRegistry()
	if reg == nil {
		return nil
	}
	deviceID, _ := payload["device_id"].(string)
	if deviceID == "" {
		return nil
	}
	return reg.GetWebhooksForDevice(deviceID)
}

// forwardToDeviceWebhooks delivers the payload to a device's own webhook targets,
// applying each target's event filter, HMAC secret and custom headers. It mirrors
// forwardToWebhooks' aggregation: an error is returned only when every eligible
// target fails. Targets whose event filter excludes this event are skipped.
func forwardToDeviceWebhooks(ctx context.Context, payload map[string]any, eventName string, cfgs []domainWebhook.DeviceWebhookConfig) error {
	var (
		failed    []string
		successes int
		eligible  int
	)
	for _, cfg := range cfgs {
		if !webhookEventAllowed(cfg.Events, eventName) {
			continue
		}
		eligible++
		if err := submitDeviceWebhookFn(ctx, payload, cfg.WebhookURL, cfg.Secret, cfg.Headers); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", cfg.WebhookURL, err))
			logrus.Warnf("Failed forwarding %s to device webhook %s: %v", eventName, cfg.WebhookURL, err)
			continue
		}
		successes++
	}

	if eligible == 0 {
		logrus.Debugf("Event %s not subscribed by any per-device webhook", eventName)
		return nil
	}

	logrus.Infof("Forwarded %s to %d/%d per-device webhook(s)", eventName, successes, eligible)
	if len(failed) > 0 && successes == 0 {
		return fmt.Errorf("all %d per-device webhook(s) failed for %s: %s", eligible, eventName, strings.Join(failed, "; "))
	}
	return nil
}

// webhookEventAllowed reports whether eventName passes a per-webhook event filter.
// An empty filter means "all events". Matching is case-insensitive, mirroring
// isEventWhitelisted.
func webhookEventAllowed(events []string, eventName string) bool {
	if len(events) == 0 {
		return true
	}
	for _, allowed := range events {
		if strings.EqualFold(strings.TrimSpace(allowed), eventName) {
			return true
		}
	}
	return false
}

// chatwootContactInfo holds extracted contact information for Chatwoot sync
type chatwootContactInfo struct {
	Identifier string
	Name       string
	IsGroup    bool
	FromName   string
	IsFromMe   bool
}

// extractChatwootContactInfo extracts contact identifier and name from message payload.
// For groups, uses the group JID as identifier and tries to fetch group name.
// For private chats, uses the sender's phone number.
func extractChatwootContactInfo(ctx context.Context, data map[string]any) (*chatwootContactInfo, error) {
	from, _ := data["from"].(string)
	fromName, _ := data["from_name"].(string)
	chatID, _ := data["chat_id"].(string)
	isFromMe, _ := data["is_from_me"].(bool)

	logrus.Infof("Chatwoot: Processing message from %s (from_name: %s, chat_id: %s, is_from_me: %v)", from, fromName, chatID, isFromMe)

	if from == "" {
		return nil, fmt.Errorf("empty 'from' field")
	}

	isGroup := utils.IsGroupJID(chatID)
	info := &chatwootContactInfo{
		IsGroup:  isGroup,
		FromName: fromName,
	}

	if isGroup {
		info.Identifier = chatID
		info.Name = getGroupName(ctx, chatID)
		if info.Name == "" {
			info.Name = "Group: " + utils.ExtractPhoneFromJID(chatID)
		}
		logrus.Infof("Chatwoot: Detected group message, using group contact: %s", info.Name)
	} else if isFromMe {
		info.Identifier = utils.ExtractPhoneFromJID(chatID)
		info.Name = info.Identifier
	} else {
		info.Identifier = utils.ExtractPhoneFromJID(from)
		info.Name = fromName
		if info.Name == "" {
			info.Name = info.Identifier
		}
	}

	return info, nil
}

// buildChatwootMessageContent extracts message body and attachments from the payload.
// For group messages, prepends the sender name to the content.
func buildChatwootMessageContent(data map[string]any, isGroup bool, fromName string) (content string, attachments []string) {
	if body, ok := data["body"].(string); ok && body != "" {
		content = body
	}

	if content == "" {
		content = extractStructuredMessageContent(data)
	}

	// For group messages, prepend sender name to content
	if isGroup && fromName != "" && content != "" {
		content = fromName + ": " + content
	}

	// Extract media attachments
	mediaFields := []string{"image", "audio", "video", "document", "sticker", "video_note"}
	for _, field := range mediaFields {
		if mediaData, ok := data[field]; ok {
			if path, ok := mediaData.(string); ok && path != "" {
				attachments = append(attachments, path)
				logrus.Infof("Chatwoot: Found %s attachment at %s", field, path)
			}
		}
	}

	// Handle empty content
	if content == "" && len(attachments) == 0 {
		content = "(Unsupported message type)"
		logrus.Info("Chatwoot: Message content is empty/unsupported, using placeholder")
	}

	// For group messages with attachments but no text, still prepend sender name
	if isGroup && fromName != "" && content == "" && len(attachments) > 0 {
		content = fromName + ": (media)"
	}

	return content, attachments
}

func shouldForwardEventToChatwoot(eventName string) bool {
	switch eventName {
	case "message", "message.reaction", "message.ack":
		return true
	default:
		return false
	}
}

func isEventWhitelistedForChatwoot(eventName string) bool {
	if len(config.WhatsappWebhookEvents) == 0 {
		return true
	}
	if isEventWhitelisted(eventName) {
		return true
	}
	// message.reaction and message.ack ride along when "message" is whitelisted,
	// so delivery/read status sync works without an explicit ack subscription.
	if eventName == "message.reaction" || eventName == "message.ack" {
		return isEventWhitelisted("message")
	}
	return false
}

func buildReactionChatwootContent(data map[string]any, isGroup bool, fromName string) string {
	reaction, _ := data["reaction"].(string)
	reactedMessageID, _ := data["reacted_message_id"].(string)

	actor := "Someone"
	if fromName != "" {
		actor = fromName
	} else if from, ok := data["from"].(string); ok && from != "" {
		actor = utils.ExtractPhoneFromJID(from)
	}

	if reactedMessageID != "" {
		if reaction == "" {
			return fmt.Sprintf("%s removed a reaction from message %s", actor, reactedMessageID)
		}
		return fmt.Sprintf("%s reacted %s to message %s", actor, reaction, reactedMessageID)
	}

	if reaction == "" {
		return fmt.Sprintf("%s removed a reaction", actor)
	}
	return fmt.Sprintf("%s reacted %s", actor, reaction)
}

func chatwootMessageTypeFromPayload(data map[string]any) string {
	if isFromMe, ok := data["is_from_me"].(bool); ok && isFromMe {
		return "outgoing"
	}
	return "incoming"
}

func extractStructuredMessageContent(data map[string]any) string {
	if contact, ok := data["contact"]; ok && contact != nil {
		if name, phone, ok := extractContactDetails(contact); ok {
			return utils.FormatContactSummary(name, phone, false)
		}
		return "Contact shared"
	}

	if contactsArray, ok := data["contacts_array"]; ok && contactsArray != nil {
		switch contacts := contactsArray.(type) {
		case []webhookContactPayload:
			return structuredContactsArraySummary(contacts)
		case []*webhookContactPayload:
			normalized := make([]webhookContactPayload, 0, len(contacts))
			for _, contact := range contacts {
				if contact != nil {
					normalized = append(normalized, *contact)
				}
			}
			return structuredContactsArraySummary(normalized)
		case []any:
			if len(contacts) == 0 {
				return "Contacts shared"
			}
			if name, phone, ok := extractContactDetails(contacts[0]); ok {
				return utils.FormatContactSummary(name, phone, true)
			}
			return "Contacts shared"
		default:
			return "Contacts shared"
		}
	}

	if location, ok := data["location"]; ok && location != nil {
		if lm, ok := location.(interface {
			GetDegreesLatitude() float64
			GetDegreesLongitude() float64
			GetName() string
		}); ok {
			name := lm.GetName()
			if name != "" {
				return fmt.Sprintf("Location: %s (%.6f, %.6f)", name, lm.GetDegreesLatitude(), lm.GetDegreesLongitude())
			}
			return fmt.Sprintf("Location: %.6f, %.6f", lm.GetDegreesLatitude(), lm.GetDegreesLongitude())
		}
		return "Location shared"
	}

	if liveLocation, ok := data["live_location"]; ok && liveLocation != nil {
		if lm, ok := liveLocation.(interface {
			GetDegreesLatitude() float64
			GetDegreesLongitude() float64
		}); ok {
			return fmt.Sprintf("Live Location: %.6f, %.6f", lm.GetDegreesLatitude(), lm.GetDegreesLongitude())
		}
		return "Live location shared"
	}

	if list, ok := data["list"]; ok && list != nil {
		if lm, ok := list.(interface{ GetTitle() string }); ok {
			title := lm.GetTitle()
			if title != "" {
				return "List: " + title
			}
		}
		return "List message"
	}

	if order, ok := data["order"]; ok && order != nil {
		if om, ok := order.(interface{ GetOrderTitle() string }); ok {
			title := om.GetOrderTitle()
			if title != "" {
				return "Order: " + title
			}
		}
		return "Order message"
	}

	return ""
}

func extractContactDetails(contact any) (name string, phone string, ok bool) {
	switch c := contact.(type) {
	case webhookContactPayload:
		return c.DisplayName, c.PhoneNumber, true
	case *webhookContactPayload:
		if c == nil {
			return "", "", false
		}
		return c.DisplayName, c.PhoneNumber, true
	case map[string]any:
		if v, ok := c["displayName"].(string); ok {
			name = v
		} else if v, ok := c["display_name"].(string); ok {
			name = v
		}
		if v, ok := c["phone_number"].(string); ok {
			phone = v
		}
		if phone == "" {
			if v, ok := c["vcard"].(string); ok {
				phone = utils.ExtractPhoneFromVCard(v)
			}
		}
		return name, phone, name != "" || phone != ""
	case interface {
		GetDisplayName() string
		GetVcard() string
	}:
		name = c.GetDisplayName()
		phone = utils.ExtractPhoneFromVCard(c.GetVcard())
		return name, phone, true
	default:
		return "", "", false
	}
}

func structuredContactsArraySummary(contacts []webhookContactPayload) string {
	if len(contacts) == 0 {
		return "Contacts shared"
	}
	first := contacts[0]
	return utils.FormatContactSummary(first.DisplayName, first.PhoneNumber, true)
}

// syncMessageToChatwoot creates or finds contact/conversation and sends the message.
func syncMessageToChatwoot(cw *chatwoot.Client, info *chatwootContactInfo, content string, attachments []string) error {
	// Lock per-identifier mutex to prevent duplicate contact/conversation creation
	mu := getContactMutex(info.Identifier)
	mu.Lock()

	contact, err := cw.FindOrCreateContact(info.Name, info.Identifier, info.IsGroup)
	if err != nil {
		mu.Unlock()
		return fmt.Errorf("failed to find/create contact for %s: %w", info.Identifier, err)
	}
	logrus.Infof("Chatwoot: Contact ID: %d", contact.ID)

	conversation, err := cw.FindOrCreateConversation(contact.ID)
	mu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to find/create conversation for contact %d: %w", contact.ID, err)
	}
	logrus.Infof("Chatwoot: Conversation ID: %d", conversation.ID)

	logrus.Infof("Chatwoot: Creating message (Length: %d, Attachments: %d)", len(content), len(attachments))
	messageType := "incoming"
	if info.IsFromMe {
		messageType = "outgoing"
	}
	msgID, err := cw.CreateMessage(conversation.ID, content, messageType, attachments)
	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}
	chatwoot.MarkMessageAsSent(msgID)

	logrus.Infof("Chatwoot: Message synced successfully for %s", info.Identifier)
	return nil
}

func forwardToChatwoot(ctx context.Context, payload map[string]any, eventName string) {
	logrus.Infof("Chatwoot: Attempting to forward %s...", eventName)

	// Resolve the client for the device that received this message. Falls back
	// to the env-var default singleton when no per-device registry/config exists.
	cw := chatwoot.GetDefaultClient()
	if reg := chatwoot.GetGlobalRegistry(); reg != nil {
		if deviceID, _ := payload["device_id"].(string); deviceID != "" {
			client, err := reg.GetClientForDevice(deviceID)
			switch {
			case err != nil:
				logrus.Warnf("Chatwoot: no config for device %s, using default client: %v", deviceID, err)
			case client == nil:
				// Device has an explicit config that is disabled: respect it and
				// skip forwarding rather than falling back to the default client.
				logrus.Infof("Chatwoot: device %s is disabled, skipping forward", deviceID)
				return
			default:
				cw = client
			}
		}
	}

	if !cw.IsConfigured() {
		logrus.Warn("Chatwoot: Client is not configured (check CHATWOOT_* env vars)")
		return
	}

	data, ok := payload["payload"].(map[string]any)
	if !ok {
		logrus.Error("Chatwoot: Invalid payload format (missing 'payload' object)")
		return
	}

	// Delivery/read receipts update the status of a previously synced message
	// rather than creating a new one.
	if eventName == "message.ack" {
		handleChatwootMessageAck(cw, data)
		return
	}

	// Extract contact information
	info, err := extractChatwootContactInfo(ctx, data)
	if err != nil {
		logrus.Warnf("Chatwoot: Skipping message: %v", err)
		return
	}

	// Build message content
	var (
		content     string
		attachments []string
	)
	switch eventName {
	case "message.reaction":
		content = buildReactionChatwootContent(data, info.IsGroup, info.FromName)
	default:
		content, attachments = buildChatwootMessageContent(data, info.IsGroup, info.FromName)
	}
	info.IsFromMe = chatwootMessageTypeFromPayload(data) == "outgoing"

	// Sync to Chatwoot
	if err := syncMessageToChatwoot(cw, info, content, attachments); err != nil {
		logrus.Errorf("Chatwoot: %v", err)
	}
}

// handleChatwootMessageAck maps a WhatsApp delivery/read receipt to the Chatwoot
// message it corresponds to and updates that message's status, so the ✓/✓✓/read
// ticks in Chatwoot reflect real WhatsApp delivery state.
func handleChatwootMessageAck(cw *chatwoot.Client, data map[string]any) {
	receiptType, _ := data["receipt_type"].(string)
	status := chatwootStatusFromReceipt(receiptType)
	if status == "" {
		return // not a delivered/read receipt we care about
	}

	for _, waMsgID := range receiptMessageIDs(data) {
		convID, msgID, ok := chatwoot.ResolveTrackedMessage(waMsgID)
		if !ok {
			continue // not a message we synced/sent
		}
		if err := cw.UpdateMessageStatus(convID, msgID, status); err != nil {
			logrus.Warnf("Chatwoot: failed to update message %d status to %s: %v", msgID, status, err)
			continue
		}
		logrus.Infof("Chatwoot: message %d marked as %s (wa id %s)", msgID, status, waMsgID)
	}
}

// chatwootStatusFromReceipt maps a WhatsApp receipt type to a Chatwoot message
// status, or "" when the receipt should be ignored.
func chatwootStatusFromReceipt(receiptType string) string {
	switch receiptType {
	case "delivered":
		return "delivered"
	case "read", "read-self", "played", "played-self":
		return "read"
	default:
		return ""
	}
}

// receiptMessageIDs extracts the WhatsApp message IDs from a receipt payload,
// tolerating the slice types it may carry through the in-memory payload map.
func receiptMessageIDs(data map[string]any) []string {
	switch ids := data["ids"].(type) {
	case []string: // types.MessageID is an alias of string
		return ids
	case []any:
		out := make([]string, 0, len(ids))
		for _, v := range ids {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// isEventWhitelisted checks if the given event name is in the configured whitelist
func isEventWhitelisted(eventName string) bool {
	for _, allowed := range config.WhatsappWebhookEvents {
		if strings.EqualFold(strings.TrimSpace(allowed), eventName) {
			return true
		}
	}
	return false
}

// getGroupName fetches the group name from WhatsApp using the group JID.
// Uses a TTL cache to avoid repeated API calls for the same group.
func getGroupName(ctx context.Context, groupJID string) string {
	// Check cache first
	if name, ok := getCachedGroupName(groupJID); ok {
		logrus.Debugf("Chatwoot: Using cached group name for %s: %s", groupJID, name)
		return name
	}

	client := ClientFromContext(ctx)
	if client == nil {
		logrus.Debug("Chatwoot: ClientFromContext returned nil, trying GetClient()")
		client = GetClient()
	}
	if client == nil {
		logrus.Warn("Chatwoot: No WhatsApp client available to fetch group name")
		return ""
	}

	jid, err := types.ParseJID(groupJID)
	if err != nil {
		logrus.Warnf("Chatwoot: Failed to parse group JID %s: %v", groupJID, err)
		return ""
	}

	// Use a fresh context with timeout since the original context may be canceled
	// (this function is called from a goroutine)
	freshCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logrus.Debugf("Chatwoot: Fetching group info for %s", groupJID)
	groupInfo, err := client.GetGroupInfo(freshCtx, jid)
	if err != nil {
		logrus.Warnf("Chatwoot: Failed to get group info for %s: %v", groupJID, err)
		return ""
	}

	if groupInfo != nil && groupInfo.Name != "" {
		logrus.Infof("Chatwoot: Got group name: %s", groupInfo.Name)
		// Cache the result
		setCachedGroupName(groupJID, groupInfo.Name)
		return groupInfo.Name
	}

	logrus.Debug("Chatwoot: GroupInfo is nil or Name is empty")
	return ""
}
