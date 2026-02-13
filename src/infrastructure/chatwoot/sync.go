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
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// SyncService handles message history synchronization to Chatwoot
type SyncService struct {
	client          *Client
	chatStorageRepo domainChatStorage.IChatStorageRepository

	// Track sync progress per device
	progressMap map[string]*SyncProgress
	progressMu  sync.RWMutex
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

	logrus.Infof("Chatwoot Sync: Starting history sync for device %s (days: %d, media: %v, groups: %v)",
		deviceID, opts.DaysLimit, opts.IncludeMedia, opts.IncludeGroups)

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

		progress.UpdateChat(chat.JID)

		err := s.syncChat(ctx, deviceID, chat, sinceTime, waClient, opts, progress)
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

	contact, err := s.client.FindOrCreateContact(contactName, chat.JID, isGroup)
	if err != nil {
		return fmt.Errorf("failed to create contact: %w", err)
	}
	logrus.Debugf("Chatwoot Sync: Contact ID: %d", contact.ID)

	// 2. Find or create conversation
	conversation, err := s.client.FindOrCreateConversation(contact.ID)
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

// syncMessage syncs a single message to Chatwoot
func (s *SyncService) syncMessage(
	ctx context.Context,
	conversationID int,
	msg *domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
) error {
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

	// Add timestamp prefix for historical context
	timePrefix := msg.Timestamp.Format("2006-01-02 15:04")

	// For group messages, add sender info
	if isGroup && !msg.IsFromMe && msg.Sender != "" {
		senderName := utils.ExtractPhoneFromJID(msg.Sender)
		content = fmt.Sprintf("[%s] %s: %s", timePrefix, senderName, content)
	} else {
		content = fmt.Sprintf("[%s] %s", timePrefix, content)
	}

	var attachments []string

	// Handle media if enabled and present
	if opts.IncludeMedia && msg.MediaType != "" && msg.URL != "" && len(msg.MediaKey) > 0 {
		filePath, err := s.downloadMedia(ctx, msg, waClient)
		if err != nil {
			logrus.Debugf("Chatwoot Sync: Failed to download media for message %s: %v", msg.ID, err)
			// Continue without media - it might be expired
			content += " [media unavailable]"
		} else if filePath != "" {
			attachments = append(attachments, filePath)
		}
	}

	// Send to Chatwoot and register the returned ID in the dedup cache so the
	// resulting webhook event is recognized as "ours" and not forwarded back to WhatsApp.
	msgID, err := s.client.CreateMessage(conversationID, content, messageType, attachments)

	for _, fp := range attachments {
		if err := os.Remove(fp); err != nil {
			logrus.Debugf("Chatwoot Sync: Failed to remove temp file %s: %v", fp, err)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}

	MarkMessageAsSent(msgID)

	return nil
}

// downloadMedia downloads media for a message and returns the temp file path
func (s *SyncService) downloadMedia(ctx context.Context, msg *domainChatStorage.Message, waClient *whatsmeow.Client) (string, error) {
	if msg.URL == "" || len(msg.MediaKey) == 0 {
		return "", fmt.Errorf("missing media URL or key")
	}

	if waClient == nil {
		return "", fmt.Errorf("WhatsApp client not available")
	}

	// Create downloadable message based on type
	var downloadable whatsmeow.DownloadableMessage

	switch msg.MediaType {
	case "image":
		downloadable = &waE2E.ImageMessage{
			URL:           proto.String(msg.URL),
			MediaKey:      msg.MediaKey,
			FileSHA256:    msg.FileSHA256,
			FileEncSHA256: msg.FileEncSHA256,
			FileLength:    proto.Uint64(msg.FileLength),
		}
	case "video":
		downloadable = &waE2E.VideoMessage{
			URL:           proto.String(msg.URL),
			MediaKey:      msg.MediaKey,
			FileSHA256:    msg.FileSHA256,
			FileEncSHA256: msg.FileEncSHA256,
			FileLength:    proto.Uint64(msg.FileLength),
		}
	case "audio", "ptt":
		downloadable = &waE2E.AudioMessage{
			URL:           proto.String(msg.URL),
			MediaKey:      msg.MediaKey,
			FileSHA256:    msg.FileSHA256,
			FileEncSHA256: msg.FileEncSHA256,
			FileLength:    proto.Uint64(msg.FileLength),
		}
	case "document":
		downloadable = &waE2E.DocumentMessage{
			URL:           proto.String(msg.URL),
			MediaKey:      msg.MediaKey,
			FileSHA256:    msg.FileSHA256,
			FileEncSHA256: msg.FileEncSHA256,
			FileLength:    proto.Uint64(msg.FileLength),
		}
	case "sticker":
		downloadable = &waE2E.StickerMessage{
			URL:           proto.String(msg.URL),
			MediaKey:      msg.MediaKey,
			FileSHA256:    msg.FileSHA256,
			FileEncSHA256: msg.FileEncSHA256,
			FileLength:    proto.Uint64(msg.FileLength),
		}
	default:
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
		return ".ogg"
	case "document":
		return ".bin"
	case "sticker":
		return ".webp"
	default:
		return ""
	}
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

// TriggerAutoSync is called when a device connects to optionally start auto-sync
func TriggerAutoSync(deviceID string, chatStorageRepo domainChatStorage.IChatStorageRepository, waClient *whatsmeow.Client) {
	if !config.ChatwootEnabled || !config.ChatwootImportMessages {
		return
	}

	client := GetDefaultClient()
	if !client.IsConfigured() {
		logrus.Warn("Chatwoot Sync: Auto-sync skipped - Chatwoot not configured")
		return
	}

	// Resolve the storage device ID (JID) from the WhatsApp client,
	// since chats are stored under the full JID, not the user-assigned alias.
	storageDeviceID := deviceID
	if waClient != nil && waClient.Store != nil && waClient.Store.ID != nil {
		if jid := waClient.Store.ID.ToNonAD().String(); jid != "" {
			storageDeviceID = jid
		}
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
