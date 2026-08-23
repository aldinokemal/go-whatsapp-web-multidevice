package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type markReadFunc func(context.Context, *whatsmeow.Client, []types.MessageID, time.Time, types.JID, types.JID, ...types.ReceiptType) error

type serviceMessage struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
	validateJIDFn   func(client *whatsmeow.Client, jid string) (types.JID, error)
	markReadFn      markReadFunc
	sendMessageFn   func(ctx context.Context, client *whatsmeow.Client, recipient types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error)
	sendAppStateFn  func(ctx context.Context, client *whatsmeow.Client, patch appstate.PatchInfo) error
}

func NewMessageService(chatStorageRepo domainChatStorage.IChatStorageRepository) domainMessage.IMessageUsecase {
	return &serviceMessage{
		chatStorageRepo: chatStorageRepo,
	}
}

func (service serviceMessage) validateJID(client *whatsmeow.Client, jid string) (types.JID, error) {
	if service.validateJIDFn != nil {
		return service.validateJIDFn(client, jid)
	}
	return utils.ValidateJidWithLogin(client, jid)
}

func (service serviceMessage) sendMessage(ctx context.Context, client *whatsmeow.Client, recipient types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error) {
	if service.sendMessageFn != nil {
		return service.sendMessageFn(ctx, client, recipient, message)
	}
	return client.SendMessage(ctx, recipient, message)
}

func (service serviceMessage) markRead(ctx context.Context, client *whatsmeow.Client, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error {
	if service.markReadFn != nil {
		return service.markReadFn(ctx, client, ids, timestamp, chat, sender, receiptTypeExtra...)
	}
	return client.MarkRead(ctx, ids, timestamp, chat, sender, receiptTypeExtra...)
}

func (service serviceMessage) sendAppState(ctx context.Context, client *whatsmeow.Client, patch appstate.PatchInfo) error {
	if service.sendAppStateFn != nil {
		return service.sendAppStateFn(ctx, client, patch)
	}
	return client.SendAppState(ctx, patch)
}

func (service serviceMessage) deleteStoredMessage(ctx context.Context, client *whatsmeow.Client, messageID, chatJID string) error {
	deviceID := deviceIDFromContext(ctx)
	if deviceID == "" && client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}
	if deviceID == "" {
		return fmt.Errorf("WhatsApp action succeeded, but local message %s could not be deleted without a device ID", messageID)
	}
	if service.chatStorageRepo == nil {
		return fmt.Errorf("WhatsApp action succeeded, but local message %s could not be deleted without chat storage", messageID)
	}
	if err := service.chatStorageRepo.DeleteMessageByDevice(deviceID, messageID, chatJID); err != nil {
		return fmt.Errorf("WhatsApp action succeeded, but failed to delete local message %s: %w", messageID, err)
	}
	return nil
}

func (service serviceMessage) MarkAsRead(ctx context.Context, request domainMessage.MarkAsReadRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateMarkAsRead(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	ids := []types.MessageID{request.MessageID}
	if err = client.MarkRead(ctx, ids, time.Now(), dataWaRecipient, *client.Store.ID); err != nil {
		return response, err
	}

	logrus.Info(map[string]any{
		"phone":      request.Phone,
		"message_id": request.MessageID,
		"chat":       dataWaRecipient.String(),
		"sender":     client.Store.ID.String(),
	})

	response.MessageID = request.MessageID
	response.Status = fmt.Sprintf("Mark as read success %s", request.MessageID)
	return response, nil
}

func (service serviceMessage) MarkAsPlayed(ctx context.Context, request domainMessage.MarkAsPlayedRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateMarkAsRead(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	chatJID, err := service.validateJID(client, request.Phone)
	if err != nil {
		return response, err
	}

	if service.chatStorageRepo == nil {
		return response, fmt.Errorf("cannot mark message %s as played without chat storage", request.MessageID)
	}
	message, err := service.chatStorageRepo.GetMessageByIDAndDevice(deviceIDFromContext(ctx), request.MessageID)
	if err != nil {
		return response, fmt.Errorf("failed to resolve message %s: %w", request.MessageID, err)
	}
	if message == nil {
		return response, fmt.Errorf("message with ID %s not found for current device", request.MessageID)
	}
	storedChatJID, err := utils.ParseJID(message.ChatJID)
	if err != nil {
		return response, fmt.Errorf("failed to parse chat for message %s: %w", request.MessageID, err)
	}
	if storedChatJID.ToNonAD().String() != chatJID.ToNonAD().String() {
		return response, pkgError.ValidationError(fmt.Sprintf("message %s does not belong to chat %s", request.MessageID, chatJID.ToNonAD().String()))
	}
	if message.MediaType != "audio" && message.MediaType != "ptt" {
		return response, pkgError.ValidationError(fmt.Sprintf("message %s is not an audio message", request.MessageID))
	}
	if message.IsFromMe {
		return response, pkgError.ValidationError(fmt.Sprintf("message %s is not an incoming message", request.MessageID))
	}

	senderJID, err := utils.ParseJID(message.Sender)
	if err != nil {
		return response, fmt.Errorf("failed to parse sender for message %s: %w", request.MessageID, err)
	}
	if chatJID.Server == types.GroupServer && senderJID.User == "" {
		return response, fmt.Errorf("sender is missing for group message %s", request.MessageID)
	}
	if err = service.markRead(
		ctx,
		client,
		[]types.MessageID{request.MessageID},
		time.Now(),
		chatJID,
		senderJID,
		types.ReceiptTypePlayed,
	); err != nil {
		return response, err
	}

	logrus.Info(map[string]any{
		"phone":        request.Phone,
		"message_id":   request.MessageID,
		"chat":         chatJID.String(),
		"sender":       senderJID.String(),
		"receipt_type": types.ReceiptTypePlayed,
	})

	response.MessageID = request.MessageID
	response.Status = fmt.Sprintf("Mark as played success %s", request.MessageID)
	return response, nil
}

func (service serviceMessage) ReactMessage(ctx context.Context, request domainMessage.ReactionRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateReactMessage(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	// Determine the sender of the original message for BuildReaction.
	// BuildReaction uses BuildMessageKey internally, which correctly sets the
	// Participant field for group chats — required by the WhatsApp protocol.
	// An empty JID means "message was from me".
	senderJID := types.EmptyJID
	message, err := service.chatStorageRepo.GetMessageByID(request.MessageID)
	if err != nil {
		logrus.Warnf("Failed to lookup message %s for reaction: %v, using heuristic", request.MessageID, err)
		if len(request.MessageID) > 22 {
			if dataWaRecipient.Server == types.GroupServer {
				logrus.Warnf("Cannot determine original sender for group reaction to %s — reaction may not be delivered", request.MessageID)
			}
		}
	} else if message != nil {
		if !message.IsFromMe && message.Sender != "" {
			parsed, parseErr := utils.ParseJID(message.Sender)
			if parseErr == nil {
				senderJID = parsed
			} else {
				logrus.Warnf("Failed to parse sender JID '%s' for reaction: %v", message.Sender, parseErr)
			}
		}
	} else {
		logrus.Debugf("Message %s not found in database, assuming sent by me", request.MessageID)
	}

	// BuildReaction correctly constructs the MessageKey with Participant field
	// for group chats, which is required for the reaction to be delivered.
	msg := client.BuildReaction(dataWaRecipient, senderJID, request.MessageID, request.Emoji)
	ts, err := client.SendMessage(ctx, dataWaRecipient, msg)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Reaction sent to %s (server timestamp: %s)", request.Phone, ts.Timestamp)
	return response, nil
}

func (service serviceMessage) RevokeMessage(ctx context.Context, request domainMessage.RevokeRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateRevokeMessage(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := service.validateJID(client, request.Phone)
	if err != nil {
		return response, err
	}

	// Resolve the original sender so group admins can revoke other members'
	// messages. BuildRevoke treats types.EmptyJID as "message was from me";
	// any other JID is admin-revoke and requires the bot to be group admin.
	// WhatsApp message IDs are globally unique, so a cross-device lookup
	// via GetMessageByID yields the same sender regardless of which device
	// owns the row.
	senderJID := types.EmptyJID
	message, lookupErr := service.chatStorageRepo.GetMessageByID(request.MessageID)
	if lookupErr != nil {
		logrus.Warnf("Failed to lookup message %s for revoke: %v, assuming self-revoke", request.MessageID, lookupErr)
	} else if message != nil && !message.IsFromMe && message.Sender != "" {
		parsed, parseErr := utils.ParseJID(message.Sender)
		if parseErr != nil {
			logrus.Warnf("Failed to parse sender JID '%s' for revoke: %v", message.Sender, parseErr)
		} else {
			// Stored senders can still be @lid; whatsmeow's Revoke needs
			// the phone-number form or it rejects the request at the wire.
			senderJID = whatsapp.NormalizeJIDFromLID(ctx, parsed, client)
		}
	}

	ts, err := service.sendMessage(ctx, client, dataWaRecipient, client.BuildRevoke(dataWaRecipient, senderJID, request.MessageID))
	if err != nil {
		return response, err
	}
	if err := service.deleteStoredMessage(ctx, client, request.MessageID, dataWaRecipient.ToNonAD().String()); err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Revoke success %s (server timestamp: %s)", request.Phone, ts.Timestamp)
	return response, nil
}

func (service serviceMessage) DeleteMessage(ctx context.Context, request domainMessage.DeleteRequest) (err error) {
	if err = validations.ValidateDeleteMessage(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	dataWaRecipient, err := service.validateJID(client, request.Phone)
	if err != nil {
		return err
	}

	isFromMe := "1"
	if len(request.MessageID) > 22 {
		isFromMe = "0"
	}

	patchInfo := appstate.PatchInfo{
		Timestamp: time.Now(),
		Type:      appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index: []string{appstate.IndexDeleteMessageForMe, dataWaRecipient.String(), request.MessageID, isFromMe, client.Store.ID.String()},
			Value: &waSyncAction.SyncActionValue{
				DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
					DeleteMedia:      proto.Bool(true),
					MessageTimestamp: proto.Int64(time.Now().UnixMilli()),
				},
			},
		}},
	}

	if err = service.sendAppState(ctx, client, patchInfo); err != nil {
		return err
	}
	return service.deleteStoredMessage(ctx, client, request.MessageID, dataWaRecipient.ToNonAD().String())
}

func (service serviceMessage) UpdateMessage(ctx context.Context, request domainMessage.UpdateMessageRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateUpdateMessage(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	msg := &waE2E.Message{Conversation: proto.String(request.Message)}
	ts, err := client.SendMessage(ctx, dataWaRecipient, client.BuildEdit(dataWaRecipient, request.MessageID, msg))
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Update message success %s (server timestamp: %s)", request.Phone, ts.Timestamp)
	return response, nil
}

// StarMessage implements message.IMessageService.
func (service serviceMessage) StarMessage(ctx context.Context, request domainMessage.StarRequest) (err error) {
	if err = validations.ValidateStarMessage(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return err
	}

	isFromMe := true
	if len(request.MessageID) > 22 {
		isFromMe = false
	}

	patchInfo := appstate.BuildStar(dataWaRecipient.ToNonAD(), *client.Store.ID, request.MessageID, isFromMe, request.IsStarred)

	if err = client.SendAppState(ctx, patchInfo); err != nil {
		return err
	}
	return nil
}

// DownloadMedia implements message.IMessageService.
func (service serviceMessage) DownloadMedia(ctx context.Context, request domainMessage.DownloadMediaRequest) (response domainMessage.DownloadMediaResponse, err error) {
	if err = validations.ValidateDownloadMedia(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	// Query the message from chat storage
	message, err := service.chatStorageRepo.GetMessageByID(request.MessageID)
	if err != nil {
		return response, fmt.Errorf("message not found: %v", err)
	}

	if message == nil {
		return response, fmt.Errorf("message with ID %s not found", request.MessageID)
	}

	directPath := utils.ResolveMediaDirectPath(message.DirectPath, message.URL)

	// Check if message has media
	if message.MediaType == "" || directPath == "" {
		return response, fmt.Errorf("message %s does not contain downloadable media", request.MessageID)
	}

	// Verify the message is from the specified chat
	if message.ChatJID != dataWaRecipient.String() {
		return response, fmt.Errorf("message %s does not belong to chat %s", request.MessageID, dataWaRecipient.String())
	}

	// Create directory structure for organized storage
	chatDir := filepath.Join(config.PathMedia, utils.ExtractPhoneNumber(message.ChatJID))
	dateDir := filepath.Join(chatDir, message.Timestamp.Format("2006-01-02"))

	err = os.MkdirAll(dateDir, 0755)
	if err != nil {
		return response, fmt.Errorf("failed to create directory: %v", err)
	}

	downloadableMsg, err := utils.BuildDownloadableMessage(
		message.MediaType,
		message.URL,
		directPath,
		message.Filename,
		message.MediaKey,
		message.FileSHA256,
		message.FileEncSHA256,
		message.FileLength,
	)
	if err != nil {
		return response, fmt.Errorf("unsupported media type: %s", message.MediaType)
	}

	// Download the media using existing utils.ExtractMedia function
	extractedMedia, err := utils.ExtractMedia(ctx, client, dateDir, downloadableMsg)
	if err != nil {
		return response, fmt.Errorf("failed to download media: %v", err)
	}

	// Get file size
	fileInfo, err := os.Stat(extractedMedia.MediaPath)
	if err != nil {
		logrus.Warnf("Could not get file size for %s: %v", extractedMedia.MediaPath, err)
	}

	// Build response
	response.MessageID = request.MessageID
	response.Status = fmt.Sprintf("Media downloaded successfully to %s", extractedMedia.MediaPath)
	response.MediaType = message.MediaType
	response.Filename = filepath.Base(extractedMedia.MediaPath)
	response.FilePath = extractedMedia.MediaPath
	if fileInfo != nil {
		response.FileSize = fileInfo.Size()
	}

	logrus.Info(map[string]any{
		"message_id": request.MessageID,
		"phone":      request.Phone,
		"chat":       dataWaRecipient.String(),
		"media_type": response.MediaType,
		"file_path":  response.FilePath,
		"file_size":  response.FileSize,
	})

	return response, nil
}
