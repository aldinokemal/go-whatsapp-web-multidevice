package rest

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

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
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var envelope utils.ResponseData
	require.NoError(t, json.Unmarshal(body, &envelope), "error must be the JSON envelope, got: %s", body)
	require.Equal(t, "BAD_REQUEST", envelope.Code)
	require.Contains(t, envelope.Message, "invalid chat_jid path parameter")
}
