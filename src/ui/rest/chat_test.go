package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestChatJIDParamDecodesPercentEncoding(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "percent-encoded group JID",
			path: "/chat/120363151317289139%40g.us/messages",
			want: "120363151317289139@g.us",
		},
		{
			name: "raw group JID",
			path: "/chat/120363151317289139@g.us/messages",
			want: "120363151317289139@g.us",
		},
		{
			name: "percent-encoded user JID",
			path: "/chat/6289685028129%40s.whatsapp.net/messages",
			want: "6289685028129@s.whatsapp.net",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			var got string
			app.Get("/chat/:chat_jid/messages", func(c fiber.Ctx) error {
				jid, err := chatJIDParam(c)
				require.NoError(t, err)
				got = jid
				return c.SendStatus(fiber.StatusOK)
			})

			resp, err := app.Test(httptest.NewRequest("GET", tt.path, nil))
			require.NoError(t, err)
			require.Equal(t, fiber.StatusOK, resp.StatusCode)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestChatJIDParamRejectsMalformedEscape(t *testing.T) {
	app := fiber.New()
	// The decode error path responds before the service is touched, so a
	// zero-value controller is enough to exercise the real handler.
	controller := &Chat{}
	app.Get("/chat/:chat_jid/messages", controller.GetChatMessages)

	req := httptest.NewRequest("GET", "/chat/placeholder/messages", nil)
	// httptest.NewRequest rejects invalid escapes up front, so smuggle the
	// malformed URI past net/url via the Opaque field.
	req.URL.Opaque = "/chat/%zz/messages"

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var envelope utils.ResponseData
	require.NoError(t, json.Unmarshal(body, &envelope), "error must be the JSON envelope, got: %s", body)
	require.Equal(t, "BAD_REQUEST", envelope.Code)
	require.Contains(t, envelope.Message, "invalid chat_jid path parameter")
}

// requestChatHistorySpy is a minimal domainChat.IChatUsecase stub that only
// records the RequestChatHistory call, for exercising the REST handler alone.
type requestChatHistorySpy struct {
	lastRequest domainChat.RequestChatHistoryRequest
	calls       int
}

func (s *requestChatHistorySpy) ListChats(context.Context, domainChat.ListChatsRequest) (domainChat.ListChatsResponse, error) {
	return domainChat.ListChatsResponse{}, nil
}

func (s *requestChatHistorySpy) GetChatMessages(context.Context, domainChat.GetChatMessagesRequest) (domainChat.GetChatMessagesResponse, error) {
	return domainChat.GetChatMessagesResponse{}, nil
}

func (s *requestChatHistorySpy) PinChat(context.Context, domainChat.PinChatRequest) (domainChat.PinChatResponse, error) {
	return domainChat.PinChatResponse{}, nil
}

func (s *requestChatHistorySpy) SetDisappearingTimer(context.Context, domainChat.SetDisappearingTimerRequest) (domainChat.SetDisappearingTimerResponse, error) {
	return domainChat.SetDisappearingTimerResponse{}, nil
}

func (s *requestChatHistorySpy) ArchiveChat(context.Context, domainChat.ArchiveChatRequest) (domainChat.ArchiveChatResponse, error) {
	return domainChat.ArchiveChatResponse{}, nil
}

func (s *requestChatHistorySpy) RequestChatHistory(_ context.Context, request domainChat.RequestChatHistoryRequest) (domainChat.RequestChatHistoryResponse, error) {
	s.calls++
	s.lastRequest = request
	return domainChat.RequestChatHistoryResponse{Status: "requested", ChatJID: request.ChatJID}, nil
}

// TestRequestChatHistoryAllowsEmptyBody pins the fix for Fiber v3.4.0's
// Bind().Body() rejecting a bodyless request with no Content-Type: the
// optional body must not require a Content-Type to fall back to defaults.
func TestRequestChatHistoryAllowsEmptyBody(t *testing.T) {
	app := fiber.New()
	spy := &requestChatHistorySpy{}
	controller := &Chat{Service: spy}
	app.Post("/chat/:chat_jid/history", controller.RequestChatHistory)

	req := httptest.NewRequest("POST", "/chat/6289685028129%40s.whatsapp.net/history", nil)

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.Equal(t, 1, spy.calls)
	require.Equal(t, "6289685028129@s.whatsapp.net", spy.lastRequest.ChatJID)
}

// TestRequestChatHistoryIgnoresConflictingBodyChatJID pins the fix that the
// route chat_jid is authoritative: a body chat_jid must never override the
// storage lookup / history request target.
func TestRequestChatHistoryIgnoresConflictingBodyChatJID(t *testing.T) {
	app := fiber.New()
	spy := &requestChatHistorySpy{}
	controller := &Chat{Service: spy}
	app.Post("/chat/:chat_jid/history", controller.RequestChatHistory)

	body := `{"chat_jid":"other@s.whatsapp.net","count":50}`
	req := httptest.NewRequest("POST", "/chat/6289685028129%40s.whatsapp.net/history", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.Equal(t, 1, spy.calls)
	require.Equal(t, "6289685028129@s.whatsapp.net", spy.lastRequest.ChatJID)
	require.Equal(t, 50, spy.lastRequest.Count)
}
