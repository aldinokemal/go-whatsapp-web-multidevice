package chat

import (
	"context"
)

// IChatUsecase defines the interface for chat-related operations
type IChatUsecase interface {
	ListChats(ctx context.Context, request ListChatsRequest) (response ListChatsResponse, err error)
	GetChatMessages(ctx context.Context, request GetChatMessagesRequest) (response GetChatMessagesResponse, err error)
	PinChat(ctx context.Context, request PinChatRequest) (response PinChatResponse, err error)
	SetDisappearingTimer(ctx context.Context, request SetDisappearingTimerRequest) (response SetDisappearingTimerResponse, err error)
	ArchiveChat(ctx context.Context, request ArchiveChatRequest) (response ArchiveChatResponse, err error)
	// RequestChatHistory asks the phone (via an on-demand history sync request)
	// for older messages of a chat, anchored at the oldest message currently
	// stored for that chat.
	RequestChatHistory(ctx context.Context, request RequestChatHistoryRequest) (response RequestChatHistoryResponse, err error)
}
