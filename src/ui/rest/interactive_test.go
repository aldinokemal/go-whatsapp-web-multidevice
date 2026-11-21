package rest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type interactiveSendStub struct {
	domainSend.ISendUsecase
	request domainSend.InteractiveRequest
}

func (s *interactiveSendStub) SendInteractive(_ context.Context, req domainSend.InteractiveRequest) (domainSend.GenericResponse, error) {
	s.request = req
	return domainSend.GenericResponse{MessageID: "test-id", Status: "accepted"}, nil
}

func TestInteractiveRoute(t *testing.T) {
	app := fiber.New()
	s := &interactiveSendStub{}
	InitRestSend(app, s)
	req := httptest.NewRequest("POST", "/send/interactive", strings.NewReader(`{"phone":"15551234567","body":"Choose","buttons":[{"name":"quick_reply","params":{"display_text":"Yes","id":"yes"}}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "Choose", s.request.Body)
	require.Equal(t, "quick_reply", s.request.Buttons[0].Name)
}
