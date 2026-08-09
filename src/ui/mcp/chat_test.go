package mcp

import (
	"context"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubChatService struct {
	domainChat.IChatUsecase
	listed   *domainChat.ListChatsRequest
	fetched  *domainChat.GetChatMessagesRequest
	archived *domainChat.ArchiveChatRequest
}

func (s *stubChatService) ListChats(_ context.Context, r domainChat.ListChatsRequest) (domainChat.ListChatsResponse, error) {
	s.listed = &r
	return domainChat.ListChatsResponse{}, nil
}
func (s *stubChatService) GetChatMessages(_ context.Context, r domainChat.GetChatMessagesRequest) (domainChat.GetChatMessagesResponse, error) {
	s.fetched = &r
	return domainChat.GetChatMessagesResponse{}, nil
}
func (s *stubChatService) ArchiveChat(_ context.Context, r domainChat.ArchiveChatRequest) (domainChat.ArchiveChatResponse, error) {
	s.archived = &r
	return domainChat.ArchiveChatResponse{}, nil
}

type stubUserService struct {
	domainUser.IUserUsecase
	contactsCalled bool
}

func (s *stubUserService) MyListContacts(_ context.Context) (domainUser.MyListContactsResponse, error) {
	s.contactsCalled = true
	return domainUser.MyListContactsResponse{}, nil
}

func TestHandleChatDispatch(t *testing.T) {
	t.Run("list_chats with filters", func(t *testing.T) {
		cs, us := &stubChatService{}, &stubUserService{}
		h := InitMcpChat(cs, us, &stubResolver{})
		_, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "list_chats", "limit": 10, "search": "bob", "has_media": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, cs.listed)
		assert.Equal(t, 10, cs.listed.Limit)
		assert.Equal(t, "bob", cs.listed.Search)
		assert.True(t, cs.listed.HasMedia)
	})

	t.Run("list_contacts", func(t *testing.T) {
		cs, us := &stubChatService{}, &stubUserService{}
		h := InitMcpChat(cs, us, &stubResolver{})
		_, err := h.handleChat(deviceCtx(), callReq(map[string]any{"action": "list_contacts"}))
		require.NoError(t, err)
		assert.True(t, us.contactsCalled)
	})

	t.Run("get_messages with time filters", func(t *testing.T) {
		cs, us := &stubChatService{}, &stubUserService{}
		h := InitMcpChat(cs, us, &stubResolver{})
		_, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "get_messages", "chat_jid": "628@s.whatsapp.net",
			"start_time": "2026-01-01T00:00:00Z", "is_from_me": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, cs.fetched)
		assert.Equal(t, "628@s.whatsapp.net", cs.fetched.ChatJID)
		require.NotNil(t, cs.fetched.StartTime)
		assert.Equal(t, "2026-01-01T00:00:00Z", *cs.fetched.StartTime)
		require.NotNil(t, cs.fetched.IsFromMe)
		assert.True(t, *cs.fetched.IsFromMe)
	})

	t.Run("archive", func(t *testing.T) {
		cs, us := &stubChatService{}, &stubUserService{}
		h := InitMcpChat(cs, us, &stubResolver{})
		_, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "archive", "chat_jid": "628@s.whatsapp.net", "archived": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, cs.archived)
		assert.True(t, cs.archived.Archived)
	})
}
