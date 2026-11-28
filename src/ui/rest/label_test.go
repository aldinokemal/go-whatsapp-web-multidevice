package rest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type labelServiceSpy struct {
	edit  *domainLabel.EditLabelRequest
	chat  *domainLabel.LabelChatRequest
	calls int
}

func (s *labelServiceSpy) ListLabels(context.Context) (domainLabel.ListLabelsResponse, error) {
	s.calls++
	return domainLabel.ListLabelsResponse{}, nil
}

func (s *labelServiceSpy) ResyncLabels(context.Context) (domainLabel.ListLabelsResponse, error) {
	s.calls++
	return domainLabel.ListLabelsResponse{}, nil
}

func (s *labelServiceSpy) EditLabel(_ context.Context, request domainLabel.EditLabelRequest) (domainLabel.EditLabelResponse, error) {
	s.calls++
	s.edit = &request
	return domainLabel.EditLabelResponse{Status: "success", LabelID: request.LabelID}, nil
}

func (s *labelServiceSpy) LabelChat(_ context.Context, request domainLabel.LabelChatRequest) (domainLabel.LabelChatResponse, error) {
	s.calls++
	s.chat = &request
	return domainLabel.LabelChatResponse{Status: "success", ChatJID: request.ChatJID}, nil
}

func postJSON(t *testing.T, app *fiber.App, path, body string) int {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	resp.Body.Close()
	return resp.StatusCode
}

func TestLabelRoutes(t *testing.T) {
	spy := &labelServiceSpy{}
	app := fiber.New()
	InitRestLabel(app, spy)

	require.Equal(t, fiber.StatusOK, postJSON(t, app, "/label", `{"label_id":"30","name":"Family","type":"CUSTOM","color":0}`))
	require.NotNil(t, spy.edit)
	require.Equal(t, "30", spy.edit.LabelID)
	require.Equal(t, "CUSTOM", spy.edit.Type)
	require.NotNil(t, spy.edit.Color, "an explicit 0 color is kept")
	require.Nil(t, spy.edit.OrderIndex)

	require.Equal(t, fiber.StatusOK, postJSON(t, app, "/chat/120363025982934543%40g.us/label", `{"chat_jid":"other@s.whatsapp.net","label_id":"30","labeled":false}`))
	require.NotNil(t, spy.chat)
	require.Equal(t, "120363025982934543@g.us", spy.chat.ChatJID, "route JID wins over the body")
	require.NotNil(t, spy.chat.Labeled)
	require.False(t, *spy.chat.Labeled)

	require.Equal(t, fiber.StatusOK, postJSON(t, app, "/chat/6289685028129%40s.whatsapp.net/label", `{"label_id":"30"}`))
	require.Nil(t, spy.chat.Labeled, "a missing flag must not turn into a removal")

	resp, err := app.Test(httptest.NewRequest("GET", "/labels", nil))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.Equal(t, fiber.StatusOK, postJSON(t, app, "/labels/resync", `{}`))
	require.Equal(t, 5, spy.calls)
}
