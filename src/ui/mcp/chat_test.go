package mcp

import (
	"context"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

type stubChatService struct {
	domainChat.IChatUsecase
	listed   *domainChat.ListChatsRequest
	fetched  *domainChat.GetChatMessagesRequest
	archived *domainChat.ArchiveChatRequest
	listResp domainChat.ListChatsResponse
	msgResp  domainChat.GetChatMessagesResponse
}

func (s *stubChatService) ListChats(_ context.Context, r domainChat.ListChatsRequest) (domainChat.ListChatsResponse, error) {
	s.listed = &r
	return s.listResp, nil
}
func (s *stubChatService) GetChatMessages(_ context.Context, r domainChat.GetChatMessagesRequest) (domainChat.GetChatMessagesResponse, error) {
	s.fetched = &r
	return s.msgResp, nil
}
func (s *stubChatService) ArchiveChat(_ context.Context, r domainChat.ArchiveChatRequest) (domainChat.ArchiveChatResponse, error) {
	s.archived = &r
	return domainChat.ArchiveChatResponse{}, nil
}

type stubUserService struct {
	domainUser.IUserUsecase
	contactsCalled bool
	contactsResp   domainUser.MyListContactsResponse
}

func (s *stubUserService) MyListContacts(_ context.Context) (domainUser.MyListContactsResponse, error) {
	s.contactsCalled = true
	return s.contactsResp, nil
}

func TestHandleChatDispatch(t *testing.T) {
	t.Run("list_chats with filters", func(t *testing.T) {
		cs, us := &stubChatService{
			listResp: domainChat.ListChatsResponse{
				Data: []domainChat.ChatInfo{
					{JID: "628123456@s.whatsapp.net", Name: "Alice"},
				},
			},
		}, &stubUserService{}
		h := InitMcpChat(cs, us, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "list_chats", "limit": 10, "search": "bob", "has_media": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, cs.listed)
		assert.Equal(t, 10, cs.listed.Limit)
		assert.Equal(t, "bob", cs.listed.Search)
		assert.True(t, cs.listed.HasMedia)
		require.NotNil(t, res)
		require.NotEmpty(t, res.Content)
		text, ok := mcpg.AsTextContent(res.Content[0])
		require.True(t, ok)
		assert.Contains(t, text.Text, "628123456@s.whatsapp.net")
		assert.Contains(t, text.Text, "Alice")
		assert.NotNil(t, res.StructuredContent)
	})

	t.Run("list_contacts", func(t *testing.T) {
		jid, err := types.ParseJID("628999@s.whatsapp.net")
		require.NoError(t, err)
		cs, us := &stubChatService{}, &stubUserService{
			contactsResp: domainUser.MyListContactsResponse{
				Data: []domainUser.MyListContactsResponseData{
					{JID: jid, Name: "Bob"},
				},
			},
		}
		h := InitMcpChat(cs, us, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{"action": "list_contacts"}))
		require.NoError(t, err)
		assert.True(t, us.contactsCalled)
		require.NotNil(t, res)
		require.NotEmpty(t, res.Content)
		text, ok := mcpg.AsTextContent(res.Content[0])
		require.True(t, ok)
		assert.Contains(t, text.Text, "628999@s.whatsapp.net")
		assert.Contains(t, text.Text, "Bob")
		assert.NotNil(t, res.StructuredContent)
	})

	t.Run("get_messages with time filters", func(t *testing.T) {
		cs, us := &stubChatService{
			msgResp: domainChat.GetChatMessagesResponse{
				Data: []domainChat.MessageInfo{
					{ID: "msg-123", Content: "hello world"},
				},
			},
		}, &stubUserService{}
		h := InitMcpChat(cs, us, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
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
		require.NotNil(t, res)
		require.NotEmpty(t, res.Content)
		text, ok := mcpg.AsTextContent(res.Content[0])
		require.True(t, ok)
		assert.Contains(t, text.Text, "msg-123")
		assert.Contains(t, text.Text, "hello world")
		assert.NotNil(t, res.StructuredContent)
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
