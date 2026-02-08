package whatsapp

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
)

var submitWebhookFn = submitWebhook

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
	// Check if event is whitelisted (if whitelist is configured)
	if len(config.WhatsappWebhookEvents) > 0 {
		if !isEventWhitelisted(eventName) {
			logrus.Debugf("Skipping event %s - not in webhook events whitelist", eventName)
			return nil
		}
	}

	err := forwardToWebhooks(ctx, payload, eventName)

	if eventName == "message" && config.ChatwootEnabled {
		go forwardToChatwoot(ctx, payload)
	}

	return err
}

func forwardToWebhooks(ctx context.Context, payload map[string]any, eventName string) error {
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
func extractChatwootContactInfo(ctx context.Context, data map[string]interface{}) (*chatwootContactInfo, error) {
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
func buildChatwootMessageContent(data map[string]interface{}, isGroup bool, fromName string) (content string, attachments []string) {
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

func chatwootMessageTypeFromPayload(data map[string]interface{}) string {
	if isFromMe, ok := data["is_from_me"].(bool); ok && isFromMe {
		return "outgoing"
	}
	return "incoming"
}

func extractStructuredMessageContent(data map[string]interface{}) string {
	if contact, ok := data["contact"]; ok && contact != nil {
		if cm, ok := contact.(interface {
			GetDisplayName() string
			GetVcard() string
		}); ok {
			name := cm.GetDisplayName()
			phone := extractPhoneFromVCard(cm.GetVcard())
			switch {
			case name != "" && phone != "":
				return fmt.Sprintf("Contact: %s (%s)", name, phone)
			case name != "":
				return "Contact: " + name
			case phone != "":
				return "Contact: " + phone
			}
		}
		return "Contact shared"
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

func extractPhoneFromVCard(vcard string) string {
	for _, line := range strings.Split(vcard, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "TEL") {
			if idx := strings.LastIndex(line, ":"); idx >= 0 {
				return strings.TrimSpace(line[idx+1:])
			}
		}
	}
	return ""
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

func forwardToChatwoot(ctx context.Context, payload map[string]any) {
	logrus.Info("Chatwoot: Attempting to forward message...")
	cw := chatwoot.GetDefaultClient()
	if !cw.IsConfigured() {
		logrus.Warn("Chatwoot: Client is not configured (check CHATWOOT_* env vars)")
		return
	}

	data, ok := payload["payload"].(map[string]interface{})
	if !ok {
		logrus.Error("Chatwoot: Invalid payload format (missing 'payload' object)")
		return
	}

	// Extract contact information
	info, err := extractChatwootContactInfo(ctx, data)
	if err != nil {
		logrus.Warnf("Chatwoot: Skipping message: %v", err)
		return
	}

	// Build message content
	content, attachments := buildChatwootMessageContent(data, info.IsGroup, info.FromName)
	info.IsFromMe = chatwootMessageTypeFromPayload(data) == "outgoing"

	// Sync to Chatwoot
	if err := syncMessageToChatwoot(cw, info, content, attachments); err != nil {
		logrus.Errorf("Chatwoot: %v", err)
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
