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

	"go.mau.fi/whatsmeow/proto/waHistorySync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
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
	keysDB        *sqlstore.Container
	log           waLog.Logger
	historySyncID int32
	startupTime   = time.Now().Unix()
)

// InitWaDB initializes the WhatsApp database connection
func InitWaDB(ctx context.Context, DBURI string) *sqlstore.Container {
	log = waLog.Stdout("Main", config.WhatsappLogLevel, true)
	dbLog := waLog.Stdout("Database", config.WhatsappLogLevel, true)

	storeContainer, err := initDatabase(ctx, dbLog, DBURI)
	if err != nil {
		log.Errorf("Database initialization error: %v", err)
		panic(pkgError.InternalServerError(fmt.Sprintf("Database initialization error: %v", err)))
	}

	return storeContainer
}

// initDatabase creates and returns a database store container based on the configured URI
func initDatabase(ctx context.Context, dbLog waLog.Logger, DBURI string) (*sqlstore.Container, error) {
	if strings.HasPrefix(DBURI, "file:") {
		return sqlstore.New(ctx, "sqlite3", DBURI, dbLog)
	} else if strings.HasPrefix(DBURI, "postgres:") {
		return sqlstore.New(ctx, "postgres", DBURI, dbLog)
	}

	return nil, fmt.Errorf("unknown database type: %s. Currently only sqlite3(file:) and postgres are supported", DBURI)
}

func syncKeysDevice(ctx context.Context, db, keysDB *sqlstore.Container) {
	if keysDB == nil {
		return
	}

	dev, err := db.GetFirstDevice(ctx)
	if err != nil {
		log.Errorf("Failed to get all devices: %v", err)
	} else {
		found := false
		if devs, err := keysDB.GetAllDevices(ctx); err != nil {
			log.Errorf("Failed to get all devices: %v", err)
		} else {
			for _, d := range devs {
				if d.ID == dev.ID {
					found = true
					break
				} else {
					keysDB.DeleteDevice(ctx, d)
				}
			}

			if !found {
				keysDB.PutDevice(ctx, dev)
			}
		}
	}
}

// InitWaCLI initializes the WhatsApp client
func InitWaCLI(ctx context.Context, storeContainer, keysStoreContainer *sqlstore.Container, chatStorageRepo domainChatStorage.IChatStorageRepository) *whatsmeow.Client {
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

	// Set global database reference for remote logout cleanup
	db = storeContainer
	keysDB = keysStoreContainer

	// Configure a separated database for accelerating encryption caching
	if keysDB != nil && device.ID != nil {
		innerStore := sqlstore.NewSQLStore(keysStoreContainer, *device.ID)

		syncKeysDevice(ctx, db, keysDB)
		device.Identities = innerStore
		device.Sessions = innerStore
		device.PreKeys = innerStore
		device.SenderKeys = innerStore
		device.MsgSecrets = innerStore
		device.PrivacyTokens = innerStore
	}

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
func UpdateGlobalClient(newCli *whatsmeow.Client, newDB *sqlstore.Container) {
	cli = newCli
	db = newDB
	log.Infof("Global WhatsApp client updated successfully")
}

// GetClient returns the current global client instance (alias for GetGlobalClient)
func GetClient() *whatsmeow.Client {
	return cli
}

// Get DB instance
func GetDB() *sqlstore.Container {
	return db
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

// CleanupDatabase removes the database file (SQLite) or deletes all devices (PostgreSQL) to prevent foreign key constraint issues
func CleanupDatabase() error {
	// Check if using PostgreSQL
	if strings.HasPrefix(config.DBURI, "postgres:") {
		logrus.Info("[CLEANUP] PostgreSQL detected - deleting all devices from database")

		// Check if database is initialized
		if db == nil {
			logrus.Warn("[CLEANUP] Database is nil, skipping device deletion")
			return nil
		}

		ctx := context.Background()

		// Get all devices
		devices, err := db.GetAllDevices(ctx)
		if err != nil {
			logrus.Errorf("[CLEANUP] Error getting devices: %v", err)
			return fmt.Errorf("failed to get devices: %v", err)
		}

		logrus.Infof("[CLEANUP] Found %d devices to delete", len(devices))

		// Delete each device (this will cascade delete related records like identity keys, sessions, etc.)
		for _, device := range devices {
			logrus.Infof("[CLEANUP] Deleting device: %s", device.ID)
			if err := db.DeleteDevice(ctx, device); err != nil {
				logrus.Errorf("[CLEANUP] Error deleting device %s: %v", device.ID, err)
				return fmt.Errorf("failed to delete device %s: %v", device.ID, err)
			}
		}

		// Also clean up keysDB if it exists and is separate
		if keysDB != nil && keysDB != db {
			keysDevices, err := keysDB.GetAllDevices(ctx)
			if err != nil {
				logrus.Errorf("[CLEANUP] Error getting devices from keysDB: %v", err)
				return fmt.Errorf("failed to get devices from keysDB: %v", err)
			}

			logrus.Infof("[CLEANUP] Found %d devices in keysDB to delete", len(keysDevices))

			for _, device := range keysDevices {
				logrus.Infof("[CLEANUP] Deleting device from keysDB: %s", device.ID)
				if err := keysDB.DeleteDevice(ctx, device); err != nil {
					logrus.Errorf("[CLEANUP] Error deleting device %s from keysDB: %v", device.ID, err)
					return fmt.Errorf("failed to delete device %s from keysDB: %v", device.ID, err)
				}
			}
		}

		logrus.Info("[CLEANUP] All devices deleted successfully from PostgreSQL")
		return nil
	}

	// SQLite: Close database connections before removing the file
	logrus.Info("[CLEANUP] SQLite detected - closing database connections before file removal")

	// Close the main database connection
	if db != nil {
		logrus.Info("[CLEANUP] Closing main database connection")
		if err := db.Close(); err != nil {
			logrus.Errorf("[CLEANUP] Error closing main database: %v", err)
			return fmt.Errorf("failed to close main database: %v", err)
		}
		logrus.Info("[CLEANUP] Main database connection closed successfully")
	}

	// Close keysDB if it exists and is separate from main db
	if keysDB != nil && keysDB != db {
		logrus.Info("[CLEANUP] Closing keysDB database connection")
		if err := keysDB.Close(); err != nil {
			logrus.Errorf("[CLEANUP] Error closing keysDB: %v", err)
			return fmt.Errorf("failed to close keysDB: %v", err)
		}
		logrus.Info("[CLEANUP] KeysDB connection closed successfully")

		// Remove keysDB file if it's also SQLite
		if config.DBKeysURI != "" && strings.HasPrefix(config.DBKeysURI, "file:") {
			keysDBPath := strings.TrimPrefix(config.DBKeysURI, "file:")
			if strings.Contains(keysDBPath, "?") {
				keysDBPath = strings.Split(keysDBPath, "?")[0]
			}

			logrus.Infof("[CLEANUP] Removing keysDB file: %s", keysDBPath)
			if err := os.Remove(keysDBPath); err != nil {
				if !os.IsNotExist(err) {
					logrus.Errorf("[CLEANUP] Error removing keysDB file: %v", err)
					return fmt.Errorf("failed to remove keysDB file: %v", err)
				} else {
					logrus.Info("[CLEANUP] KeysDB file already removed")
				}
			} else {
				logrus.Info("[CLEANUP] KeysDB file removed successfully")
			}
		}
	}

	// Now remove the main database file
	dbPath := strings.TrimPrefix(config.DBURI, "file:")
	if strings.Contains(dbPath, "?") {
		dbPath = strings.Split(dbPath, "?")[0]
	}

	logrus.Infof("[CLEANUP] Removing main database file: %s", dbPath)
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
func ReinitializeWhatsAppComponents(ctx context.Context, chatStorageRepo domainChatStorage.IChatStorageRepository) (*sqlstore.Container, *whatsmeow.Client, error) {
	logrus.Info("[CLEANUP] Reinitializing database and client...")

	newDB := InitWaDB(ctx, config.DBURI)
	if config.DBKeysURI != "" {
		keysDB = InitWaDB(ctx, config.DBKeysURI)
	}
	newCli := InitWaCLI(ctx, newDB, keysDB, chatStorageRepo)

	// Update global references
	db = newDB
	cli = newCli

	logrus.Info("[CLEANUP] Database and client reinitialized successfully")

	return newDB, newCli, nil
}

// PerformCompleteCleanup performs all cleanup operations in the correct order
func PerformCompleteCleanup(ctx context.Context, logPrefix string, chatStorageRepo domainChatStorage.IChatStorageRepository) (*sqlstore.Container, *whatsmeow.Client, error) {
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
func PerformCleanupAndUpdateGlobals(ctx context.Context, logPrefix string, chatStorageRepo domainChatStorage.IChatStorageRepository) (*sqlstore.Container, *whatsmeow.Client, error) {
	newDB, newCli, err := PerformCompleteCleanup(ctx, logPrefix, chatStorageRepo)
	if err != nil {
		return nil, nil, err
	}

	// Ensure global client is properly synchronized
	UpdateGlobalClient(newCli, newDB)

	return newDB, newCli, nil
}

// handleRemoteLogout performs cleanup when user logs out from their phone
func handleRemoteLogout(ctx context.Context, chatStorageRepo domainChatStorage.IChatStorageRepository) {
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
func handler(ctx context.Context, rawEvt any, chatStorageRepo domainChatStorage.IChatStorageRepository) {
	switch evt := rawEvt.(type) {
	case *events.DeleteForMe:
		handleDeleteForMe(ctx, evt, chatStorageRepo)
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
	case *events.GroupInfo:
		handleGroupInfo(ctx, evt)
	}
}

// Event handler functions

func handleDeleteForMe(ctx context.Context, evt *events.DeleteForMe, chatStorageRepo domainChatStorage.IChatStorageRepository) {
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
	if len(config.WhatsappWebhook) > 0 {
		go func() {
			if err := forwardDeleteToWebhook(ctx, evt, message); err != nil {
				log.Errorf("Failed to forward delete event to webhook: %v", err)
			}
		}()
	}
}

func handleAppStateSyncComplete(_ context.Context, evt *events.AppStateSyncComplete) {
	if len(cli.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
		if err := cli.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
			log.Warnf("Failed to send available presence: %v", err)
		} else {
			log.Infof("Marked self as available")
		}
	}
}

func handlePairSuccess(ctx context.Context, evt *events.PairSuccess) {
	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    "LOGIN_SUCCESS",
		Message: fmt.Sprintf("Successfully pair with %s", evt.ID.String()),
	}
	syncKeysDevice(ctx, db, keysDB)
}

func handleLoggedOut(ctx context.Context, chatStorageRepo domainChatStorage.IChatStorageRepository) {
	logrus.Warn("[REMOTE_LOGOUT] Received LoggedOut event - user logged out from phone")

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
	if err := cli.SendPresence(context.Background(), types.PresenceAvailable); err != nil {
		log.Warnf("Failed to send available presence: %v", err)
	} else {
		log.Infof("Marked self as available")
	}
}

func handleStreamReplaced(_ context.Context) {
	os.Exit(0)
}

func handleMessage(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository) {
	// Log message metadata
	metaParts := buildMessageMetaParts(evt)
	log.Infof("Received message %s from %s (%s): %+v",
		evt.Info.ID,
		evt.Info.SourceString(),
		strings.Join(metaParts, ", "),
		evt.Message,
	)

	if err := chatStorageRepo.CreateMessage(ctx, evt); err != nil {
		// Log storage errors to avoid silent failures that could lead to data loss
		log.Errorf("Failed to store incoming message %s: %v", evt.Info.ID, err)
	}

	// Handle image message if present
	handleImageMessage(ctx, evt)

	// Auto-mark message as read if configured
	handleAutoMarkRead(ctx, evt)

	// Handle auto-reply if configured
	handleAutoReply(ctx, evt, chatStorageRepo)

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
	if !config.WhatsappAutoDownloadMedia {
		return
	}
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

	if err := cli.MarkRead(context.Background(), messageIDs, timestamp, chat, sender); err != nil {
		log.Warnf("Failed to mark message %s as read: %v", evt.Info.ID, err)
	} else {
		log.Debugf("Marked message %s as read", evt.Info.ID)
	}
}

func handleAutoReply(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository) {
	if config.WhatsappAutoReplyMessage == "" {
		return
	}

	// Skip groups, broadcasts, and self messages
	if utils.IsGroupJID(evt.Info.Chat.String()) || evt.Info.IsIncomingBroadcast() || evt.Info.IsFromMe {
		return
	}

	// Only reply to direct 1:1 chats (e.g., *@s.whatsapp.net)
	if evt.Info.Chat.Server != types.DefaultUserServer {
		return
	}

	// Extra safety: skip any broadcast/status contexts
	source := evt.Info.SourceString()
	if strings.Contains(source, "broadcast") ||
		strings.HasSuffix(evt.Info.Chat.String(), "@broadcast") ||
		strings.HasPrefix(evt.Info.Chat.String(), "status@") {
		return
	}

	// Require actual typed text (not captions or synthetic labels)
	hasText := false

	// Unwrap FutureProof wrappers to access the inner message content first
	innerMsg := evt.Message
	for i := 0; i < 3; i++ { // safeguard against excessively nested wrappers
		if vm := innerMsg.GetViewOnceMessage(); vm != nil && vm.GetMessage() != nil {
			innerMsg = vm.GetMessage()
			continue
		}
		if em := innerMsg.GetEphemeralMessage(); em != nil && em.GetMessage() != nil {
			innerMsg = em.GetMessage()
			continue
		}
		if vm2 := innerMsg.GetViewOnceMessageV2(); vm2 != nil && vm2.GetMessage() != nil {
			innerMsg = vm2.GetMessage()
			continue
		}
		if vm2e := innerMsg.GetViewOnceMessageV2Extension(); vm2e != nil && vm2e.GetMessage() != nil {
			innerMsg = vm2e.GetMessage()
			continue
		}
		break
	}

	// Check for genuine typed text on the unwrapped content
	if conv := innerMsg.GetConversation(); conv != "" {
		hasText = true
	} else if ext := innerMsg.GetExtendedTextMessage(); ext != nil && ext.GetText() != "" {
		hasText = true
	} else if protoMsg := innerMsg.GetProtocolMessage(); protoMsg != nil {
		if edited := protoMsg.GetEditedMessage(); edited != nil {
			if ext := edited.GetExtendedTextMessage(); ext != nil && ext.GetText() != "" {
				hasText = true
			} else if conv := edited.GetConversation(); conv != "" {
				hasText = true
			}
		}
	}
	if !hasText {
		return
	}

	// Format recipient JID
	recipientJID := utils.FormatJID(evt.Info.Sender.String())

	// Send the auto-reply message
	response, err := cli.SendMessage(
		ctx,
		recipientJID,
		&waE2E.Message{Conversation: proto.String(config.WhatsappAutoReplyMessage)},
	)

	if err != nil {
		log.Errorf("Failed to send auto-reply message: %v", err)
		return
	}

	// Store the auto-reply message in chat storage if send was successful
	if chatStorageRepo != nil {
		// Get our own JID as sender
		senderJID := ""
		if cli.Store.ID != nil {
			senderJID = cli.Store.ID.String()
		}

		// Store the sent auto-reply message
		if err := chatStorageRepo.StoreSentMessageWithContext(
			ctx,
			response.ID,                     // Message ID from WhatsApp response
			senderJID,                       // Our JID as sender
			recipientJID.String(),           // Recipient JID
			config.WhatsappAutoReplyMessage, // Auto-reply content
			response.Timestamp,              // Timestamp from response
		); err != nil {
			// Log storage error but don't fail the auto-reply
			log.Errorf("Failed to store auto-reply message in chat storage: %v", err)
		} else {
			log.Debugf("Auto-reply message %s stored successfully in chat storage", response.ID)
		}
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
			if err := forwardMessageToWebhook(ctx, evt); err != nil {
				logrus.Error("Failed forward to webhook: ", err)
			}
		}(evt)
	}
}

func handleReceipt(ctx context.Context, evt *events.Receipt) {
	sendReceipt := false
	switch evt.Type {
	case types.ReceiptTypeRead, types.ReceiptTypeReadSelf:
		sendReceipt = true
		log.Infof("%v was read by %s at %s: %+v", evt.MessageIDs, evt.SourceString(), evt.Timestamp, evt)
	case types.ReceiptTypeDelivered:
		sendReceipt = true
		log.Infof("%s was delivered to %s at %s: %+v", evt.MessageIDs[0], evt.SourceString(), evt.Timestamp, evt)
	}

	// Forward receipt (ack) event to webhook if configured
	// Note: Receipt events are not rate limited as they are critical for message delivery status
	if len(config.WhatsappWebhook) > 0 && sendReceipt {
		go func(e *events.Receipt) {
			if err := forwardReceiptToWebhook(ctx, e); err != nil {
				logrus.Errorf("Failed to forward ack event to webhook: %v", err)
			}
		}(evt)
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

func handleHistorySync(ctx context.Context, evt *events.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository) {
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
func processHistorySync(ctx context.Context, data *waHistorySync.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository) error {
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
func processConversationMessages(_ context.Context, data *waHistorySync.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository) error {
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

		// Extract ephemeral expiration from conversation
		ephemeralExpiration := conv.GetEphemeralExpiration()

		// Process messages in the conversation
		messages := conv.GetMessages()
		log.Debugf("Processing %d messages for chat %s", len(messages), chatJID)

		// Collect messages for batch processing
		var messageBatch []*domainChatStorage.Message
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
				// For self-messages, use the full JID format to match regular message processing
				if cli.Store.ID != nil {
					sender = cli.Store.ID.String() // Use full JID instead of just User part
				} else {
					// Skip messages where we can't determine the sender to avoid NOT NULL violations
					log.Warnf("Skipping self-message %s: client ID unavailable", messageID)
					continue
				}
			} else {
				participant := msgKey.GetParticipant()
				if participant != "" {
					// For group messages, participant contains the actual sender
					if senderJID, err := types.ParseJID(participant); err == nil {
						sender = senderJID.String() // Use full JID format for consistency
					} else {
						// Fallback to participant string, but ensure it's not empty
						if participant != "" {
							sender = participant
						} else {
							log.Warnf("Skipping message %s: empty participant", messageID)
							continue
						}
					}
				} else {
					// For individual chats, use the chat JID as sender with full format
					sender = jid.String() // Use full JID format for consistency
				}
			}

			// Convert timestamp from Unix seconds to time.Time
			// WhatsApp history sync timestamps are in seconds, not milliseconds
			timestamp := time.Unix(int64(msg.GetMessageTimestamp()), 0)

			// Track latest timestamp
			if timestamp.After(latestTimestamp) {
				latestTimestamp = timestamp
			}

			// Create message object and add to batch
			message := &domainChatStorage.Message{
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
			chat := &domainChatStorage.Chat{
				JID:                 chatJID,
				Name:                chatName,
				LastMessageTime:     latestTimestamp,
				EphemeralExpiration: ephemeralExpiration,
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
func processPushNames(_ context.Context, data *waHistorySync.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository) error {
	pushnames := data.GetPushnames()
	log.Infof("Processing %d push names from history sync", len(pushnames))

	for _, pushname := range pushnames {
		jidStr := pushname.GetID()
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

func handleGroupInfo(ctx context.Context, evt *events.GroupInfo) {
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

	// Forward group info event to webhook if configured
	if len(config.WhatsappWebhook) > 0 {
		go func(e *events.GroupInfo) {
			if err := forwardGroupInfoToWebhook(ctx, e); err != nil {
				logrus.Errorf("Failed to forward group info event to webhook: %v", err)
			}
		}(evt)
	}
}
