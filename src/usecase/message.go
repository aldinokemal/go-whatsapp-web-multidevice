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
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type serviceMessage struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
}

func NewMessageService(chatStorageRepo domainChatStorage.IChatStorageRepository) domainMessage.IMessageUsecase {
	return &serviceMessage{
		chatStorageRepo: chatStorageRepo,
	}
}

func (service serviceMessage) MarkAsRead(ctx context.Context, request domainMessage.MarkAsReadRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateMarkAsRead(ctx, request); err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	ids := []types.MessageID{request.MessageID}
	if err = whatsapp.GetClient().MarkRead(ctx, ids, time.Now(), dataWaRecipient, *whatsapp.GetClient().Store.ID); err != nil {
		return response, err
	}

	logrus.Info(map[string]any{
		"phone":      request.Phone,
		"message_id": request.MessageID,
		"chat":       dataWaRecipient.String(),
		"sender":     whatsapp.GetClient().Store.ID.String(),
	})

	response.MessageID = request.MessageID
	response.Status = fmt.Sprintf("Mark as read success %s", request.MessageID)
	return response, nil
}

func (service serviceMessage) ReactMessage(ctx context.Context, request domainMessage.ReactionRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateReactMessage(ctx, request); err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	msg := &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{
				FromMe:    proto.Bool(true),
				ID:        proto.String(request.MessageID),
				RemoteJID: proto.String(dataWaRecipient.String()),
			},
			Text:              proto.String(request.Emoji),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	}
	ts, err := whatsapp.GetClient().SendMessage(ctx, dataWaRecipient, msg)
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
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	ts, err := whatsapp.GetClient().SendMessage(context.Background(), dataWaRecipient, whatsapp.GetClient().BuildRevoke(dataWaRecipient, types.EmptyJID, request.MessageID))
	if err != nil {
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
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
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
			Index: []string{appstate.IndexDeleteMessageForMe, dataWaRecipient.String(), request.MessageID, isFromMe, whatsapp.GetClient().Store.ID.String()},
			Value: &waSyncAction.SyncActionValue{
				DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
					DeleteMedia:      proto.Bool(true),
					MessageTimestamp: proto.Int64(time.Now().UnixMilli()),
				},
			},
		}},
	}

	if err = whatsapp.GetClient().SendAppState(ctx, patchInfo); err != nil {
		return err
	}
	return nil
}

func (service serviceMessage) UpdateMessage(ctx context.Context, request domainMessage.UpdateMessageRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateUpdateMessage(ctx, request); err != nil {
		return response, err
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	msg := &waE2E.Message{Conversation: proto.String(request.Message)}
	ts, err := whatsapp.GetClient().SendMessage(context.Background(), dataWaRecipient, whatsapp.GetClient().BuildEdit(dataWaRecipient, request.MessageID, msg))
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

	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return err
	}

	isFromMe := true
	if len(request.MessageID) > 22 {
		isFromMe = false
	}

	patchInfo := appstate.BuildStar(dataWaRecipient.ToNonAD(), *whatsapp.GetClient().Store.ID, request.MessageID, isFromMe, request.IsStarred)

	if err = whatsapp.GetClient().SendAppState(ctx, patchInfo); err != nil {
		return err
	}
	return nil
}

// DownloadMedia implements message.IMessageService.
func (service serviceMessage) DownloadMedia(ctx context.Context, request domainMessage.DownloadMediaRequest) (response domainMessage.DownloadMediaResponse, err error) {
	if err = validations.ValidateDownloadMedia(ctx, request); err != nil {
		return response, err
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
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

	// Check if message has media
	if message.MediaType == "" || message.URL == "" {
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

	// Create a downloadable message interface based on media type
	var downloadableMsg interface{}

	switch message.MediaType {
	case "image":
		downloadableMsg = &waE2E.ImageMessage{
			URL:           proto.String(message.URL),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}
	case "video":
		downloadableMsg = &waE2E.VideoMessage{
			URL:           proto.String(message.URL),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}
	case "audio":
		downloadableMsg = &waE2E.AudioMessage{
			URL:           proto.String(message.URL),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}
	case "document":
		downloadableMsg = &waE2E.DocumentMessage{
			URL:           proto.String(message.URL),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
			FileName:      proto.String(message.Filename),
		}
	case "sticker":
		downloadableMsg = &waE2E.StickerMessage{
			URL:           proto.String(message.URL),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}
	default:
		return response, fmt.Errorf("unsupported media type: %s", message.MediaType)
	}

	// Download the media using existing utils.ExtractMedia function
	extractedMedia, err := utils.ExtractMedia(ctx, whatsapp.GetClient(), dateDir, downloadableMsg.(whatsmeow.DownloadableMessage))
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
