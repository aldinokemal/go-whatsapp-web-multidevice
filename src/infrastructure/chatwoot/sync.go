package chatwoot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot/pgimport"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// SyncService handles message history synchronization to Chatwoot
type SyncService struct {
	client          *Client
	chatStorageRepo domainChatStorage.IChatStorageRepository

	// Track sync progress per device
	progressMap map[string]*SyncProgress
	progressMu  sync.RWMutex

	// pgImporter is the optional direct-Postgres history importer. When
	// non-nil (i.e. when config.ChatwootImportDBURI is set), SyncHistory
	// writes rows into Chatwoot's schema directly instead of replaying
	// every message through the REST API. Live message forwarding and
	// inbound handling always use the REST client, regardless of this.
	pgImporter *pgimport.Importer
	pgInitMu   sync.Mutex
}

// NewSyncService creates a new sync service instance
func NewSyncService(
	client *Client,
	chatStorageRepo domainChatStorage.IChatStorageRepository,
) *SyncService {
	return &SyncService{
		client:          client,
		chatStorageRepo: chatStorageRepo,
		progressMap:     make(map[string]*SyncProgress),
	}
}

// groupNameResolver memoizes GetGroupInfo lookups for the duration of a
// single sync run. Scoping the cache per-run (rather than per-SyncService)
// avoids cross-device pollution when two devices sync concurrently and may
// see different subjects for the same group.
type groupNameResolver struct {
	mu    sync.Mutex
	cache map[string]string
}

func newGroupNameResolver() *groupNameResolver {
	return &groupNameResolver{cache: make(map[string]string)}
}

// resolve looks up a group's real subject via whatsmeow, caching the
// result. Returns "" on parse/RPC failure so the caller can fall back to
// the stored chat name.
func (g *groupNameResolver) resolve(ctx context.Context, waClient *whatsmeow.Client, chatJID string) string {
	if waClient == nil {
		return ""
	}
	g.mu.Lock()
	if cached, ok := g.cache[chatJID]; ok {
		g.mu.Unlock()
		return cached
	}
	g.mu.Unlock()

	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := waClient.GetGroupInfo(lookupCtx, jid)
	if err != nil || info == nil || info.Name == "" {
		return ""
	}

	g.mu.Lock()
	g.cache[chatJID] = info.Name
	g.mu.Unlock()
	return info.Name
}

// pgImporterForSync returns the shared direct-Postgres importer, opening it
// lazily the first time it's requested. A blank import DB URI means the REST
// path is selected. A configured-but-broken URI is a sync error, not a REST
// fallback, because operators explicitly opted into direct DB import.
func (s *SyncService) pgImporterForSync(ctx context.Context) (*pgimport.Importer, error) {
	if strings.TrimSpace(config.ChatwootImportDBURI) == "" {
		return nil, nil
	}
	s.pgInitMu.Lock()
	defer s.pgInitMu.Unlock()
	if s.pgImporter != nil {
		return s.pgImporter, nil
	}
	imp, err := pgimport.New(ctx, pgimport.Config{
		DSN:       config.ChatwootImportDBURI,
		AccountID: config.ChatwootAccountID,
		InboxID:   config.ChatwootInboxID,
		// APIToken is forwarded so the importer can resolve the Chatwoot
		// agent that owns this token from the access_tokens table.
		// Outgoing imported messages get stamped with that agent so they
		// render with a name/avatar instead of as "Unknown sender".
		APIToken: config.ChatwootAPIToken,
	})
	if err != nil {
		return nil, fmt.Errorf("chatwoot pgimport: %w", err)
	}
	s.pgImporter = imp
	return s.pgImporter, nil
}

// GetProgress returns the current sync progress for a device
func (s *SyncService) GetProgress(deviceID string) *SyncProgress {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	if progress, ok := s.progressMap[deviceID]; ok {
		cloned := progress.Clone()
		return &cloned
	}
	return nil
}

// IsRunning returns true if a sync is currently running for the device
func (s *SyncService) IsRunning(deviceID string) bool {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	if progress, ok := s.progressMap[deviceID]; ok {
		return progress.IsRunning()
	}
	return false
}

// SyncHistory performs the initial message history sync to Chatwoot
func (s *SyncService) SyncHistory(ctx context.Context, deviceID string, waClient *whatsmeow.Client, opts SyncOptions) (*SyncProgress, error) {
	// Atomic check-and-set to prevent race condition
	progress := NewSyncProgress(deviceID)
	s.progressMu.Lock()
	if existing, ok := s.progressMap[deviceID]; ok && existing.IsRunning() {
		s.progressMu.Unlock()
		cloned := existing.Clone()
		return &cloned, fmt.Errorf("sync already in progress for device %s", deviceID)
	}
	s.progressMap[deviceID] = progress
	s.progressMu.Unlock()

	progress.SetRunning()

	// Per-run group-name cache — local so concurrent SyncHistory calls
	// for different devices don't share and invalidate each other's
	// entries.
	groupResolver := newGroupNameResolver()

	// Decide up front whether this run uses the direct-Postgres importer
	// or the REST path. The decision is logged
	// so operators can tell from the logs which path ran.
	importer, err := s.pgImporterForSync(ctx)
	if err != nil {
		progress.SetFailed(err)
		return progress, err
	}
	backend := "REST"
	if importer != nil {
		backend = "pgimport"
	}
	logrus.Infof("Chatwoot Sync: Starting history sync for device %s (backend=%s days=%d media=%v groups=%v)",
		deviceID, backend, opts.DaysLimit, opts.IncludeMedia, opts.IncludeGroups)

	// 1. Get all chats for this device
	chats, err := s.chatStorageRepo.GetChats(&domainChatStorage.ChatFilter{
		DeviceID: deviceID,
	})
	if err != nil {
		progress.SetFailed(err)
		return progress, fmt.Errorf("failed to get chats: %w", err)
	}

	progress.SetTotals(len(chats), 0)
	logrus.Infof("Chatwoot Sync: Found %d chats to sync", len(chats))

	// 2. Calculate time boundary
	sinceTime := time.Now().AddDate(0, 0, -opts.DaysLimit)

	// 3. Process each chat
	for _, chat := range chats {
		if err := ctx.Err(); err != nil {
			progress.SetFailed(err)
			return progress, err // Context cancelled
		}

		// status@broadcast, 0@s.whatsapp.net and @newsletter channels carry no
		// actionable conversation for an agent, and operators can ignore
		// additional JIDs (or whole address spaces) via CHATWOOT_IGNORE_JIDS.
		// Skipping at the chat level (rather than per-message) keeps the
		// totals honest in the progress tracker — the chat is excluded
		// entirely instead of counted as "synced 0".
		if utils.IsSystemBroadcastJID(chat.JID) || utils.IsNewsletterJID(chat.JID) || utils.MatchesIgnoredJID(chat.JID, config.ChatwootIgnoreJids) {
			logrus.Debugf("Chatwoot Sync: Skipping ignored chat %s", chat.JID)
			continue
		}

		progress.UpdateChat(chat.JID)

		// Resolve the real group subject before dispatching so both paths
		// write a meaningful name instead of the "Group 120363…@g.us"
		// fallback from sqlite_repository.go. Individual chats don't need
		// this — their stored name comes from the push-name pipeline.
		if strings.HasSuffix(chat.JID, "@g.us") {
			if resolved := groupResolver.resolve(ctx, waClient, chat.JID); resolved != "" {
				chat.Name = resolved
			}
		}

		var err error
		if importer != nil {
			err = s.syncChatPG(ctx, importer, deviceID, chat, sinceTime, waClient, opts, progress)
		} else {
			err = s.syncChat(ctx, deviceID, chat, sinceTime, waClient, opts, progress)
		}
		if err != nil {
			logrus.Errorf("Chatwoot Sync: Failed to sync chat %s: %v", chat.JID, err)
			progress.IncrementFailedChats()
			// Continue with other chats
		} else {
			progress.IncrementSyncedChats()
		}
	}

	progress.SetCompleted()
	logrus.Infof("Chatwoot Sync: Completed for device %s. Chats: %d (failed: %d), Messages: %d (failed: %d)",
		deviceID, progress.SyncedChats, progress.FailedChats, progress.SyncedMessages, progress.FailedMessages)

	return progress, nil
}

// syncChat syncs a single chat's messages to Chatwoot
func (s *SyncService) syncChat(
	ctx context.Context,
	deviceID string,
	chat *domainChatStorage.Chat,
	sinceTime time.Time,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	progress *SyncProgress,
) error {
	isGroup := strings.HasSuffix(chat.JID, "@g.us")

	// Skip groups if not configured
	if isGroup && !opts.IncludeGroups {
		logrus.Debugf("Chatwoot Sync: Skipping group %s (groups disabled)", chat.JID)
		return nil
	}

	logrus.Infof("Chatwoot Sync: Processing chat %s (%s)", chat.Name, chat.JID)

	// 1. Find or create contact in Chatwoot
	contactName := chat.Name
	if contactName == "" {
		contactName = utils.ExtractPhoneFromJID(chat.JID)
	}

	var contact *Contact
	err := retrySyncOp(ctx, 3, func() error {
		var createErr error
		contact, createErr = s.client.FindOrCreateContact(contactName, chat.JID, isGroup)
		return createErr
	})
	if err != nil {
		return fmt.Errorf("failed to create contact: %w", err)
	}
	logrus.Debugf("Chatwoot Sync: Contact ID: %d", contact.ID)

	// 2. Find or create conversation
	var conversation *Conversation
	err = retrySyncOp(ctx, 3, func() error {
		var createErr error
		conversation, createErr = s.client.FindOrCreateConversation(contact.ID, chat.JID)
		return createErr
	})
	if err != nil {
		return fmt.Errorf("failed to create conversation: %w", err)
	}
	logrus.Debugf("Chatwoot Sync: Conversation ID: %d", conversation.ID)

	// 3. Get messages since time boundary
	messages, err := s.chatStorageRepo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID:  deviceID,
		ChatJID:   chat.JID,
		StartTime: &sinceTime,
		Limit:     opts.MaxMessagesPerChat,
	})
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}

	if len(messages) == 0 {
		logrus.Debugf("Chatwoot Sync: No messages to sync for %s", chat.JID)
		return nil
	}

	progress.AddMessages(len(messages))
	logrus.Infof("Chatwoot Sync: Found %d messages for %s", len(messages), chat.JID)

	// 4. Sort messages by timestamp (oldest first for proper ordering)
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	// 5. Sync each message
	for i, msg := range messages {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := s.syncMessage(ctx, conversation.ID, msg, waClient, opts, isGroup)
		if err != nil {
			logrus.Warnf("Chatwoot Sync: Failed to sync message %s: %v", msg.ID, err)
			progress.IncrementFailedMessages()
			// Continue with other messages
		} else {
			progress.IncrementSyncedMessages()
		}

		// Rate limiting: pause between batches
		if i > 0 && i%opts.BatchSize == 0 {
			time.Sleep(opts.DelayBetweenBatches)
		}
	}

	return nil
}

// syncChatPG syncs a chat's history to Chatwoot via the direct-Postgres
// importer. It mirrors syncChat's preprocessing (filtering groups, time
// window, sort order) but delegates the actual writes to pgimport, which
// uses a single transaction per chat and preserves original timestamps.
//
// Media is normally represented by text placeholders because direct-DB import
// does not touch Chatwoot's ActiveStorage layer. When
// ChatwootImportMediaWithREST is enabled, downloadable media rows are first
// uploaded through Chatwoot REST with the same source_id; the following DB
// import then skips those REST-created rows idempotently.
func (s *SyncService) syncChatPG(
	ctx context.Context,
	importer *pgimport.Importer,
	deviceID string,
	chat *domainChatStorage.Chat,
	sinceTime time.Time,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	progress *SyncProgress,
) error {
	isGroup := strings.HasSuffix(chat.JID, "@g.us")
	if isGroup && !opts.IncludeGroups {
		logrus.Debugf("Chatwoot Sync: Skipping group %s (groups disabled)", chat.JID)
		return nil
	}

	logrus.Infof("Chatwoot pgimport: Processing chat %s (%s)", chat.Name, chat.JID)

	messages, err := s.chatStorageRepo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID:  deviceID,
		ChatJID:   chat.JID,
		StartTime: &sinceTime,
		Limit:     opts.MaxMessagesPerChat,
	})
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		logrus.Debugf("Chatwoot pgimport: No messages to sync for %s", chat.JID)
		return nil
	}

	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	progress.AddMessages(len(messages))
	logrus.Infof("Chatwoot pgimport: Found %d messages for %s", len(messages), chat.JID)

	// Use a display name that is never the "Group <jid>" fallback from
	// sqlite_repository.go. If the stored name still starts with "Group "
	// followed by the JID user portion, treat it as unset and let the
	// importer use the phone number / JID as last resort.
	chatName := chat.Name
	if isGroup && strings.HasPrefix(chatName, "Group ") {
		chatName = ""
	}

	if mediaMessages := chatwootRESTMediaCandidates(messages, opts); len(mediaMessages) > 0 {
		conversation, err := s.findOrCreateHistoryConversation(ctx, chat, isGroup)
		if err != nil {
			logrus.Warnf("Chatwoot pgimport: REST media pre-pass skipped for %s: %v", chat.JID, err)
		} else {
			s.syncHybridMediaMessagesREST(ctx, conversation.ID, mediaMessages, waClient, opts, isGroup)
		}
	}

	result, err := importer.ImportChat(ctx, pgimport.ImportChatRequest{
		ChatJID:  chat.JID,
		ChatName: chatName,
		Messages: messages,
	})
	if err != nil {
		// On a fatal tx-level error, count every message as failed so the
		// progress totals stay honest.
		progress.AddFailedMessages(len(messages))
		return err
	}
	if err := s.storeChatwootImportLinks(result); err != nil {
		return err
	}

	// Idempotent skips are recorded as "synced" for UI purposes — the row
	// is present in Chatwoot, which is what the operator cares about.
	progress.AddSyncedMessages(result.MessagesWrote + result.MessagesSkipped)
	progress.AddFailedMessages(result.MessagesFailed)

	logrus.Infof("Chatwoot pgimport: %s wrote=%d skipped=%d failed=%d",
		chat.JID, result.MessagesWrote, result.MessagesSkipped, result.MessagesFailed)
	return nil
}

func hasDownloadableChatwootMedia(msg *domainChatStorage.Message) bool {
	return msg != nil && msg.MediaType != "" && utils.ResolveMediaDirectPath(msg.DirectPath, msg.URL) != "" && len(msg.MediaKey) > 0
}

func chatwootRESTMediaCandidates(messages []*domainChatStorage.Message, opts SyncOptions) []*domainChatStorage.Message {
	if !config.ChatwootImportMediaWithREST || !opts.IncludeMedia {
		return nil
	}
	candidates := make([]*domainChatStorage.Message, 0)
	for _, msg := range messages {
		if hasDownloadableChatwootMedia(msg) {
			candidates = append(candidates, msg)
		}
	}
	return candidates
}

func (s *SyncService) findOrCreateHistoryConversation(ctx context.Context, chat *domainChatStorage.Chat, isGroup bool) (*Conversation, error) {
	contactName := chat.Name
	if contactName == "" {
		contactName = utils.ExtractPhoneFromJID(chat.JID)
	}

	var contact *Contact
	err := retrySyncOp(ctx, 3, func() error {
		var createErr error
		contact, createErr = s.client.FindOrCreateContact(contactName, chat.JID, isGroup)
		return createErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create contact for REST media import: %w", err)
	}

	var conversation *Conversation
	err = retrySyncOp(ctx, 3, func() error {
		var createErr error
		conversation, createErr = s.client.FindOrCreateConversation(contact.ID, chat.JID)
		return createErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation for REST media import: %w", err)
	}
	return conversation, nil
}

func (s *SyncService) syncHybridMediaMessagesREST(
	ctx context.Context,
	conversationID int,
	messages []*domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
) {
	for _, msg := range messages {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := s.syncMessageWithOptions(ctx, conversationID, msg, waClient, opts, isGroup, true); err != nil {
			logrus.Warnf("Chatwoot pgimport: REST media pre-pass failed for message %s: %v", msg.ID, err)
		}
	}
}

func (s *SyncService) storeChatwootImportLinks(result *pgimport.ImportResult) error {
	if s.chatStorageRepo == nil || result == nil {
		return nil
	}
	for i := range result.Links {
		link := result.Links[i]
		if err := s.chatStorageRepo.UpsertChatwootMessageLink(&link); err != nil {
			return fmt.Errorf("failed to store chatwoot import link for %s: %w", link.WhatsAppMessageID, err)
		}
	}
	return nil
}

// syncMessage syncs a single message to Chatwoot
func (s *SyncService) syncMessage(
	ctx context.Context,
	conversationID int,
	msg *domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
) error {
	return s.syncMessageWithOptions(ctx, conversationID, msg, waClient, opts, isGroup, false)
}

func (s *SyncService) syncMessageWithOptions(
	ctx context.Context,
	conversationID int,
	msg *domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
	requireMediaAttachment bool,
) error {
	if msg.ID != "" && msg.DeviceID != "" && s.chatStorageRepo != nil {
		existing, err := s.chatStorageRepo.GetChatwootMessageLinkByWhatsAppID(msg.DeviceID, msg.ID)
		if err != nil {
			return fmt.Errorf("failed to lookup chatwoot message link: %w", err)
		}
		if existing != nil && existing.ChatwootMessageID != 0 {
			logrus.Debugf("Chatwoot Sync: Skipping already-linked message %s -> %d", msg.ID, existing.ChatwootMessageID)
			return nil
		}
	}

	// Determine message type: "incoming" or "outgoing"
	messageType := "incoming"
	if msg.IsFromMe {
		messageType = "outgoing"
	}

	// Build content
	content := msg.Content
	if content == "" && msg.MediaType != "" {
		content = fmt.Sprintf("[%s]", msg.MediaType) // Placeholder for media-only
	}

	// For group messages, add sender info so agents can tell participants apart
	if isGroup && !msg.IsFromMe && msg.Sender != "" {
		senderName := utils.ExtractPhoneFromJID(msg.Sender)
		content = fmt.Sprintf("%s: %s", senderName, content)
	}

	var attachments []string

	// Handle media if enabled and present
	if opts.IncludeMedia && msg.MediaType != "" && msg.URL != "" && len(msg.MediaKey) > 0 {
		filePath, err := s.downloadMedia(ctx, msg, waClient)
		if err != nil {
			if requireMediaAttachment {
				return fmt.Errorf("failed to download required media: %w", err)
			}
			logrus.Debugf("Chatwoot Sync: Failed to download media for message %s: %v", msg.ID, err)
			// Continue without media - it might be expired
			content += " [media unavailable]"
		} else if filePath != "" {
			attachments = append(attachments, filePath)
		}
	}
	if requireMediaAttachment && len(attachments) == 0 {
		return fmt.Errorf("required media attachment is unavailable")
	}

	// Send to Chatwoot with retry on transient errors (429, 5xx). Register
	// the returned ID in the dedup cache so the resulting webhook event is
	// recognized as "ours" and not forwarded back to WhatsApp.
	// Stamp source_id = WAID:<id> so the REST-imported row shares the importer's
	// dedup key and gives later replies a stable thread anchor.
	var msgOpts MessageOptions
	if msg.ID != "" {
		msgOpts.SourceID = "WAID:" + msg.ID
	}

	var msgID int
	err := retrySyncOp(ctx, 3, func() error {
		var createErr error
		msgID, createErr = s.client.CreateMessage(conversationID, content, messageType, attachments, msgOpts)
		return createErr
	})

	for _, fp := range attachments {
		if err := os.Remove(fp); err != nil {
			logrus.Debugf("Chatwoot Sync: Failed to remove temp file %s: %v", fp, err)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}

	MarkMessageAsSent(msgID)
	if msgID != 0 && msg.ID != "" && msg.DeviceID != "" && s.chatStorageRepo != nil {
		if err := s.chatStorageRepo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
			DeviceID:                     msg.DeviceID,
			WhatsAppMessageID:            msg.ID,
			WhatsAppChatJID:              msg.ChatJID,
			ChatwootMessageID:            msgID,
			ChatwootConversationID:       conversationID,
			ChatwootInboxID:              s.client.InboxID,
			ChatwootContactInboxSourceID: msg.ChatJID,
			SourceID:                     msgOpts.SourceID,
			Direction:                    messageType,
			IsRead:                       false,
		}); err != nil {
			return fmt.Errorf("failed to store chatwoot message link: %w", err)
		}
	}

	return nil
}

// downloadMedia downloads media for a message and returns the temp file path
func (s *SyncService) downloadMedia(ctx context.Context, msg *domainChatStorage.Message, waClient *whatsmeow.Client) (string, error) {
	directPath := utils.ResolveMediaDirectPath(msg.DirectPath, msg.URL)
	if directPath == "" || len(msg.MediaKey) == 0 {
		return "", fmt.Errorf("missing media direct path or key")
	}

	if waClient == nil {
		return "", fmt.Errorf("WhatsApp client not available")
	}

	downloadable, err := utils.BuildDownloadableMessage(
		msg.MediaType,
		msg.URL,
		directPath,
		msg.Filename,
		msg.MediaKey,
		msg.FileSHA256,
		msg.FileEncSHA256,
		msg.FileLength,
	)
	if err != nil {
		return "", fmt.Errorf("unsupported media type: %s", msg.MediaType)
	}

	// Download
	data, err := waClient.Download(ctx, downloadable)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Write to temp file
	ext := getExtensionForMediaType(msg.MediaType, msg.Filename)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("chatwoot-sync-*%s", ext))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write media: %w", err)
	}

	return tmpFile.Name(), nil
}

// getExtensionForMediaType returns the file extension for a media type
func getExtensionForMediaType(mediaType, filename string) string {
	if filename != "" {
		if ext := filepath.Ext(filename); ext != "" {
			return ext
		}
	}
	switch mediaType {
	case "image":
		return ".jpg"
	case "video":
		return ".mp4"
	case "audio", "ptt":
		return ".oga"
	case "document":
		return ".bin"
	case "sticker":
		return ".webp"
	default:
		return ""
	}
}

// retrySyncOp retries fn up to maxAttempts times with exponential backoff
// (1s, 2s, 4s). Retries only transient errors — Retryable() returns true
// for network/IO failures and for HTTP 429 / 5xx responses, and false for
// 4xx validation errors so we don't hammer Chatwoot on misconfiguration.
func retrySyncOp(ctx context.Context, maxAttempts int, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !Retryable(lastErr) {
			return lastErr
		}
		if attempt < maxAttempts-1 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			logrus.Debugf("Chatwoot Sync: retry attempt %d/%d after %v: %v", attempt+1, maxAttempts, backoff, lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return lastErr
}

// Global sync service instance for REST endpoints
var (
	globalSyncService     *SyncService
	globalSyncServiceOnce sync.Once
)

// GetSyncService returns a shared sync service instance
func GetSyncService(
	client *Client,
	chatStorageRepo domainChatStorage.IChatStorageRepository,
) *SyncService {
	globalSyncServiceOnce.Do(func() {
		globalSyncService = NewSyncService(client, chatStorageRepo)
	})
	return globalSyncService
}

// GetDefaultSyncService returns the global sync service if initialized
func GetDefaultSyncService() *SyncService {
	return globalSyncService
}

// Close releases resources held by the sync service, including the optional
// direct-Postgres importer pool. Safe to call even when pgImporter is nil.
func (s *SyncService) Close() error {
	if s.pgImporter != nil {
		return s.pgImporter.Close()
	}
	return nil
}

// autoSyncTriggered latches per storage-JID so history auto-sync runs at most
// once per device per process. events.Connected fires on every reconnect, and
// SyncHistory's in-flight guard only blocks *concurrent* runs — without this
// latch a reconnect after a completed sync would re-import history (harmless for
// the idempotent pgimport path, but the REST path would duplicate messages).
var autoSyncTriggered sync.Map

// TriggerAutoSync starts a one-time history sync for a freshly connected device
// when CHATWOOT_IMPORT_MESSAGES is enabled. It is safe to call on every connect
// event: it self-guards on configuration, requires a logged-in client (so the
// storage JID is available), and latches per device so it runs only once per
// process. The sync runs in the background and never blocks the caller.
func TriggerAutoSync(chatStorageRepo domainChatStorage.IChatStorageRepository, waClient *whatsmeow.Client) {
	if !config.ChatwootEnabled || !config.ChatwootImportMessages {
		return
	}

	// Chats are stored under the full WhatsApp JID, so a logged-in client is
	// required to resolve the storage device ID. Before login there is nothing
	// to sync; a later connect (post-login) retries.
	if waClient == nil || waClient.Store == nil || waClient.Store.ID == nil {
		return
	}
	storageDeviceID := waClient.Store.ID.ToNonAD().String()
	if storageDeviceID == "" {
		return
	}

	client := GetDefaultClient()
	if !client.IsConfigured() {
		// Provisioning may not have resolved the inbox yet; don't consume the
		// latch so a later connect can retry once configuration completes.
		logrus.Warn("Chatwoot Sync: Auto-sync skipped - Chatwoot not configured")
		return
	}

	if _, loaded := autoSyncTriggered.LoadOrStore(storageDeviceID, struct{}{}); loaded {
		return // already triggered this process for this device
	}

	syncService := GetSyncService(client, chatStorageRepo)

	go func() {
		opts := DefaultSyncOptions()
		opts.DaysLimit = config.ChatwootDaysLimitImportMessages

		logrus.Infof("Chatwoot Sync: Auto-sync triggered for device %s", storageDeviceID)

		_, err := syncService.SyncHistory(context.Background(), storageDeviceID, waClient, opts)
		if err != nil {
			logrus.Errorf("Chatwoot Sync: Auto-sync failed for device %s: %v", storageDeviceID, err)
		}
	}()
}
