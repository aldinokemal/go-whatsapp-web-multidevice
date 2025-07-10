package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// Type definitions
type ExtractedMedia struct {
	MediaPath string `json:"media_path"`
	MimeType  string `json:"mime_type"`
	Caption   string `json:"caption"`
}

// Global variables
var (
	cli           *whatsmeow.Client
	db            *sqlstore.Container // Add global database reference for cleanup
	log           waLog.Logger
	historySyncID int32
	startupTime   = time.Now().Unix()
)

// InitWaDB initializes the WhatsApp database connection
func InitWaDB(ctx context.Context) *sqlstore.Container {
	log = waLog.Stdout("Main", config.WhatsappLogLevel, true)
	dbLog := waLog.Stdout("Database", config.WhatsappLogLevel, true)

	storeContainer, err := initDatabase(ctx, dbLog)
	if err != nil {
		log.Errorf("Database initialization error: %v", err)
		panic(pkgError.InternalServerError(fmt.Sprintf("Database initialization error: %v", err)))
	}

	// Set global database reference for remote logout cleanup
	db = storeContainer

	return storeContainer
}

// initDatabase creates and returns a database store container based on the configured URI
func initDatabase(ctx context.Context, dbLog waLog.Logger) (*sqlstore.Container, error) {
	if strings.HasPrefix(config.DBURI, "file:") {
		return sqlstore.New(ctx, "sqlite3", config.DBURI, dbLog)
	} else if strings.HasPrefix(config.DBURI, "postgres:") {
		return sqlstore.New(ctx, "postgres", config.DBURI, dbLog)
	}

	return nil, fmt.Errorf("unknown database type: %s. Currently only sqlite3(file:) and postgres are supported", config.DBURI)
}

// InitWaCLI initializes the WhatsApp client
func InitWaCLI(ctx context.Context, storeContainer *sqlstore.Container, chatStorageRepo *chatstorage.Storage) *whatsmeow.Client {
	device, err := storeContainer.GetFirstDevice(ctx)
	if err != nil {
		log.Errorf("Failed to get device: %v", err)
		panic(err)
	}

	if device == nil {
		log.Errorf("No device found")
		panic("No device found")
	}

	// Configure device properties
	osName := fmt.Sprintf("%s %s", config.AppOs, config.AppVersion)
	store.DeviceProps.PlatformType = &config.AppPlatform
	store.DeviceProps.Os = &osName

	// Create and configure the client
	cli = whatsmeow.NewClient(device, waLog.Stdout("Client", config.WhatsappLogLevel, true))
	cli.EnableAutoReconnect = true
	cli.AutoTrustIdentity = true
	cli.AddEventHandler(func(rawEvt interface{}) {
		handler(ctx, rawEvt, chatStorageRepo)
	})

	return cli
}

// UpdateGlobalClient updates the global cli variable with a new client instance
// This is needed when reinitializing the client after logout to ensure all
// infrastructure code uses the new client instance
func UpdateGlobalClient(newCli *whatsmeow.Client) {
	cli = newCli
	log.Infof("Global WhatsApp client updated successfully")
}

// GetGlobalClient returns the current global client instance
func GetGlobalClient() *whatsmeow.Client {
	return cli
}

// GetClient returns the current global client instance (alias for GetGlobalClient)
func GetClient() *whatsmeow.Client {
	return cli
}

// GetConnectionStatus returns the current connection status of the global client
func GetConnectionStatus() (isConnected bool, isLoggedIn bool, deviceID string) {
	if cli == nil {
		return false, false, ""
	}

	isConnected = cli.IsConnected()
	isLoggedIn = cli.IsLoggedIn()

	if cli.Store != nil && cli.Store.ID != nil {
		deviceID = cli.Store.ID.String()
	}

	return isConnected, isLoggedIn, deviceID
}

// CleanupDatabase removes the database file to prevent foreign key constraint issues
func CleanupDatabase() error {
	dbPath := strings.TrimPrefix(config.DBURI, "file:")
	if strings.Contains(dbPath, "?") {
		dbPath = strings.Split(dbPath, "?")[0]
	}

	logrus.Infof("[CLEANUP] Removing database file: %s", dbPath)
	if err := os.Remove(dbPath); err != nil {
		if !os.IsNotExist(err) {
			logrus.Errorf("[CLEANUP] Error removing database file: %v", err)
			return err
		} else {
			logrus.Info("[CLEANUP] Database file already removed")
		}
	} else {
		logrus.Info("[CLEANUP] Database file removed successfully")
	}
	return nil
}

// CleanupTemporaryFiles removes history files, QR images, and send items
func CleanupTemporaryFiles() error {
	// Clean up history files
	if files, err := filepath.Glob(fmt.Sprintf("./%s/history-*", config.PathStorages)); err == nil {
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				logrus.Errorf("[CLEANUP] Error removing history file %s: %v", f, err)
				return err
			}
		}
		logrus.Info("[CLEANUP] History files cleaned up")
	}

	// Clean up QR images
	if qrImages, err := filepath.Glob(fmt.Sprintf("./%s/scan-*", config.PathQrCode)); err == nil {
		for _, f := range qrImages {
			if err := os.Remove(f); err != nil {
				logrus.Errorf("[CLEANUP] Error removing QR image %s: %v", f, err)
				return err
			}
		}
		logrus.Info("[CLEANUP] QR images cleaned up")
	}

	// Clean up send items
	if qrItems, err := filepath.Glob(fmt.Sprintf("./%s/*", config.PathSendItems)); err == nil {
		for _, f := range qrItems {
			if !strings.Contains(f, ".gitignore") {
				if err := os.Remove(f); err != nil {
					logrus.Errorf("[CLEANUP] Error removing send item %s: %v", f, err)
					return err
				}
			}
		}
		logrus.Info("[CLEANUP] Send items cleaned up")
	}

	return nil
}

// ReinitializeWhatsAppComponents reinitializes database and client components
func ReinitializeWhatsAppComponents(ctx context.Context, chatStorageRepo *chatstorage.Storage) (*sqlstore.Container, *whatsmeow.Client, error) {
	logrus.Info("[CLEANUP] Reinitializing database and client...")

	newDB := InitWaDB(ctx)
	newCli := InitWaCLI(ctx, newDB, chatStorageRepo)

	// Update global references
	db = newDB
	cli = newCli

	logrus.Info("[CLEANUP] Database and client reinitialized successfully")

	return newDB, newCli, nil
}

// PerformCompleteCleanup performs all cleanup operations in the correct order
func PerformCompleteCleanup(ctx context.Context, logPrefix string, chatStorageRepo *chatstorage.Storage) (*sqlstore.Container, *whatsmeow.Client, error) {
	logrus.Infof("[%s] Starting complete cleanup process...", logPrefix)

	// Disconnect current client if it exists
	if cli != nil {
		cli.Disconnect()
		logrus.Infof("[%s] Client disconnected", logPrefix)
	}

	// Truncate all chatstorage data before other cleanup
	if chatStorageRepo != nil {
		logrus.Infof("[%s] Truncating chatstorage data...", logPrefix)
		if err := chatStorageRepo.TruncateAllDataWithLogging(logPrefix); err != nil {
			logrus.Errorf("[%s] Failed to truncate chatstorage data: %v", logPrefix, err)
			// Continue with cleanup even if chatstorage truncation fails
		}
	}

	// Clean up database
	if err := CleanupDatabase(); err != nil {
		return nil, nil, fmt.Errorf("database cleanup failed: %v", err)
	}

	// Reinitialize components
	newDB, newCli, err := ReinitializeWhatsAppComponents(ctx, chatStorageRepo)
	if err != nil {
		return nil, nil, fmt.Errorf("reinitialization failed: %v", err)
	}

	// Clean up temporary files
	if err := CleanupTemporaryFiles(); err != nil {
		logrus.Errorf("[%s] Temporary file cleanup failed (non-critical): %v", logPrefix, err)
		// Don't return error for file cleanup as it's non-critical
	}

	logrus.Infof("[%s] Complete cleanup process finished successfully", logPrefix)
	logrus.Infof("[%s] Application is ready for next login without restart", logPrefix)

	return newDB, newCli, nil
}

// PerformCleanupAndUpdateGlobals is a convenience function that performs cleanup
// and ensures global client synchronization
func PerformCleanupAndUpdateGlobals(ctx context.Context, logPrefix string, chatStorageRepo *chatstorage.Storage) (*sqlstore.Container, *whatsmeow.Client, error) {
	newDB, newCli, err := PerformCompleteCleanup(ctx, logPrefix, chatStorageRepo)
	if err != nil {
		return nil, nil, err
	}

	// Ensure global client is properly synchronized
	UpdateGlobalClient(newCli)

	return newDB, newCli, nil
}

// handleRemoteLogout performs cleanup when user logs out from their phone
func handleRemoteLogout(ctx context.Context, chatStorageRepo *chatstorage.Storage) {
	logrus.Info("[REMOTE_LOGOUT] User logged out from phone - starting cleanup...")
	logrus.Info("[REMOTE_LOGOUT] This will clear all WhatsApp session data and chat storage")

	// Log database state before cleanup
	if db != nil {
		devices, dbErr := db.GetAllDevices(ctx)
		if dbErr != nil {
			logrus.Errorf("[REMOTE_LOGOUT] Error getting devices before cleanup: %v", dbErr)
		} else {
			logrus.Infof("[REMOTE_LOGOUT] Devices before cleanup: %d found", len(devices))
		}
	}

	// Perform complete cleanup with global client synchronization
	_, _, err := PerformCleanupAndUpdateGlobals(ctx, "REMOTE_LOGOUT", chatStorageRepo)
	if err != nil {
		logrus.Errorf("[REMOTE_LOGOUT] Cleanup failed: %v", err)
		return
	}

	logrus.Info("[REMOTE_LOGOUT] Remote logout cleanup completed successfully")
}

// handler is the main event handler for WhatsApp events
func handler(ctx context.Context, rawEvt interface{}, chatStorageRepo *chatstorage.Storage) {
	switch evt := rawEvt.(type) {
	case *events.DeleteForMe:
		handleDeleteForMe(ctx, evt)
	case *events.AppStateSyncComplete:
		handleAppStateSyncComplete(ctx, evt)
	case *events.PairSuccess:
		handlePairSuccess(ctx, evt)
	case *events.LoggedOut:
		handleLoggedOut(ctx, chatStorageRepo)
	case *events.Connected, *events.PushNameSetting:
		handleConnectionEvents(ctx)
	case *events.StreamReplaced:
		handleStreamReplaced(ctx)
	case *events.Message:
		handleMessage(ctx, evt, chatStorageRepo)
	case *events.Receipt:
		handleReceipt(ctx, evt)
	case *events.Presence:
		handlePresence(ctx, evt)
	case *events.HistorySync:
		handleHistorySync(ctx, evt, chatStorageRepo)
	case *events.AppState:
		handleAppState(ctx, evt)
	}
}

// Event handler functions

func handleDeleteForMe(_ context.Context, evt *events.DeleteForMe) {
	log.Infof("Deleted message %s for %s", evt.MessageID, evt.SenderJID.String())
}

func handleAppStateSyncComplete(_ context.Context, evt *events.AppStateSyncComplete) {
	if len(cli.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
		if err := cli.SendPresence(types.PresenceAvailable); err != nil {
			log.Warnf("Failed to send available presence: %v", err)
		} else {
			log.Infof("Marked self as available")
		}
	}
}

func handlePairSuccess(_ context.Context, evt *events.PairSuccess) {
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "LOGIN_SUCCESS",
		Message: fmt.Sprintf("Successfully pair with %s", evt.ID.String()),
	}
}

func handleLoggedOut(ctx context.Context, chatStorageRepo *chatstorage.Storage) {
	logrus.Warn("[REMOTE_LOGOUT] Received LoggedOut event - user logged out from phone")

	// Broadcast immediate notification about remote logout
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "REMOTE_LOGOUT",
		Message: "User logged out from phone - cleaning up session...",
		Result:  nil,
	}

	// Perform comprehensive cleanup
	handleRemoteLogout(ctx, chatStorageRepo)

	// Broadcast final notification that cleanup is complete and ready for new login
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "LOGOUT_COMPLETE",
		Message: "Remote logout cleanup completed - ready for new login",
		Result:  nil,
	}
}

func handleConnectionEvents(_ context.Context) {
	if len(cli.Store.PushName) == 0 {
		return
	}

	// Send presence available when connecting and when the pushname is changed.
	// This makes sure that outgoing messages always have the right pushname.
	if err := cli.SendPresence(types.PresenceAvailable); err != nil {
		log.Warnf("Failed to send available presence: %v", err)
	} else {
		log.Infof("Marked self as available")
	}
}

func handleStreamReplaced(_ context.Context) {
	os.Exit(0)
}

func handleMessage(ctx context.Context, evt *events.Message, chatStorageRepo *chatstorage.Storage) {
	// Log message metadata
	metaParts := buildMessageMetaParts(evt)
	log.Infof("Received message %s from %s (%s): %+v",
		evt.Info.ID,
		evt.Info.SourceString(),
		strings.Join(metaParts, ", "),
		evt.Message,
	)

	chatStorageRepo.CreateMessage(ctx, evt)

	// Handle image message if present
	handleImageMessage(ctx, evt)

	// Auto-mark message as read if configured
	handleAutoMarkRead(ctx, evt)

	// Handle auto-reply if configured
	handleAutoReply(evt)

	// Forward to webhook if configured
	handleWebhookForward(ctx, evt)
}

func buildMessageMetaParts(evt *events.Message) []string {
	metaParts := []string{
		fmt.Sprintf("pushname: %s", evt.Info.PushName),
		fmt.Sprintf("timestamp: %s", evt.Info.Timestamp),
	}
	if evt.Info.Type != "" {
		metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
	}
	if evt.Info.Category != "" {
		metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
	}
	if evt.IsViewOnce {
		metaParts = append(metaParts, "view once")
	}
	return metaParts
}

func handleImageMessage(ctx context.Context, evt *events.Message) {
	if img := evt.Message.GetImageMessage(); img != nil {
		if path, err := utils.ExtractMedia(ctx, cli, config.PathStorages, img); err != nil {
			log.Errorf("Failed to download image: %v", err)
		} else {
			log.Infof("Image downloaded to %s", path)
		}
	}
}

func handleAutoMarkRead(_ context.Context, evt *events.Message) {
	// Only mark read if auto-mark read is enabled and message is incoming
	if !config.WhatsappAutoMarkRead || evt.Info.IsFromMe {
		return
	}

	// Mark the message as read
	messageIDs := []types.MessageID{evt.Info.ID}
	timestamp := time.Now()
	chat := evt.Info.Chat
	sender := evt.Info.Sender

	if err := cli.MarkRead(messageIDs, timestamp, chat, sender); err != nil {
		log.Warnf("Failed to mark message %s as read: %v", evt.Info.ID, err)
	} else {
		log.Debugf("Marked message %s as read", evt.Info.ID)
	}
}

func handleAutoReply(evt *events.Message) {
	if config.WhatsappAutoReplyMessage != "" &&
		!utils.IsGroupJID(evt.Info.Chat.String()) &&
		!evt.Info.IsIncomingBroadcast() &&
		evt.Message.GetExtendedTextMessage().GetText() != "" {
		_, _ = cli.SendMessage(
			context.Background(),
			utils.FormatJID(evt.Info.Sender.String()),
			&waE2E.Message{Conversation: proto.String(config.WhatsappAutoReplyMessage)},
		)
	}
}

func handleWebhookForward(ctx context.Context, evt *events.Message) {
	// Skip webhook for specific protocol messages that shouldn't trigger webhooks
	if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		protocolType := protocolMessage.GetType().String()
		// Skip EPHEMERAL_SYNC_RESPONSE but allow REVOKE and MESSAGE_EDIT
		if protocolType == "EPHEMERAL_SYNC_RESPONSE" {
			log.Debugf("Skipping webhook for EPHEMERAL_SYNC_RESPONSE message")
			return
		}
	}

	if len(config.WhatsappWebhook) > 0 &&
		!strings.Contains(evt.Info.SourceString(), "broadcast") {
		go func(evt *events.Message) {
			if err := forwardToWebhook(ctx, evt); err != nil {
				logrus.Error("Failed forward to webhook: ", err)
			}
		}(evt)
	}
}

func handleReceipt(_ context.Context, evt *events.Receipt) {
	switch evt.Type {
	case types.ReceiptTypeRead, types.ReceiptTypeReadSelf:
		log.Infof("%v was read by %s at %s", evt.MessageIDs, evt.SourceString(), evt.Timestamp)
	case types.ReceiptTypeDelivered:
		log.Infof("%s was delivered to %s at %s", evt.MessageIDs[0], evt.SourceString(), evt.Timestamp)
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

func handleHistorySync(ctx context.Context, evt *events.HistorySync, chatStorageRepo *chatstorage.Storage) {
	id := atomic.AddInt32(&historySyncID, 1)
	fileName := fmt.Sprintf("%s/history-%d-%s-%d-%s.json",
		config.PathStorages,
		startupTime,
		cli.Store.ID.String(),
		id,
		evt.Data.SyncType.String(),
	)

	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Errorf("Failed to open file to write history sync: %v", err)
		return
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err = enc.Encode(evt.Data); err != nil {
		log.Errorf("Failed to write history sync: %v", err)
		return
	}

	log.Infof("Wrote history sync to %s", fileName)

	// Process history sync data to database
	if chatStorageRepo != nil {
		if err := processHistorySync(ctx, evt.Data, chatStorageRepo); err != nil {
			log.Errorf("Failed to process history sync to database: %v", err)
		}
	}
}

func handleAppState(_ context.Context, evt *events.AppState) {
	log.Debugf("App state event: %+v / %+v", evt.Index, evt.SyncActionValue)
}

// processHistorySync processes history sync data and stores messages in the database
func processHistorySync(ctx context.Context, data *waHistorySync.HistorySync, chatStorageRepo *chatstorage.Storage) error {
	if data == nil {
		return nil
	}

	syncType := data.GetSyncType()
	log.Infof("Processing history sync type: %s", syncType.String())

	switch syncType {
	case waHistorySync.HistorySync_INITIAL_BOOTSTRAP, waHistorySync.HistorySync_RECENT:
		// Process conversation messages
		return processConversationMessages(ctx, data, chatStorageRepo)
	case waHistorySync.HistorySync_PUSH_NAME:
		// Process push names to update chat names
		return processPushNames(ctx, data, chatStorageRepo)
	default:
		// Other sync types are not needed for message storage
		log.Debugf("Skipping history sync type: %s", syncType.String())
		return nil
	}
}

// processConversationMessages processes and stores conversation messages from history sync
func processConversationMessages(_ context.Context, data *waHistorySync.HistorySync, chatStorageRepo *chatstorage.Storage) error {
	conversations := data.GetConversations()
	log.Infof("Processing %d conversations from history sync", len(conversations))

	for _, conv := range conversations {
		chatJID := conv.GetID()
		if chatJID == "" {
			continue
		}

		// Parse JID to get proper format
		jid, err := types.ParseJID(chatJID)
		if err != nil {
			log.Warnf("Failed to parse JID %s: %v", chatJID, err)
			continue
		}

		displayName := conv.GetDisplayName()

		// Get or create chat
		chatName := chatStorageRepo.GetChatNameWithPushName(jid, chatJID, "", displayName)

		// Process messages in the conversation
		messages := conv.GetMessages()
		log.Debugf("Processing %d messages for chat %s", len(messages), chatJID)

		// Collect messages for batch processing
		var messageBatch []*chatstorage.Message
		var latestTimestamp time.Time

		for _, histMsg := range messages {
			if histMsg == nil || histMsg.Message == nil {
				continue
			}

			msg := histMsg.Message
			msgKey := msg.GetKey()
			if msgKey == nil {
				continue
			}

			// Skip messages without ID
			messageID := msgKey.GetID()
			if messageID == "" {
				continue
			}

			// Extract message content and media info
			content := utils.ExtractMessageTextFromProto(msg.GetMessage())
			mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := utils.ExtractMediaInfo(msg.GetMessage())

			// Skip if there's no content and no media
			if content == "" && mediaType == "" {
				continue
			}

			// Determine sender
			sender := ""
			isFromMe := msgKey.GetFromMe()
			if isFromMe {
				if cli.Store.ID != nil {
					sender = cli.Store.ID.User
				}
			} else {
				participant := msgKey.GetParticipant()
				if participant != "" {
					// For group messages, participant contains the actual sender
					if senderJID, err := types.ParseJID(participant); err == nil {
						sender = senderJID.User
					} else {
						sender = participant
					}
				} else {
					// For individual chats, use the chat JID as sender
					sender = jid.User
				}
			}

			// Convert timestamp from Unix seconds to time.Time
			timestamp := time.Unix(int64(msg.GetMessageTimestamp()), 0)

			// Track latest timestamp
			if timestamp.After(latestTimestamp) {
				latestTimestamp = timestamp
			}

			// Create message object and add to batch
			message := &chatstorage.Message{
				ID:            messageID,
				ChatJID:       chatJID,
				Sender:        sender,
				Content:       content,
				Timestamp:     timestamp,
				IsFromMe:      isFromMe,
				MediaType:     mediaType,
				Filename:      filename,
				URL:           url,
				MediaKey:      mediaKey,
				FileSHA256:    fileSHA256,
				FileEncSHA256: fileEncSHA256,
				FileLength:    fileLength,
			}

			messageBatch = append(messageBatch, message)
		}

		// Store or update the chat with latest message time
		if len(messageBatch) > 0 {
			chat := &chatstorage.Chat{
				JID:                 chatJID,
				Name:                chatName,
				LastMessageTime:     latestTimestamp,
				EphemeralExpiration: 0, // Will be updated from individual messages
			}

			// Store or update the chat
			if err := chatStorageRepo.StoreChat(chat); err != nil {
				log.Warnf("Failed to store chat %s: %v", chatJID, err)
				continue
			}

			// Store messages in batch
			if err := chatStorageRepo.StoreMessagesBatch(messageBatch); err != nil {
				log.Warnf("Failed to store messages batch for chat %s: %v", chatJID, err)
			} else {
				log.Debugf("Stored %d messages for chat %s", len(messageBatch), chatJID)
			}
		}
	}

	return nil
}

// processPushNames processes push names from history sync to update chat names
func processPushNames(ctx context.Context, data *waHistorySync.HistorySync, chatStorageRepo *chatstorage.Storage) error {
	pushnames := data.GetPushnames()
	log.Infof("Processing %d push names from history sync", len(pushnames))

	for _, pushname := range pushnames {
		jidStr := pushname.GetId()
		name := pushname.GetPushname()

		if jidStr == "" || name == "" {
			continue
		}

		// Check if chat exists
		existingChat, err := chatStorageRepo.GetChat(jidStr)
		if err != nil || existingChat == nil {
			// Chat doesn't exist yet, skip
			continue
		}

		// Update chat name if it's different
		if existingChat.Name != name {
			existingChat.Name = name
			if err := chatStorageRepo.StoreChat(existingChat); err != nil {
				log.Warnf("Failed to update chat name for %s: %v", jidStr, err)
			} else {
				log.Debugf("Updated chat name for %s to %s", jidStr, name)
			}
		}
	}

	return nil
}
