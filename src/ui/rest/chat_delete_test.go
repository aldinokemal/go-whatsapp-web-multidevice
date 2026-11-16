package rest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type deleteChatHandlerSpy struct {
	domainChat.IChatUsecase
	request domainChat.DeleteChatRequest
	calls   int
	err     error
}

func (s *deleteChatHandlerSpy) DeleteChat(_ context.Context, r domainChat.DeleteChatRequest) (domainChat.DeleteChatResponse, error) {
	s.request = r
	s.calls++
	return domainChat.DeleteChatResponse{Status: "success", Message: "Deleted", ChatJID: r.ChatJID, LocalOnly: r.LocalOnly}, s.err
}
func TestDeleteChatHandler(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		err        error
		status     int
		code       string
	}{
		{name: "empty optional body", status: 200, code: "SUCCESS"},
		{name: "explicit options and authoritative path", body: `{"chat_jid":"wrong@g.us","delete_media":false,"local_only":true}`, status: 200, code: "SUCCESS"},
		{name: "bad body", body: `{"delete_media":"no"}`, status: 400, code: "BAD_REQUEST"},
		{name: "remote success local failure", body: `{}`, err: pkgError.LocalDeleteFailed("chat was deleted on WhatsApp but the local copy was not fully removed"), status: 500, code: "LOCAL_DELETE_FAILED"},
		{name: "local only failure", body: `{"local_only":true}`, err: pkgError.InternalServerError("local deletion failed"), status: 500, code: "INTERNAL_SERVER_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(middleware.Recovery())
			spy := &deleteChatHandlerSpy{err: tc.err}
			controller := &Chat{Service: spy}
			app.Post("/chat/:chat_jid/delete", controller.DeleteChat)
			req := httptest.NewRequest("POST", "/chat/628111%40s.whatsapp.net/delete", strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, tc.status, resp.StatusCode)
			var envelope utils.ResponseData
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&envelope))
			require.Equal(t, tc.code, envelope.Code)
			if tc.status != 400 {
				require.Equal(t, "628111@s.whatsapp.net", spy.request.ChatJID)
			} else {
				require.Zero(t, spy.calls)
			}
			if tc.name == "explicit options and authoritative path" {
				require.NotNil(t, spy.request.DeleteMedia)
				require.False(t, *spy.request.DeleteMedia)
				require.True(t, spy.request.LocalOnly)
			}
		})
	}
}
