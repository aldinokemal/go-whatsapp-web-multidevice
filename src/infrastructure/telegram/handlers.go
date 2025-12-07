package telegram

import (
	"context"
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// HandleTopicMessage handles messages sent to Telegram topics (bridges to WhatsApp)
func HandleTopicMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	msg := ctx.EffectiveMessage
	topicID := msg.MessageThreadId

	// Get WhatsApp JID from mapping
	whatsappJID, err := repo.GetWhatsAppJID(topicID)
	if err != nil {
		logrus.Errorf("Failed to get WhatsApp JID for topic %d: %v", topicID, err)
		return nil
	}
	if whatsappJID == "" {
		// No mapping found, possibly not a bridged topic
		return nil
	}

	logrus.Infof("Bridging Telegram message from topic %d to WhatsApp JID %s", topicID, whatsappJID)

	client := whatsapp.GetClient()
	if client == nil {
		_, _ = msg.Reply(b, "WhatsApp client not connected", nil)
		return nil
	}

	jid, err := types.ParseJID(whatsappJID)
	if err != nil {
		logrus.Errorf("Invalid WhatsApp JID %s: %v", whatsappJID, err)
		return nil
	}

	// Prepare message content
	var waMsg *waE2E.Message

	if msg.Text != "" {
		waMsg = &waE2E.Message{
			Conversation: proto.String(msg.Text),
		}
	} else if msg.Photo != nil {
		// Handle photo (download and send) - Implementation simplified for now
		// In a real implementation, you would download the file using bot.GetFile
		// and then upload it to WhatsApp
		caption := msg.Caption
		if caption == "" {
			caption = "Photo from Telegram"
		}
		waMsg = &waE2E.Message{
			Conversation: proto.String(fmt.Sprintf("[Photo] %s", caption)),
		}
	} else if msg.Document != nil {
		waMsg = &waE2E.Message{
			Conversation: proto.String(fmt.Sprintf("[Document] %s", msg.Document.FileName)),
		}
	} else {
		// Unsupported message type
		return nil
	}

	// Send to WhatsApp
	resp, err := client.SendMessage(context.Background(), jid, waMsg)
	if err != nil {
		logrus.Errorf("Failed to send message to WhatsApp: %v", err)
		_, _ = msg.Reply(b, fmt.Sprintf("Failed to send: %v", err), nil)
		return nil
	}

	// Store sent message in database
	senderJID := ""
	if client.Store.ID != nil {
		senderJID = client.Store.ID.String()
	}

	// Normalize recipient for storage
	normalizedJID := whatsapp.NormalizeJIDFromLID(context.Background(), jid, client)

	err = repo.StoreSentMessageWithContext(
		context.Background(),
		resp.ID,
		senderJID,
		normalizedJID.String(),
		utils.ExtractMessageTextFromProto(waMsg),
		resp.Timestamp,
	)

	if err != nil {
		logrus.Errorf("Failed to store sent message: %v", err)
	}

	return nil
}
