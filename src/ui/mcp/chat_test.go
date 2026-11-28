package mcp

import (
	"context"
	"errors"
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
	pinned   *domainChat.PinChatRequest
	read     *domainChat.MarkChatAsReadRequest
	timer    *domainChat.SetDisappearingTimerRequest
	history  *domainChat.RequestChatHistoryRequest
	err      error
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
func (s *stubChatService) PinChat(_ context.Context, r domainChat.PinChatRequest) (domainChat.PinChatResponse, error) {
	s.pinned = &r
	return domainChat.PinChatResponse{Message: "pinned"}, s.err
}
func (s *stubChatService) MarkChatAsRead(_ context.Context, r domainChat.MarkChatAsReadRequest) (domainChat.MarkChatAsReadResponse, error) {
	s.read = &r
	return domainChat.MarkChatAsReadResponse{Message: "marked"}, s.err
}
func (s *stubChatService) SetDisappearingTimer(_ context.Context, r domainChat.SetDisappearingTimerRequest) (domainChat.SetDisappearingTimerResponse, error) {
	s.timer = &r
	return domainChat.SetDisappearingTimerResponse{Message: "timer set"}, s.err
}
func (s *stubChatService) RequestChatHistory(_ context.Context, r domainChat.RequestChatHistoryRequest) (domainChat.RequestChatHistoryResponse, error) {
	s.history = &r
	return domainChat.RequestChatHistoryResponse{ChatJID: r.ChatJID, AnchorMessageID: "OLD1"}, s.err
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

	t.Run("pin", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "pin", "chat_jid": "628@s.whatsapp.net", "pinned": true,
		}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
		require.NotNil(t, cs.pinned)
		assert.Equal(t, "628@s.whatsapp.net", cs.pinned.ChatJID)
		assert.True(t, cs.pinned.Pinned)
	})

	t.Run("read", func(t *testing.T) {
		for _, read := range []bool{true, false} {
			cs := &stubChatService{}
			h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
			res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
				"action": "read", "chat_jid": "628@s.whatsapp.net", "read": read,
			}))
			require.NoError(t, err)
			assert.False(t, res.IsError)
			require.NotNil(t, cs.read)
			assert.Equal(t, "628@s.whatsapp.net", cs.read.ChatJID)
			require.NotNil(t, cs.read.Read)
			assert.Equal(t, read, *cs.read.Read)
		}
	})

	t.Run("read without the flag", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		_, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "read", "chat_jid": "628@s.whatsapp.net",
		}))
		require.NoError(t, err)
		require.NotNil(t, cs.read)
		assert.Nil(t, cs.read.Read, "a missing flag must reach validation as nil, not as unread")
	})

	t.Run("set_disappearing", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "set_disappearing", "chat_jid": "628@s.whatsapp.net", "timer_seconds": float64(604800),
		}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
		require.NotNil(t, cs.timer)
		assert.Equal(t, uint32(604800), cs.timer.TimerSeconds)
	})

	t.Run("set_disappearing zero disables", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		_, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "set_disappearing", "chat_jid": "628@s.whatsapp.net", "timer_seconds": float64(0),
		}))
		require.NoError(t, err)
		require.NotNil(t, cs.timer)
		assert.Equal(t, uint32(0), cs.timer.TimerSeconds)
	})

	t.Run("new actions surface usecase errors", func(t *testing.T) {
		for _, args := range []map[string]any{
			{"action": "pin", "pinned": true},
			{"action": "set_disappearing", "timer_seconds": float64(86400)},
			{"action": "request_history"},
		} {
			args["chat_jid"] = "628@s.whatsapp.net"
			h := InitMcpChat(&stubChatService{err: errors.New("boom")}, &stubUserService{}, &stubResolver{})
			res, err := h.handleChat(deviceCtx(), callReq(args))
			require.NoError(t, err)
			assert.True(t, res.IsError, args["action"])
			assert.Equal(t, "boom", resultText(t, res))
		}
	})

	t.Run("request_history", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "request_history", "chat_jid": "628@s.whatsapp.net", "count": float64(100),
		}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
		require.NotNil(t, cs.history)
		assert.Equal(t, 100, cs.history.Count)
		assert.Contains(t, resultText(t, res), "History sync requested")
		assert.NotNil(t, res.StructuredContent)
	})

	t.Run("set_disappearing rejects a timer that would wrap to 0", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "set_disappearing", "chat_jid": "628@s.whatsapp.net", "timer_seconds": float64(4294967296),
		}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Nil(t, cs.timer)
	})

	t.Run("chat_jid is sanitized like REST send", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		_, err := h.handleChat(deviceCtx(), callReq(map[string]any{"action": "request_history", "chat_jid": "120363025246125486"}))
		require.NoError(t, err)
		require.NotNil(t, cs.history)
		assert.Equal(t, "120363025246125486@g.us", cs.history.ChatJID)
		_, err = h.handleChat(deviceCtx(), callReq(map[string]any{"action": "pin", "chat_jid": "628123", "pinned": true}))
		require.NoError(t, err)
		require.NotNil(t, cs.pinned)
		assert.Equal(t, "628123@s.whatsapp.net", cs.pinned.ChatJID)
	})
}
