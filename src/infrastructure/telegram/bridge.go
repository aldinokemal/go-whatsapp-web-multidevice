package telegram

import (
	"context"
	"fmt"
	"os"

	"html"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

// BridgeMessageToTelegram forwards incoming WhatsApp messages to Telegram
func BridgeMessageToTelegram(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository) {
	if config.TelegramBotToken == "" || config.TelegramTargetGroupID == 0 {
		return
	}

	client := whatsapp.GetClient()
	if client == nil {
		return
	}

	// Determine sender and chat JID
	chatJID := evt.Info.Chat
	senderJID := evt.Info.Sender

	// Normalize JIDs
	chatJID = whatsapp.NormalizeJIDFromLID(ctx, chatJID, client)
	senderJID = whatsapp.NormalizeJIDFromLID(ctx, senderJID, client)

	// We use the Chat JID as the identifier for the topic
	// For individual chats: ChatJID == SenderJID (usually)
	// For group chats: ChatJID is the group, SenderJID is the participant
	targetJID := chatJID.String()

	// Get or Create Topic
	topicID, err := chatStorageRepo.GetTelegramTopicID(targetJID)
	if err != nil {
		logrus.Errorf("Failed to get topic ID: %v", err)
		return
	}

	bot := Bot
	if bot == nil {
		logrus.Warn("Telegram bot not initialized")
		return
	}

	// Create topic if not exists
	if topicID == 0 {
		// Determine topic name
		topicName := evt.Info.PushName
		if topicName == "" {
			topicName = targetJID
		}
		// If group, use group name (would need to fetch group info, simplified for now)
		if evt.Info.IsGroup {
			topicName = fmt.Sprintf("Group: %s", targetJID)
		}

		// Create forum topic
		topic, err := bot.CreateForumTopic(config.TelegramTargetGroupID, topicName, &gotgbot.CreateForumTopicOpts{})
		if err != nil {
			logrus.Errorf("Failed to create forum topic: %v", err)
			return
		}
		topicID = topic.MessageThreadId

		// Save mapping
		if err := chatStorageRepo.SaveTelegramMapping(topicID, targetJID); err != nil {
			logrus.Errorf("Failed to save telegram mapping: %v", err)
		}
		logrus.Infof("Created new Telegram topic %d for WhatsApp JID %s", topicID, targetJID)
	}

	// Prepare Message Content
	messageText := ""

	// Add sender info if group
	senderName := evt.Info.PushName
	if senderName == "" {
		senderName = senderJID.User
	}

	if evt.Info.IsGroup {
		messageText += fmt.Sprintf("<b>%s:</b>\n", html.EscapeString(senderName))
	}

	// Handle Text
	conversation := evt.Message.GetConversation()
	if conversation != "" {
		messageText += html.EscapeString(conversation)
	} else {
		// Handle extended text
		extended := evt.Message.GetExtendedTextMessage()
		if extended != nil {
			messageText += html.EscapeString(extended.GetText())
		}
	}

	// Send Text Message
	if messageText != "" {
		_, err := bot.SendMessage(config.TelegramTargetGroupID, messageText, &gotgbot.SendMessageOpts{
			MessageThreadId: topicID,
			ParseMode:       "HTML",
		})
		if err != nil {
			logrus.Errorf("Failed to send text to Telegram: %v", err)
		}
	}

	// Handle Media
	if config.WhatsappAutoDownloadMedia {
		// Image
		if img := evt.Message.GetImageMessage(); img != nil {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, img)
			if err == nil {
				// Upload photo
				f, _ := os.Open(extracted.MediaPath)
				defer f.Close()
				_, err = bot.SendPhoto(config.TelegramTargetGroupID, gotgbot.InputFileByReader("photo.jpg", f), &gotgbot.SendPhotoOpts{
					MessageThreadId: topicID,
					Caption:         img.GetCaption(),
				})
				if err != nil {
					logrus.Errorf("Failed to send photo to Telegram: %v", err)
				}
			}
		}

		// Document
		if doc := evt.Message.GetDocumentMessage(); doc != nil {
			extracted, err := utils.ExtractMedia(ctx, client, config.PathMedia, doc)
			if err == nil {
				// Upload document
				f, _ := os.Open(extracted.MediaPath)
				defer f.Close()
				_, err = bot.SendDocument(config.TelegramTargetGroupID, gotgbot.InputFileByReader(doc.GetFileName(), f), &gotgbot.SendDocumentOpts{
					MessageThreadId: topicID,
					Caption:         doc.GetCaption(),
				})
				if err != nil {
					logrus.Errorf("Failed to send document to Telegram: %v", err)
				}
			}
		}
	}
}
