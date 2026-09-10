package mcp

import (
	"encoding/json"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resultText(t *testing.T, res *mcpg.CallToolResult) string {
	t.Helper()
	require.NotNil(t, res)
	require.Len(t, res.Content, 1)
	text, ok := res.Content[0].(mcpg.TextContent)
	require.True(t, ok, "expected a text content block")
	return text.Text
}

func TestStructuredWithJSON(t *testing.T) {
	cases := []struct {
		name       string
		structured any
		summary    string
		want       string
	}{
		{
			name:       "map payload",
			structured: map[string]any{"is_connected": true},
			summary:    "connected=true logged_in=true",
			want:       "connected=true logged_in=true\n{\"is_connected\":true}",
		},
		{
			name:       "slice payload",
			structured: []string{"628@s.whatsapp.net"},
			summary:    "Found 1 contacts",
			want:       "Found 1 contacts\n[\"628@s.whatsapp.net\"]",
		},
		{
			name:       "nil payload",
			structured: nil,
			summary:    "Found 0 contacts",
			want:       "Found 0 contacts\nnull",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := structuredWithJSON(tc.structured, tc.summary)
			assert.Equal(t, tc.want, resultText(t, res))
			assert.Equal(t, tc.structured, res.StructuredContent)
		})
	}

	t.Run("unmarshalable payload drops structured content", func(t *testing.T) {
		res := structuredWithJSON(make(chan int), "Retrieved 0 chats")
		assert.Equal(t, "Retrieved 0 chats", resultText(t, res))
		assert.Nil(t, res.StructuredContent)
		_, err := json.Marshal(res)
		assert.NoError(t, err)
	})
}

func TestReadToolsSerializePayloadIntoText(t *testing.T) {
	t.Run("list_chats", func(t *testing.T) {
		cs := &stubChatService{listResp: domainChat.ListChatsResponse{
			Data: []domainChat.ChatInfo{{JID: "628@s.whatsapp.net", Name: "Bob"}},
		}}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{"action": "list_chats"}))
		require.NoError(t, err)

		text := resultText(t, res)
		assert.Contains(t, text, "Retrieved 1 chats")
		payload, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		assert.Contains(t, text, string(payload))
		assert.Contains(t, text, `"jid":"628@s.whatsapp.net"`)
	})

	t.Run("group participants", func(t *testing.T) {
		svc := &stubGroupService{partsResp: domainGroup.GetGroupParticipantsResponse{
			GroupID:      "123@g.us",
			Participants: []domainGroup.GroupParticipant{{JID: "628@s.whatsapp.net"}},
		}}
		h := InitMcpGroup(svc, &stubResolver{})
		res, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "participants", "group_id": "123@g.us",
		}))
		require.NoError(t, err)

		text := resultText(t, res)
		assert.Contains(t, text, "Group 123@g.us has 1 participants")
		payload, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		assert.Contains(t, text, string(payload))
	})

	t.Run("write actions keep the summary only", func(t *testing.T) {
		cs := &stubChatService{}
		h := InitMcpChat(cs, &stubUserService{}, &stubResolver{})
		res, err := h.handleChat(deviceCtx(), callReq(map[string]any{
			"action": "archive", "chat_jid": "628@s.whatsapp.net", "archived": true,
		}))
		require.NoError(t, err)
		assert.Equal(t, "Chat archived", resultText(t, res))
	})
}
