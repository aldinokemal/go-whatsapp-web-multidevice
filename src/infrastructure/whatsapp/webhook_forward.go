package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
)

var (
	submitWebhookFn     = submitWebhook
	getChatwootClientFn = chatwoot.GetDefaultClient
	// contactDisplayNameFn resolves the operator-saved address-book name for a
	// 1:1 JID from the WhatsApp contact store. It is a seam so tests can stub
	// the lookup without a real client/store.
	contactDisplayNameFn = lookupContactDisplayName
	// sessionIDForJIDFn resolves the operator-facing session id (the device_id
	// registered via POST /devices, e.g. "org_2") for a connected WhatsApp JID.
	// It is a seam so tests can stub the device-manager lookup.
	sessionIDForJIDFn = sessionIDForJID
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
	deviceJID, _ := payload["device_id"].(string)
	webhookConfig, err := getWebhookConfigForDevice(deviceJID)
	if err != nil {
		// A config lookup failure is not a delivery failure: fall back to the global
		// webhook config so the event still reaches the global targets and Chatwoot.
		logrus.Warnf("Failed to get webhook config for device %s, falling back to global config: %v", deviceJID, err)
		webhookConfig = nil
	}

	webhookAllowed := isEventWhitelistedForDevice(eventName, webhookConfig)
	chatwootAllowed := config.ChatwootEnabled && shouldForwardEventToChatwoot(eventName) && isEventWhitelistedForChatwoot(eventName)

	if !webhookAllowed && !chatwootAllowed {
		logrus.Debugf("Skipping event %s - not allowed for webhooks or Chatwoot", eventName)
		return nil
	}

	webhookURLs := getWebhookURLsFromConfig(webhookConfig)
	if len(webhookURLs) == 0 {
		webhookURLs = config.WhatsappWebhook
	}

	// Enrich the payload with the operator-facing session id so multi-tenant
	// consumers can correlate a webhook (whose device_id is the WhatsApp JID)
	// back to the session id they registered via POST /devices. Done here,
	// synchronously, before the Chatwoot goroutine is spawned below, so the
	// shared payload map is never mutated concurrently.
	if webhookAllowed {
		addWebhookSessionID(payload)
	}

	var webhookErr error
	if webhookAllowed {
		webhookErr = forwardToWebhooks(ctx, payload, eventName, webhookURLs, webhookConfig)
	} else {
		logrus.Debugf("Skipping event %s for configured webhooks, but allowing Chatwoot", eventName)
	}

	if chatwootAllowed {
		go forwardToChatwoot(ctx, payload, eventName)
	}

	return webhookErr
}

// webhookStorageForTest is injectable for unit testing without a real DeviceManager.
var webhookStorageForTest func(deviceJID string) (*domainChatStorage.DeviceRecord, error)

// getDeviceRecordForTest resolves the device record, using test override if set.
func getDeviceRecordForTest(deviceJID string) (*domainChatStorage.DeviceRecord, error) {
	if webhookStorageForTest != nil {
		return webhookStorageForTest(deviceJID)
	}
	dm := GetDeviceManager()
	if dm != nil && dm.storage != nil {
		return dm.storage.GetDeviceRecordByJID(deviceJID)
	}
	return nil, nil
}

// getWebhookConfigForDevice returns the webhook configuration to use for a given device.
// If the device has a custom webhook config, it returns that config.
// Otherwise, it returns nil (caller should use global config).
func getWebhookConfigForDevice(deviceJID string) (*domainChatStorage.DeviceWebhookConfig, error) {
	if deviceJID == "" {
		return nil, nil
	}

	record, err := getDeviceRecordForTest(deviceJID)
	if err != nil {
		return nil, fmt.Errorf("failed to get device record: %w", err)
	}
	if record != nil && record.WebhookURL != nil && *record.WebhookURL != "" {
		logrus.Debugf("Using device-specific webhook config for %s", deviceJID)
		return &domainChatStorage.DeviceWebhookConfig{
			WebhookURL:                record.WebhookURL,
			WebhookSecret:             record.WebhookSecret,
			WebhookEvents:             record.WebhookEvents,
			WebhookInsecureSkipVerify: record.WebhookInsecureSkipVerify,
		}, nil
	}

	return nil, nil
}

// getWebhookURLsFromConfig extracts webhook URLs from the config.
func getWebhookURLsFromConfig(config *domainChatStorage.DeviceWebhookConfig) []string {
	if config == nil || config.WebhookURL == nil || *config.WebhookURL == "" {
		return nil
	}
	return []string{*config.WebhookURL}
}

// isEventWhitelistedForDevice checks if an event is whitelisted for a specific device.
// Uses device-specific events if set, otherwise falls back to global config.
func isEventWhitelistedForDevice(eventName string, deviceConfig *domainChatStorage.DeviceWebhookConfig) bool {
	if deviceConfig != nil && deviceConfig.WebhookEvents != "" {
		for _, allowed := range strings.Split(deviceConfig.WebhookEvents, ",") {
			if strings.EqualFold(strings.TrimSpace(allowed), eventName) {
				return true
			}
		}
		return false
	}
	return len(config.WhatsappWebhookEvents) == 0 || isEventWhitelisted(eventName)
}

// addWebhookSessionID injects the operator-facing session id into a webhook
// payload, derived from its device_id (the WhatsApp JID). It is a no-op when the
// JID can't be mapped to a session (single-session deployments before login, or
// JIDs not tracked by the device manager) or when session_id is already present,
// keeping the change backward-compatible: device_id stays the JID.
func addWebhookSessionID(payload map[string]any) {
	if payload == nil {
		return
	}
	if _, exists := payload["session_id"]; exists {
		return
	}
	jid, _ := payload["device_id"].(string)
	if sessionID := sessionIDForJIDFn(jid); sessionID != "" {
		payload["session_id"] = sessionID
	}
}

// sessionIDForJID resolves the session id registered via POST /devices for a
// connected WhatsApp JID, using the global device manager. Returns "" when no
// manager or matching instance is available.
func sessionIDForJID(jid string) string {
	if jid == "" {
		return ""
	}
	dm := GetDeviceManager()
	if dm == nil {
		return ""
	}
	if inst, ok := dm.getDeviceByJID(jid); ok && inst != nil {
		return inst.ID()
	}
	return ""
}

// forwardToWebhooks delivers the payload to each URL in the webhookURLs slice.
// It logs successes and failures, returning an error only if all deliveries fail.
// Partial failures (some succeed, some fail) are logged but do not cause a return error.
func forwardToWebhooks(ctx context.Context, payload map[string]any, eventName string, webhookURLs []string, webhookConfig *domainChatStorage.DeviceWebhookConfig) error {
	total := len(webhookURLs)
	logrus.Infof("Forwarding %s to %d configured webhook(s)", eventName, total)

	if total == 0 {
		return nil
	}

	var (
		failed    []string
		successes int
	)
	for _, url := range webhookURLs {
		if err := submitWebhookFn(ctx, payload, url, webhookConfig); err != nil {
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

// chatwootContactInfo holds extracted contact information for Chatwoot sync
type chatwootContactInfo struct {
	Identifier string
	Name       string
	IsGroup    bool
	FromName   string
	IsFromMe   bool
	// ChatJID is the full WhatsApp chat JID. It is used as the conversation's
	// contact_inbox source_id so Chatwoot's contact endpoints (read receipts)
	// can resolve the conversation, matching the value stored on message links.
	ChatJID string
}

type chatwootSyncResult struct {
	MessageID      int
	ConversationID int
	InboxID        int
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

	// System JIDs (status broadcasts and the WhatsApp service account) carry
	// no useful conversation context for an agent inbox — relaying them would
	// create a "Status" contact and a flood of status-update messages in
	// Chatwoot for every contact who posts a status. We filter these out
	// here for both incoming and outgoing flows.
	if utils.IsSystemBroadcastJID(chatID) || utils.IsSystemBroadcastJID(from) {
		return nil, fmt.Errorf("skipping system/broadcast JID chat=%s from=%s", chatID, from)
	}

	// Channel (newsletter) feeds are broadcast-only: no conversation for an
	// agent, and the channel id is not a phone number — relaying one would
	// fail Chatwoot contact creation with a 422 e164 error.
	if utils.IsNewsletterJID(chatID) || utils.IsNewsletterJID(from) {
		return nil, fmt.Errorf("skipping newsletter JID chat=%s from=%s", chatID, from)
	}

	// Operator-configured ignore list (CHATWOOT_IGNORE_JIDS) on top of the
	// always-ignored system JIDs — supports exact JIDs and the "@g.us" /
	// "@s.whatsapp.net" / "@lid" address-space wildcards.
	if utils.MatchesIgnoredJID(chatID, config.ChatwootIgnoreJids) || utils.MatchesIgnoredJID(from, config.ChatwootIgnoreJids) {
		return nil, fmt.Errorf("skipping ignored JID chat=%s from=%s", chatID, from)
	}

	isGroup := utils.IsGroupJID(chatID)
	info := &chatwootContactInfo{
		IsGroup:  isGroup,
		FromName: fromName,
		ChatJID:  chatID,
	}

	if isGroup {
		info.Identifier = chatID
		info.Name = getGroupName(ctx, chatID)
		if info.Name == "" {
			info.Name = "Group: " + utils.ExtractPhoneFromJID(chatID)
		}
		logrus.Infof("Chatwoot: Detected group message, using group contact: %s", info.Name)
	} else if isFromMe {
		info.Identifier = chatwootIdentifierForJID(chatID)
		// The contact is the recipient (chatID). Prefer the operator's saved
		// address-book name so an operator-initiated chat isn't created with a
		// bare phone number; there is no useful pushname for our own messages.
		info.Name = contactDisplayNameFn(ctx, chatID)
		if info.Name == "" {
			info.Name = info.Identifier
		}
	} else {
		info.Identifier = chatwootIdentifierForJID(from)
		// Prefer the operator's saved address-book name (FullName), then the
		// sender's pushname from the event, then the bare phone/identifier.
		// Using only the pushname meant a number the operator had saved under a
		// real name still surfaced in Chatwoot as its phone number (issue #688).
		info.Name = contactDisplayNameFn(ctx, from)
		if info.Name == "" {
			info.Name = fromName
		}
		if info.Name == "" {
			info.Name = info.Identifier
		}
	}

	return info, nil
}

// chatwootIdentifierForJID returns the value to pass as the Chatwoot contact
// identifier for a 1:1 chat. For @lid (privacy-masked) JIDs the full JID is
// preserved so the Chatwoot client at client.go:FindContactByIdentifier
// takes its identifier-based branch (HasSuffix "@lid"); for ordinary
// @s.whatsapp.net JIDs the suffix is stripped so the client takes its
// phone-normalization branch. Without this distinction, an @lid sender
// whose LID->phone resolution fails earlier in the pipeline would arrive
// here as e.g. "1234abcde@lid", get its suffix stripped to "1234abcde",
// and then be misclassified as a phone number — creating a Chatwoot
// contact with a garbage phone_number that subsequent messages from the
// same @lid sender cannot find.
func chatwootIdentifierForJID(jid string) string {
	if strings.HasSuffix(jid, "@lid") {
		return jid
	}
	return utils.ExtractPhoneFromJID(jid)
}

// lookupContactDisplayName returns the operator-saved address-book name for a
// 1:1 WhatsApp JID from the local contact store, preferring the saved FullName
// over the contact's self-set pushname and business name (the same priority and
// source used by the /user/my/contacts endpoint). It returns "" when no client
// is available or the contact is unknown, letting callers fall back to the live
// event pushname / phone number.
func lookupContactDisplayName(ctx context.Context, jid string) string {
	client := ClientFromContext(ctx)
	if client == nil || client.Store == nil || client.Store.Contacts == nil {
		return ""
	}
	parsed, err := types.ParseJID(jid)
	if err != nil || parsed.IsEmpty() {
		return ""
	}
	// The incoming context may already be canceled (this runs from a goroutine,
	// mirroring getGroupName); use a fresh short deadline for the local read.
	freshCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	contact, err := client.Store.Contacts.GetContact(freshCtx, parsed)
	if err != nil || !contact.Found {
		return ""
	}
	switch {
	case contact.FullName != "":
		return contact.FullName
	case contact.PushName != "":
		return contact.PushName
	case contact.BusinessName != "":
		return contact.BusinessName
	default:
		return ""
	}
}

// extractMediaPath returns the on-disk path for a media field in the
// webhook payload, handling both shapes that buildAutoDownloadPayload can
// emit: a bare string (no caption) or a map with a "path" key (when a
// caption rode along). Returns "" when neither shape applies — including
// when WhatsappAutoDownloadMedia is disabled and the field carries only
// {"url", ...} (Chatwoot can't render a remote URL as an attachment, so
// we skip rather than POST a URL it can't fetch).
func extractMediaPath(mediaData any) string {
	switch v := mediaData.(type) {
	case string:
		return v
	case map[string]any:
		if path, ok := v["path"].(string); ok {
			return path
		}
	}
	return ""
}

// buildChatwootMessageContent extracts message body and attachments from the payload.
// For group messages, prepends the sender name to the content.
func buildChatwootMessageContent(data map[string]any, isGroup bool, fromName string) (content string, attachments []string) {
	// Group sender attribution: label inbound participant messages so agents can
	// tell who spoke. Skip our own outgoing messages (the label would be the
	// operator's own pushname) and fall back to the participant's phone when no
	// pushname is available, matching the reaction path.
	fromMe := chatwootMessageTypeFromPayload(data) == "outgoing"
	senderLabel := fromName
	if senderLabel == "" {
		if from, ok := data["from"].(string); ok {
			senderLabel = utils.ExtractPhoneFromJID(from)
		}
	}
	prefixGroupSender := isGroup && !fromMe && senderLabel != ""

	if body, ok := data["body"].(string); ok && body != "" {
		// Translate WhatsApp formatting (*bold*, _italic_, ~strike~) into the
		// GitHub-flavored markdown Chatwoot renders so emphasis survives the hop.
		content = utils.WhatsAppToChatwootMarkdown(body)
	}

	if content == "" {
		content = extractStructuredMessageContent(data)
	}

	// For group messages, prepend sender name to content
	if prefixGroupSender && content != "" {
		content = senderLabel + ": " + content
	}

	// Extract media attachments. The producer side (buildAutoDownloadPayload
	// in event_message.go) emits a string path when the media has no caption
	// and a {"path", "caption"} map when it does — the latter case applies
	// to image, video, and document fields, where WhatsApp lets the user
	// attach a body alongside the media. Without the map branch, captioned
	// images/videos/documents land in Chatwoot as a text caption with NO
	// attached file, since the string assertion silently fails. audio and
	// sticker are always plain strings, so the original branch covers them.
	mediaFields := []string{"image", "audio", "video", "document", "sticker", "video_note"}
	for _, field := range mediaFields {
		mediaData, ok := data[field]
		if !ok || mediaData == nil {
			continue
		}
		path := extractMediaPath(mediaData)
		if path == "" {
			continue
		}
		attachments = append(attachments, path)
		logrus.Infof("Chatwoot: Found %s attachment at %s", field, path)
	}

	// Handle empty content
	if content == "" && len(attachments) == 0 {
		content = "(Unsupported message type)"
		logrus.Info("Chatwoot: Message content is empty/unsupported, using placeholder")
	}

	// For group messages with attachments but no text, still prepend sender name
	if prefixGroupSender && content == "" && len(attachments) > 0 {
		content = senderLabel + ": (media)"
	}

	return content, attachments
}

func shouldForwardEventToChatwoot(eventName string) bool {
	switch eventName {
	case "message", "message.reaction":
		return true
	case "message.ack":
		return config.ChatwootMessageRead
	case "message.edited":
		return config.ChatwootForwardEdits
	case "message.revoked", "message.deleted":
		// Either feature needs the event: ChatwootForwardDeletes posts a
		// tombstone note, ChatwootMessageDelete hard-deletes the linked Chatwoot
		// message. They are independent and gated separately downstream.
		return config.ChatwootForwardDeletes || config.ChatwootMessageDelete
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
	// Derived message sub-events (reactions, edits, deletes) ride along when the
	// base "message" event is whitelisted, so operators don't have to enumerate
	// every sub-event to get them mirrored to Chatwoot.
	switch eventName {
	case "message.reaction", "message.ack", "message.edited", "message.revoked", "message.deleted":
		return isEventWhitelisted("message")
	}
	return false
}

func buildReactionChatwootContent(data map[string]any, fromName string) string {
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
func syncMessageToChatwoot(cw *chatwoot.Client, info *chatwootContactInfo, content string, attachments []string, opts chatwoot.MessageOptions) (*chatwootSyncResult, error) {
	// Lock per-identifier mutex to prevent duplicate contact/conversation creation
	mu := getContactMutex(info.Identifier)
	mu.Lock()

	contact, err := cw.FindOrCreateContact(info.Name, info.Identifier, info.IsGroup)
	if err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("failed to find/create contact for %s: %w", info.Identifier, err)
	}
	logrus.Infof("Chatwoot: Contact ID: %d", contact.ID)

	conversation, err := cw.FindOrCreateConversation(contact.ID, info.ChatJID)
	mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to find/create conversation for contact %d: %w", contact.ID, err)
	}
	logrus.Infof("Chatwoot: Conversation ID: %d", conversation.ID)

	logrus.Infof("Chatwoot: Creating message (Length: %d, Attachments: %d)", len(content), len(attachments))
	messageType := "incoming"
	if info.IsFromMe {
		messageType = "outgoing"
	}
	msgID, err := cw.CreateMessage(conversation.ID, content, messageType, attachments, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}
	chatwoot.MarkMessageAsSent(msgID)

	logrus.Infof("Chatwoot: Message synced successfully for %s", info.Identifier)
	return &chatwootSyncResult{
		MessageID:      msgID,
		ConversationID: conversation.ID,
		InboxID:        cw.InboxID,
	}, nil
}

func chatwootLinkStorageFromContext(ctx context.Context) (string, domainChatStorage.IChatStorageRepository) {
	instance, ok := DeviceFromContext(ctx)
	if !ok || instance == nil {
		return "", nil
	}

	deviceID := instance.JID()
	if deviceID == "" {
		if client := instance.GetClient(); client != nil && client.Store != nil && client.Store.ID != nil {
			deviceID = client.Store.ID.ToNonAD().String()
		}
	}
	if deviceID == "" {
		deviceID = instance.ID()
	}
	return deviceID, instance.GetChatStorage()
}

func buildChatwootForwardMessageLink(deviceID string, data map[string]any, opts chatwoot.MessageOptions, result *chatwootSyncResult) *domainChatStorage.ChatwootMessageLink {
	if result == nil || deviceID == "" || result.MessageID == 0 {
		return nil
	}
	waMessageID, _ := data["id"].(string)
	if waMessageID == "" {
		return nil
	}
	chatJID, _ := data["chat_id"].(string)

	return &domainChatStorage.ChatwootMessageLink{
		DeviceID:                     deviceID,
		WhatsAppMessageID:            waMessageID,
		WhatsAppChatJID:              chatJID,
		ChatwootMessageID:            result.MessageID,
		ChatwootConversationID:       result.ConversationID,
		ChatwootInboxID:              result.InboxID,
		ChatwootContactInboxSourceID: chatJID,
		SourceID:                     opts.SourceID,
		Direction:                    chatwootMessageTypeFromPayload(data),
		IsRead:                       false,
	}
}

func extractReceiptMessageIDs(data map[string]any) []string {
	rawIDs, ok := data["ids"]
	if !ok {
		return nil
	}

	switch ids := rawIDs.(type) {
	case []string:
		return ids
	case []any:
		result := make([]string, 0, len(ids))
		for _, raw := range ids {
			if id, ok := raw.(string); ok && id != "" {
				result = append(result, id)
			}
		}
		return result
	case string:
		if ids == "" {
			return nil
		}
		return []string{ids}
	default:
		return nil
	}
}

func deleteTargetMessageID(eventName string, data map[string]any) string {
	switch eventName {
	case "message.revoked":
		if id, _ := data["revoked_message_id"].(string); id != "" {
			return id
		}
	case "message.deleted":
		if id, _ := data["deleted_message_id"].(string); id != "" {
			return id
		}
		if id, _ := data["id"].(string); id != "" {
			return id
		}
	}
	return ""
}

func syncReadReceiptsToChatwoot(cw *chatwoot.Client, deviceID string, linkRepo domainChatStorage.IChatStorageRepository, data map[string]any) {
	receiptType, _ := data["receipt_type"].(string)
	if receiptType != string(types.ReceiptTypeRead) && receiptType != string(types.ReceiptTypeReadSelf) {
		return
	}
	if deviceID == "" || linkRepo == nil {
		logrus.Warn("Chatwoot: Cannot sync read receipt without message-link storage")
		return
	}

	for _, messageID := range extractReceiptMessageIDs(data) {
		link, err := linkRepo.GetChatwootMessageLinkByWhatsAppID(deviceID, messageID)
		if err != nil {
			logrus.Errorf("Chatwoot: Failed to lookup read receipt link for %s: %v", messageID, err)
			continue
		}
		if link == nil || link.ChatwootConversationID == 0 {
			continue
		}

		sourceID := link.ChatwootContactInboxSourceID
		if sourceID == "" {
			sourceID = link.WhatsAppChatJID
		}
		if sourceID == "" {
			logrus.Debugf("Chatwoot: Skipping read receipt %s without contact inbox source", messageID)
			continue
		}
		if err := cw.UpdateLastSeen(link.ChatwootConversationID, sourceID); err != nil {
			logrus.Errorf("Chatwoot: Failed to update last seen for message %s: %v", messageID, err)
			continue
		}
		link.IsRead = true
		if err := linkRepo.UpsertChatwootMessageLink(link); err != nil {
			logrus.Errorf("Chatwoot: Failed to mark link read for %s: %v", messageID, err)
		}
	}
}

func deleteLinkedChatwootMessage(cw *chatwoot.Client, deviceID string, linkRepo domainChatStorage.IChatStorageRepository, eventName string, data map[string]any) bool {
	if !config.ChatwootMessageDelete || deviceID == "" || linkRepo == nil {
		return false
	}

	targetID := deleteTargetMessageID(eventName, data)
	if targetID == "" {
		return false
	}

	link, err := linkRepo.GetChatwootMessageLinkByWhatsAppID(deviceID, targetID)
	if err != nil {
		logrus.Errorf("Chatwoot: Failed to lookup delete link for %s: %v", targetID, err)
		return false
	}
	if link == nil || link.ChatwootConversationID == 0 || link.ChatwootMessageID == 0 {
		return false
	}

	if err := cw.DeleteMessage(link.ChatwootConversationID, link.ChatwootMessageID); err != nil {
		logrus.Errorf("Chatwoot: Failed to delete Chatwoot message %d for WhatsApp %s: %v", link.ChatwootMessageID, targetID, err)
		return false
	}
	return true
}

func chatwootForwardMessageID(payload map[string]any) string {
	data, ok := payload["payload"].(map[string]any)
	if !ok {
		return ""
	}
	messageID, _ := data["id"].(string)
	return strings.TrimSpace(messageID)
}

func chatwootForwardRetryDelay(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	delay := time.Minute
	for i := 0; i < attempts; i++ {
		delay *= 2
		if delay >= time.Hour {
			return time.Hour
		}
	}
	return delay
}

func truncateChatwootForwardError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 2000 {
		return msg[:2000]
	}
	return msg
}

// isRetryableChatwootForwardEvent reports whether a forward event is durable
// enough to enqueue for retry on a transient failure. Base messages and their
// edit/delete/reaction sub-events all carry a unique WhatsApp message id, so the
// retry queue can replay them and dedup correctly. Read receipts (message.ack)
// are intentionally excluded — they are best-effort and self-heal on the next
// receipt.
func isRetryableChatwootForwardEvent(eventName string) bool {
	switch eventName {
	case "message", "message.edited", "message.revoked", "message.deleted", "message.reaction":
		return true
	default:
		return false
	}
}

func enqueueChatwootForwardRetry(linkRepo domainChatStorage.IChatStorageRepository, deviceID, eventName string, payload map[string]any, syncErr error) bool {
	if !isRetryableChatwootForwardEvent(eventName) || linkRepo == nil || strings.TrimSpace(deviceID) == "" || !chatwoot.Retryable(syncErr) {
		return false
	}

	messageID := chatwootForwardMessageID(payload)
	if messageID == "" {
		return false
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		logrus.Errorf("Chatwoot: Failed to serialize retry payload for %s: %v", messageID, err)
		return false
	}

	event := &domainChatStorage.ChatwootForwardEvent{
		DeviceID:          deviceID,
		EventName:         eventName,
		WhatsAppMessageID: messageID,
		PayloadJSON:       string(payloadJSON),
		LastError:         truncateChatwootForwardError(syncErr),
		NextAttemptAt:     time.Now().Add(chatwootForwardRetryDelay(0)),
	}
	if err := linkRepo.EnqueueChatwootForwardEvent(event); err != nil {
		logrus.Errorf("Chatwoot: Failed to enqueue retry for %s: %v", messageID, err)
		return false
	}
	logrus.Warnf("Chatwoot: Queued retry for WhatsApp message %s after transient failure", messageID)
	return true
}

func syncPayloadToChatwoot(ctx context.Context, payload map[string]any, eventName, deviceID string, linkRepo domainChatStorage.IChatStorageRepository) error {
	cw := getChatwootClientFn()
	if cw == nil {
		logrus.Warn("Chatwoot: Client is not initialized")
		return nil
	}
	if !cw.IsConfigured() {
		logrus.Warn("Chatwoot: Client is not configured (check CHATWOOT_* env vars)")
		return nil
	}

	data, ok := payload["payload"].(map[string]any)
	if !ok {
		logrus.Error("Chatwoot: Invalid payload format (missing 'payload' object)")
		return nil
	}

	switch eventName {
	case "message.ack":
		syncReadReceiptsToChatwoot(cw, deviceID, linkRepo, data)
		return nil
	case "message.revoked", "message.deleted":
		if deleteLinkedChatwootMessage(cw, deviceID, linkRepo, eventName, data) {
			return nil
		}
		// The hard delete was attempted (or there was no link to delete). The
		// tombstone note that follows is a separate feature; only fall through to
		// it when note forwarding is enabled, so ChatwootMessageDelete alone never
		// posts a note the operator disabled.
		if !config.ChatwootForwardDeletes {
			return nil
		}
	}

	// Extract contact information
	info, err := extractChatwootContactInfo(ctx, data)
	if err != nil {
		logrus.Warnf("Chatwoot: Skipping message: %v", err)
		return nil
	}

	// Build message content. msgOpts threads WhatsApp identifiers into Chatwoot:
	// SourceID stamps the live message with WAID:<id> (unifying dedup with the
	// importer and anchoring future replies); ContentAttributes.in_reply_to_external_id
	// threads reactions/edits/deletes/replies onto the message they reference.
	var (
		content     string
		attachments []string
		msgOpts     chatwoot.MessageOptions
	)
	switch eventName {
	case "message.reaction":
		content = buildReactionChatwootContent(data, info.FromName)
		if rid, _ := data["reacted_message_id"].(string); rid != "" {
			msgOpts.ContentAttributes = map[string]any{"in_reply_to_external_id": "WAID:" + rid}
		}
	case "message.edited", "message.revoked", "message.deleted":
		var threadID string
		content, threadID = buildEditDeleteChatwootContent(eventName, data, info.IsGroup, info.FromName)
		if content == "" {
			logrus.Debugf("Chatwoot: Skipping %s with no renderable content", eventName)
			return nil
		}
		if threadID != "" {
			msgOpts.ContentAttributes = map[string]any{"in_reply_to_external_id": "WAID:" + threadID}
		}
	default:
		content, attachments = buildChatwootMessageContent(data, info.IsGroup, info.FromName)
		if id, _ := data["id"].(string); id != "" {
			msgOpts.SourceID = "WAID:" + id
		}
		if rid, _ := data["replied_to_id"].(string); rid != "" {
			msgOpts.ContentAttributes = map[string]any{"in_reply_to_external_id": "WAID:" + rid}
		}
	}
	info.IsFromMe = chatwootMessageTypeFromPayload(data) == "outgoing"

	// Sync to Chatwoot
	if eventName == "message" && deviceID != "" && linkRepo != nil {
		if waMessageID, _ := data["id"].(string); waMessageID != "" {
			existing, err := linkRepo.GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID)
			if err != nil {
				logrus.Errorf("Chatwoot: Failed to lookup message link for %s: %v", waMessageID, err)
				return err
			}
			if existing != nil && existing.ChatwootMessageID != 0 {
				logrus.Debugf("Chatwoot: Skipping already-linked message %s -> %d", waMessageID, existing.ChatwootMessageID)
				return nil
			}
		}
	}

	result, err := syncMessageToChatwoot(cw, info, content, attachments, msgOpts)
	if err != nil {
		return err
	}
	if eventName == "message" && linkRepo != nil {
		if link := buildChatwootForwardMessageLink(deviceID, data, msgOpts, result); link != nil {
			if err := linkRepo.UpsertChatwootMessageLink(link); err != nil {
				logrus.Errorf("Chatwoot: Failed to store message link for %s: %v", link.WhatsAppMessageID, err)
			}
		}
	}
	return nil
}

func forwardToChatwoot(ctx context.Context, payload map[string]any, eventName string) {
	logrus.Infof("Chatwoot: Attempting to forward %s...", eventName)
	deviceID, linkRepo := chatwootLinkStorageFromContext(ctx)
	if err := syncPayloadToChatwoot(ctx, payload, eventName, deviceID, linkRepo); err != nil {
		logrus.Errorf("Chatwoot: %v", err)
		enqueueChatwootForwardRetry(linkRepo, deviceID, eventName, payload, err)
	}
}

func processChatwootForwardRetryEvent(repo domainChatStorage.IChatStorageRepository, event *domainChatStorage.ChatwootForwardEvent) error {
	if event == nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode retry payload %d: %w", event.ID, err)
	}
	return syncPayloadToChatwoot(context.Background(), payload, event.EventName, event.DeviceID, repo)
}

func processDueChatwootForwardRetries(repo domainChatStorage.IChatStorageRepository) {
	if repo == nil {
		return
	}
	events, err := repo.ListDueChatwootForwardEvents(time.Now(), 20)
	if err != nil {
		logrus.Errorf("Chatwoot: Failed to list retry queue: %v", err)
		return
	}
	for _, event := range events {
		if err := processChatwootForwardRetryEvent(repo, event); err != nil {
			nextAttempt := time.Now().Add(chatwootForwardRetryDelay(event.Attempts + 1))
			if markErr := repo.MarkChatwootForwardEventFailed(event.ID, truncateChatwootForwardError(err), nextAttempt); markErr != nil {
				logrus.Errorf("Chatwoot: Failed to reschedule retry job %d: %v", event.ID, markErr)
			}
			logrus.Warnf("Chatwoot: Retry job %d failed, next attempt at %s: %v", event.ID, nextAttempt.Format(time.RFC3339), err)
			continue
		}
		if err := repo.MarkChatwootForwardEventDone(event.ID); err != nil {
			logrus.Errorf("Chatwoot: Failed to delete completed retry job %d: %v", event.ID, err)
		}
	}
}

var chatwootForwardRetryWorkerOnce sync.Once

func StartChatwootForwardRetryWorker(repo domainChatStorage.IChatStorageRepository) {
	if repo == nil || !config.ChatwootEnabled {
		return
	}
	chatwootForwardRetryWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				processDueChatwootForwardRetries(repo)
				<-ticker.C
			}
		}()
	})
}

// buildEditDeleteChatwootContent renders a WhatsApp edit or delete event into a
// Chatwoot note and the WhatsApp id of the message it refers to (for threading).
// Edits carry the new text in "body"; deletes/revokes carry only the original
// message id. Returns ("", "") when there is nothing to render.
func buildEditDeleteChatwootContent(eventName string, data map[string]any, isGroup bool, fromName string) (content, threadID string) {
	switch eventName {
	case "message.edited":
		threadID, _ = data["original_message_id"].(string)
		body, _ := data["body"].(string)
		body = utils.WhatsAppToChatwootMarkdown(strings.TrimSpace(body))
		if isGroup && fromName != "" && body != "" {
			body = fromName + ": " + body
		}
		if body == "" {
			return "✏️ _(message edited)_", threadID
		}
		return "✏️ **Edited:** " + body, threadID
	case "message.revoked":
		threadID, _ = data["revoked_message_id"].(string)
		return "🗑️ _This message was deleted._", threadID
	case "message.deleted":
		threadID, _ = data["deleted_message_id"].(string)
		return "🗑️ _This message was deleted._", threadID
	}
	return "", ""
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
