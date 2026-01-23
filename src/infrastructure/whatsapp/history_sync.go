package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var historySyncID int32

func handleHistorySync(ctx context.Context, evt *events.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) {
	if client == nil || client.Store == nil || client.Store.ID == nil {
		log.Warnf("Skipping history sync handling: WhatsApp client not initialized")
		return
	}
	id := atomic.AddInt32(&historySyncID, 1)
	fileName := fmt.Sprintf("%s/history-%d-%s-%d-%s.json",
		config.PathStorages,
		startupTime,
		client.Store.ID.String(),
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
		if err := processHistorySync(ctx, evt.Data, chatStorageRepo, client); err != nil {
			log.Errorf("Failed to process history sync to database: %v", err)
		}
	}
}

// processHistorySync processes history sync data and stores messages in the database
func processHistorySync(ctx context.Context, data *waHistorySync.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) error {
	if data == nil {
		return nil
	}

	syncType := data.GetSyncType()
	log.Infof("Processing history sync type: %s", syncType.String())

	switch syncType {
	case waHistorySync.HistorySync_INITIAL_BOOTSTRAP, waHistorySync.HistorySync_RECENT:
		// Process conversation messages
		return processConversationMessages(ctx, data, chatStorageRepo, client)
	case waHistorySync.HistorySync_PUSH_NAME:
		// Process push names to update chat names
		return processPushNames(ctx, data, chatStorageRepo, client)
	default:
		// Other sync types are not needed for message storage
		log.Debugf("Skipping history sync type: %s", syncType.String())
		return nil
	}
}

// processConversationMessages processes and stores conversation messages from history sync
func processConversationMessages(ctx context.Context, data *waHistorySync.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) error {
	conversations := data.GetConversations()
	log.Infof("Processing %d conversations from history sync", len(conversations))

	// Prioritize device JID from context (set by event handler with correct device instance)
	// over client.Store.ID which may point to a different device in multi-device scenarios
	deviceID := ""
	if inst, ok := DeviceFromContext(ctx); ok && inst != nil {
		deviceID = inst.JID()
		if deviceID == "" {
			deviceID = inst.ID()
		}
	}
	if deviceID == "" && client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}

	for _, conv := range conversations {
		rawChatJID := conv.GetID()
		if rawChatJID == "" {
			continue
		}

		// Parse JID to get proper format
		jid, err := types.ParseJID(rawChatJID)
		if err != nil {
			log.Warnf("Failed to parse JID %s: %v", rawChatJID, err)
			continue
		}

		// Normalize JID (convert @lid to @s.whatsapp.net if possible)
		jid = NormalizeJIDFromLID(ctx, jid, client)
		chatJID := jid.String()

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
				if client != nil && client.Store.ID != nil {
					sender = client.Store.ID.ToNonAD().String() // Use full JID instead of just User part
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
						// Normalize sender JID (convert @lid to @s.whatsapp.net if possible)
						senderJID = NormalizeJIDFromLID(ctx, senderJID, client)
						sender = senderJID.ToNonAD().String() // Use full JID format for consistency
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
				DeviceID:      deviceID,
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
				DeviceID:            deviceID,
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
func processPushNames(ctx context.Context, data *waHistorySync.HistorySync, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) error {
	pushnames := data.GetPushnames()
	log.Infof("Processing %d push names from history sync", len(pushnames))

	// Extract device ID from context (same pattern as processConversationMessages)
	deviceID := ""
	if inst, ok := DeviceFromContext(ctx); ok && inst != nil {
		deviceID = inst.JID()
		if deviceID == "" {
			deviceID = inst.ID()
		}
	}
	if deviceID == "" && client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}

	for _, pushname := range pushnames {
		rawJIDStr := pushname.GetID()
		name := pushname.GetPushname()

		if rawJIDStr == "" || name == "" {
			continue
		}

		// Parse and normalize JID (convert @lid to @s.whatsapp.net if possible)
		jid, err := types.ParseJID(rawJIDStr)
		if err != nil {
			log.Warnf("Failed to parse JID %s in push names: %v", rawJIDStr, err)
			continue
		}
		jid = NormalizeJIDFromLID(ctx, jid, client)
		jidStr := jid.String()

		// Check if chat exists (device-scoped to avoid cross-device data leak)
		existingChat, err := chatStorageRepo.GetChatByDevice(deviceID, jidStr)
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
