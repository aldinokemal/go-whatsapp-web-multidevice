package rest

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type callUsecaseStub struct{}

func (callUsecaseStub) RejectCall(context.Context, string, string) error { return nil }

func (callUsecaseStub) StartCall(context.Context, domainCall.StartCallRequest) (domainCall.StartCallResponse, error) {
	return domainCall.StartCallResponse{CallID: "CALL1", Target: "+628123456789"}, nil
}

func (callUsecaseStub) AnswerCall(context.Context, string, string) error { return nil }

func (callUsecaseStub) HangupCall(context.Context, string) error { return nil }

func (callUsecaseStub) AttachAudio(
	context.Context,
	string,
	<-chan []byte,
	func([]float32),
) error {
	return nil
}

func (callUsecaseStub) WatchCallEnd(context.Context, string, func()) error { return nil }

func TestBetaCallRoutesFollowTheFlag(t *testing.T) {
	previous := config.WhatsappBetaCallEnabled
	t.Cleanup(func() { config.WhatsappBetaCallEnabled = previous })

	tests := []struct {
		name       string
		enabled    bool
		path       string
		wantStatus int
	}{
		{name: "start hidden while disabled", enabled: false, path: "/call/start", wantStatus: fiber.StatusNotFound},
		{name: "answer hidden while disabled", enabled: false, path: "/call/answer", wantStatus: fiber.StatusNotFound},
		{name: "hangup hidden while disabled", enabled: false, path: "/call/hangup", wantStatus: fiber.StatusNotFound},
		{name: "start registered when enabled", enabled: true, path: "/call/start", wantStatus: fiber.StatusMethodNotAllowed},
		{name: "answer registered when enabled", enabled: true, path: "/call/answer", wantStatus: fiber.StatusMethodNotAllowed},
		{name: "hangup registered when enabled", enabled: true, path: "/call/hangup", wantStatus: fiber.StatusMethodNotAllowed},
		{name: "audio hidden while disabled", enabled: false, path: "/call/audio", wantStatus: fiber.StatusNotFound},
		{name: "audio demands a websocket upgrade when enabled", enabled: true, path: "/call/audio", wantStatus: fiber.StatusUpgradeRequired},
		{name: "reject always registered", enabled: false, path: "/call/reject", wantStatus: fiber.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Registration reads the flag at init time, so build a fresh app per case.
			config.WhatsappBetaCallEnabled = tt.enabled
			app := fiber.New()
			InitRestCall(app, callUsecaseStub{})

			resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, tt.path, nil))
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
