package usecase

import (
	"context"
	"fmt"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

type serviceChat struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
}

func NewChatService(chatStorageRepo domainChatStorage.IChatStorageRepository) domainChat.IChatUsecase {
	return &serviceChat{
		chatStorageRepo: chatStorageRepo,
	}
}

func (service serviceChat) ListChats(ctx context.Context, request domainChat.ListChatsRequest) (response domainChat.ListChatsResponse, err error) {
	if err = validations.ValidateListChats(ctx, &request); err != nil {
		return response, err
	}

	// Create filter from request
	filter := &domainChatStorage.ChatFilter{
		Limit:      request.Limit,
		Offset:     request.Offset,
		SearchName: request.Search,
		HasMedia:   request.HasMedia,
	}

	// Get chats from storage
	chats, err := service.chatStorageRepo.GetChats(filter)
	if err != nil {
		logrus.WithError(err).Error("Failed to get chats from storage")
		return response, err
	}

	// Get total count for pagination
	totalCount, err := service.chatStorageRepo.GetTotalChatCount()
	if err != nil {
		logrus.WithError(err).Error("Failed to get total chat count")
		// Continue with partial data
		totalCount = 0
	}

	// Convert entities to domain objects
	client := whatsapp.GetClient()
	chatInfos := make([]domainChat.ChatInfo, 0, len(chats))
	for _, chat := range chats {
		jidStr := chat.JID
		name := chat.Name

		// Try to normalize JID if it's an LID
		// This handles the case where the chat was stored with an LID JID
		// but we want to display the phone number JID
		if jid, err := types.ParseJID(chat.JID); err == nil && jid.Server == types.HiddenUserServer {
			normalized := whatsapp.NormalizeJIDFromLID(ctx, jid, client)
			if normalized.Server != types.HiddenUserServer {
				jidStr = normalized.String()
				// Update name if it was just the LID ID
				if name == jid.User {
					name = normalized.User
				}
			}
		}

		chatInfo := domainChat.ChatInfo{
			JID:                 jidStr,
			Name:                name,
			LastMessageTime:     chat.LastMessageTime.Format(time.RFC3339),
			EphemeralExpiration: chat.EphemeralExpiration,
			CreatedAt:           chat.CreatedAt.Format(time.RFC3339),
			UpdatedAt:           chat.UpdatedAt.Format(time.RFC3339),
		}
		chatInfos = append(chatInfos, chatInfo)
	}

	// Create pagination response
	pagination := domainChat.PaginationResponse{
		Limit:  request.Limit,
		Offset: request.Offset,
		Total:  int(totalCount),
	}

	response.Data = chatInfos
	response.Pagination = pagination

	logrus.WithFields(logrus.Fields{
		"total_chats": len(chatInfos),
		"limit":       request.Limit,
		"offset":      request.Offset,
	}).Info("Listed chats successfully")

	return response, nil
}

func (service serviceChat) GetChatMessages(ctx context.Context, request domainChat.GetChatMessagesRequest) (response domainChat.GetChatMessagesResponse, err error) {
	if err = validations.ValidateGetChatMessages(ctx, &request); err != nil {
		return response, err
	}

	// Get chat info first
	// If the request comes with a Phone JID, but we only have LID in DB, we need to handle that
	targetJID := request.ChatJID
	chat, err := service.chatStorageRepo.GetChat(targetJID)
	if err != nil {
		logrus.WithError(err).WithField("chat_jid", targetJID).Error("Failed to get chat info")
		return response, err
	}

	// If chat not found, try to check if we can resolve it via LID
	if chat == nil {
		client := whatsapp.GetClient()
		if parsedJID, err := types.ParseJID(request.ChatJID); err == nil && parsedJID.Server != types.HiddenUserServer {
			// Try to get LID for this phone number
			lidJID := whatsapp.GetLIDFromPhone(ctx, parsedJID, client)
			if lidJID.Server == types.HiddenUserServer {
				// We found an LID, try to fetch chat with this LID
				logrus.Infof("Chat not found for %s, trying LID %s", request.ChatJID, lidJID.String())
				lidChat, err := service.chatStorageRepo.GetChat(lidJID.String())
				if err == nil && lidChat != nil {
					chat = lidChat
					targetJID = lidChat.JID // Update targetJID to use for message query
				}
			}
		}
	}

	if chat == nil {
		return response, fmt.Errorf("chat with JID %s not found", request.ChatJID)
	}

	// Create message filter from request
	filter := &domainChatStorage.MessageFilter{
		ChatJID:   targetJID, // Use the resolved JID (could be LID)
		Limit:     request.Limit,
		Offset:    request.Offset,
		MediaOnly: request.MediaOnly,
		IsFromMe:  request.IsFromMe,
	}

	// Parse time filters if provided
	if request.StartTime != nil && *request.StartTime != "" {
		startTime, err := time.Parse(time.RFC3339, *request.StartTime)
		if err != nil {
			return response, fmt.Errorf("invalid start_time format: %v", err)
		}
		filter.StartTime = &startTime
	}

	if request.EndTime != nil && *request.EndTime != "" {
		endTime, err := time.Parse(time.RFC3339, *request.EndTime)
		if err != nil {
			return response, fmt.Errorf("invalid end_time format: %v", err)
		}
		filter.EndTime = &endTime
	}

	// Get messages from storage
	var messages []*domainChatStorage.Message
	if request.Search != "" {
		// Use search functionality if search query is provided
		messages, err = service.chatStorageRepo.SearchMessages(request.ChatJID, request.Search, request.Limit)
		if err != nil {
			logrus.WithError(err).WithField("chat_jid", request.ChatJID).Error("Failed to search messages")
			return response, err
		}
	} else {
		// Use regular filter
		messages, err = service.chatStorageRepo.GetMessages(filter)
		if err != nil {
			logrus.WithError(err).WithField("chat_jid", request.ChatJID).Error("Failed to get messages")
			return response, err
		}
	}

	// Get total message count for pagination
	totalCount, err := service.chatStorageRepo.GetChatMessageCount(targetJID)
	if err != nil {
		logrus.WithError(err).WithField("chat_jid", targetJID).Error("Failed to get message count")
		// Continue with partial data
		totalCount = 0
	}

	// Convert entities to domain objects
	client := whatsapp.GetClient()
	messageInfos := make([]domainChat.MessageInfo, 0, len(messages))
	for _, message := range messages {
		sender := message.Sender
		chatJID := message.ChatJID

		// Normalize sender and chatJID if they are LIDs
		if senderJID, err := types.ParseJID(sender); err == nil && senderJID.Server == types.HiddenUserServer {
			normalized := whatsapp.NormalizeJIDFromLID(ctx, senderJID, client)
			sender = normalized.String()
		}

		if cJID, err := types.ParseJID(chatJID); err == nil && cJID.Server == types.HiddenUserServer {
			normalized := whatsapp.NormalizeJIDFromLID(ctx, cJID, client)
			chatJID = normalized.String()
		}

		messageInfo := domainChat.MessageInfo{
			ID:         message.ID,
			ChatJID:    chatJID,
			SenderJID:  sender,
			Content:    message.Content,
			Timestamp:  message.Timestamp.Format(time.RFC3339),
			IsFromMe:   message.IsFromMe,
			MediaType:  message.MediaType,
			Filename:   message.Filename,
			URL:        message.URL,
			FileLength: message.FileLength,
			CreatedAt:  message.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  message.UpdatedAt.Format(time.RFC3339),
		}
		messageInfos = append(messageInfos, messageInfo)
	}

	// Create chat info for response
	// Ensure we return the requested JID (Phone) even if we fetched from LID
	respChatJID := chat.JID
	respChatName := chat.Name

	if jid, err := types.ParseJID(respChatJID); err == nil && jid.Server == types.HiddenUserServer {
		normalized := whatsapp.NormalizeJIDFromLID(ctx, jid, client)
		if normalized.Server != types.HiddenUserServer {
			respChatJID = normalized.String()
			if respChatName == jid.User {
				respChatName = normalized.User
			}
		}
	}

	chatInfo := domainChat.ChatInfo{
		JID:                 respChatJID,
		Name:                respChatName,
		LastMessageTime:     chat.LastMessageTime.Format(time.RFC3339),
		EphemeralExpiration: chat.EphemeralExpiration,
		CreatedAt:           chat.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           chat.UpdatedAt.Format(time.RFC3339),
	}

	// Create pagination response
	pagination := domainChat.PaginationResponse{
		Limit:  request.Limit,
		Offset: request.Offset,
		Total:  int(totalCount),
	}

	response.Data = messageInfos
	response.Pagination = pagination
	response.ChatInfo = chatInfo

	logrus.WithFields(logrus.Fields{
		"chat_jid":       request.ChatJID,
		"total_messages": len(messageInfos),
		"limit":          request.Limit,
		"offset":         request.Offset,
	}).Info("Retrieved chat messages successfully")

	return response, nil
}

func (service serviceChat) PinChat(ctx context.Context, request domainChat.PinChatRequest) (response domainChat.PinChatResponse, err error) {
	if err = validations.ValidatePinChat(ctx, &request); err != nil {
		return response, err
	}

	// Validate JID and ensure connection
	targetJID, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.ChatJID)
	if err != nil {
		return response, err
	}

	// Build pin patch using whatsmeow's BuildPin
	patchInfo := appstate.BuildPin(targetJID, request.Pinned)

	// Send app state update
	if err = whatsapp.GetClient().SendAppState(ctx, patchInfo); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"chat_jid": request.ChatJID,
			"pinned":   request.Pinned,
		}).Error("Failed to send pin chat app state")
		return response, err
	}

	// Build response
	response.Status = "success"
	response.ChatJID = request.ChatJID
	response.Pinned = request.Pinned

	if request.Pinned {
		response.Message = "Chat pinned successfully"
	} else {
		response.Message = "Chat unpinned successfully"
	}

	logrus.WithFields(logrus.Fields{
		"chat_jid": request.ChatJID,
		"pinned":   request.Pinned,
	}).Info("Chat pin operation completed successfully")

	return response, nil
}
