package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
)

// addDeviceStubUsecase implements domainDevice.IDeviceUsecase by embedding the
// interface while recording the arguments actually received by AddDevice.
type addDeviceStubUsecase struct {
	domainDevice.IDeviceUsecase
	receivedDeviceID string
	receivedWebhook  *chatstorage.DeviceWebhookConfig
}

func (s *addDeviceStubUsecase) AddDevice(_ context.Context, deviceID string, webhook *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	s.receivedDeviceID = deviceID
	s.receivedWebhook = webhook
	return &domainDevice.Device{ID: deviceID}, nil
}

func (s *addDeviceStubUsecase) LoginDevice(_ context.Context, _ string) (domainApp.LoginResponse, error) {
	return domainApp.LoginResponse{ImagePath: "statics/qrcode/scan-qr-dev1.png"}, nil
}

func newAddDeviceTestApp(stub *addDeviceStubUsecase) *fiber.App {
	app := fiber.New()
	app.Use(middleware.Recovery())
	controller := Device{Service: stub}
	app.Post("/devices", controller.AddDevice)
	app.Get("/devices/:device_id/login", controller.LoginDevice)
	return app
}

// TestAddDevice_ForwardsFullWebhookConfig verifies that POST /devices accepts the
// complete webhook configuration (url, secret, events, insecure_skip_verify) that the
// device manager UI sends, instead of silently dropping everything but webhook_url.
func TestAddDevice_ForwardsFullWebhookConfig(t *testing.T) {
	stub := &addDeviceStubUsecase{}
	app := newAddDeviceTestApp(stub)

	body := `{
		"device_id": "dev1",
		"webhook_url": "https://hook.example.com",
		"webhook_secret": "s3cret",
		"webhook_events": "message,message.ack",
		"webhook_insecure_skip_verify": true
	}`
	req := httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if stub.receivedDeviceID != "dev1" {
		t.Fatalf("expected device_id dev1, got %q", stub.receivedDeviceID)
	}
	cfg := stub.receivedWebhook
	if cfg == nil {
		t.Fatal("expected webhook config to be forwarded to the usecase, got nil")
	}
	if cfg.WebhookURL == nil || *cfg.WebhookURL != "https://hook.example.com" {
		t.Fatalf("expected webhook_url to be forwarded, got %v", cfg.WebhookURL)
	}
	if cfg.WebhookSecret != "s3cret" {
		t.Fatalf("expected webhook_secret to be forwarded, got %q", cfg.WebhookSecret)
	}
	if cfg.WebhookEvents != "message,message.ack" {
		t.Fatalf("expected webhook_events to be forwarded, got %q", cfg.WebhookEvents)
	}
	if !cfg.WebhookInsecureSkipVerify {
		t.Fatal("expected webhook_insecure_skip_verify to be forwarded as true")
	}
}

// TestAddDevice_NoWebhookFields verifies that a plain device creation without any
// webhook fields passes a nil config to the usecase.
func TestAddDevice_NoWebhookFields(t *testing.T) {
	stub := &addDeviceStubUsecase{}
	app := newAddDeviceTestApp(stub)

	req := httptest.NewRequest(http.MethodPost, "/devices", strings.NewReader(`{"device_id":"dev2"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if stub.receivedWebhook != nil {
		t.Fatalf("expected nil webhook config when no webhook fields sent, got %+v", stub.receivedWebhook)
	}

	var parsed struct {
		Results map[string]any `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if parsed.Results["id"] != "dev2" {
		t.Fatalf("expected result id dev2, got %v", parsed.Results["id"])
	}
}

// TestLoginDevice_QRLinkKeepsRequestPort verifies the QR link points back at the
// host:port the client connected to, so it stays reachable when the app is
// served on a non-default port.
func TestLoginDevice_QRLinkKeepsRequestPort(t *testing.T) {
	app := newAddDeviceTestApp(&addDeviceStubUsecase{})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "http://172.168.0.101:3000/devices/dev1/login", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var parsed struct {
		Results map[string]any `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	want := "http://172.168.0.101:3000/statics/qrcode/scan-qr-dev1.png"
	if parsed.Results["qr_link"] != want {
		t.Fatalf("expected qr_link %q, got %v", want, parsed.Results["qr_link"])
	}
}
