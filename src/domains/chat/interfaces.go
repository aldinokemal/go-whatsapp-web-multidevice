package chat

import (
	"context"
)

// IChatUsecase defines the interface for chat-related operations
type IChatUsecase interface {
	ListChats(ctx context.Context, request ListChatsRequest) (response ListChatsResponse, err error)
	GetChatMessages(ctx context.Context, request GetChatMessagesRequest) (response GetChatMessagesResponse, err error)
	PinChat(ctx context.Context, request PinChatRequest) (response PinChatResponse, err error)
}
