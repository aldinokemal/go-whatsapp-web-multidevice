package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func newAddDeviceTestApp(stub *addDeviceStubUsecase) *fiber.App {
	app := fiber.New()
	app.Use(middleware.Recovery())
	controller := Device{Service: stub}
	app.Post("/devices", controller.AddDevice)
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

type deviceWebhookStubUsecase struct {
	domainDevice.IDeviceUsecase
	receivedDeviceID string
	receivedConfig   *chatstorage.DeviceWebhookConfig
	getConfig        *chatstorage.DeviceWebhookConfig
}

func (s *deviceWebhookStubUsecase) SetDeviceWebhookConfig(_ context.Context, deviceID string, config *chatstorage.DeviceWebhookConfig) error {
	s.receivedDeviceID = deviceID
	s.receivedConfig = config
	return nil
}

func (s *deviceWebhookStubUsecase) GetDeviceWebhookConfig(_ context.Context, deviceID string) (*chatstorage.DeviceWebhookConfig, error) {
	s.receivedDeviceID = deviceID
	return s.getConfig, nil
}

func newDeviceWebhookTestApp(stub *deviceWebhookStubUsecase) *fiber.App {
	app := fiber.New()
	app.Use(middleware.Recovery())
	controller := Device{Service: stub}
	app.Patch("/devices/:device_id/webhook", controller.UpdateDeviceWebhook)
	app.Get("/devices/:device_id/webhook", controller.GetDeviceWebhook)
	return app
}

func TestUpdateDeviceWebhook_ForwardsIgnoreGroups(t *testing.T) {
	stub := &deviceWebhookStubUsecase{}
	app := newDeviceWebhookTestApp(stub)

	body := `{"webhook_url": "https://hook.example.com", "webhook_ignore_groups": true}`
	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if stub.receivedConfig == nil {
		t.Fatal("expected webhook config to be forwarded to the usecase, got nil")
	}
	if stub.receivedConfig.WebhookIgnoreGroups == nil || !*stub.receivedConfig.WebhookIgnoreGroups {
		t.Fatalf("expected webhook_ignore_groups=true to be forwarded, got %v", stub.receivedConfig.WebhookIgnoreGroups)
	}

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	results, ok := respBody["results"].(map[string]any)
	if !ok {
		t.Fatalf("expected results object in response, got %v", respBody)
	}
	if ignoreGroups, ok := results["webhook_ignore_groups"].(bool); !ok || !ignoreGroups {
		t.Fatalf("expected webhook_ignore_groups=true in response, got %v", results["webhook_ignore_groups"])
	}
}

func TestUpdateDeviceWebhook_PreservesIgnoreGroupsWhenOmitted(t *testing.T) {
	trueVal := true
	stub := &deviceWebhookStubUsecase{getConfig: &chatstorage.DeviceWebhookConfig{WebhookIgnoreGroups: &trueVal}}
	app := newDeviceWebhookTestApp(stub)

	body := `{"webhook_url": "https://hook.example.com"}`
	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if stub.receivedConfig == nil {
		t.Fatal("expected webhook config to be forwarded to the usecase, got nil")
	}
	if stub.receivedConfig.WebhookIgnoreGroups == nil || !*stub.receivedConfig.WebhookIgnoreGroups {
		t.Fatalf("expected previously-stored webhook_ignore_groups=true to be preserved, got %v", stub.receivedConfig.WebhookIgnoreGroups)
	}

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	results, ok := respBody["results"].(map[string]any)
	if !ok {
		t.Fatalf("expected results object in response, got %v", respBody)
	}
	if ignoreGroups, ok := results["webhook_ignore_groups"].(bool); !ok || !ignoreGroups {
		t.Fatalf("expected webhook_ignore_groups=true in response, got %v", results["webhook_ignore_groups"])
	}
}

func TestUpdateDeviceWebhook_ExplicitFalseOverridesExisting(t *testing.T) {
	trueVal := true
	stub := &deviceWebhookStubUsecase{getConfig: &chatstorage.DeviceWebhookConfig{WebhookIgnoreGroups: &trueVal}}
	app := newDeviceWebhookTestApp(stub)

	body := `{"webhook_url": "https://hook.example.com", "webhook_ignore_groups": false}`
	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if stub.receivedConfig == nil {
		t.Fatal("expected webhook config to be forwarded to the usecase, got nil")
	}
	if stub.receivedConfig.WebhookIgnoreGroups == nil || *stub.receivedConfig.WebhookIgnoreGroups {
		t.Fatalf("expected explicit webhook_ignore_groups=false to override existing, got %v", stub.receivedConfig.WebhookIgnoreGroups)
	}
}

func TestGetDeviceWebhook_ReturnsIgnoreGroups(t *testing.T) {
	trueVal := true
	stub := &deviceWebhookStubUsecase{getConfig: &chatstorage.DeviceWebhookConfig{WebhookIgnoreGroups: &trueVal}}
	app := newDeviceWebhookTestApp(stub)

	req := httptest.NewRequest(http.MethodGet, "/devices/dev1/webhook", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	results, ok := respBody["results"].(map[string]any)
	if !ok {
		t.Fatalf("expected results object in response, got %v", respBody)
	}
	if ignoreGroups, ok := results["webhook_ignore_groups"].(bool); !ok || !ignoreGroups {
		t.Fatalf("expected webhook_ignore_groups=true in GET response, got %v", results["webhook_ignore_groups"])
	}
}

func TestGetDeviceWebhook_NilIgnoreGroupsReturnsNull(t *testing.T) {
	stub := &deviceWebhookStubUsecase{getConfig: &chatstorage.DeviceWebhookConfig{}}
	app := newDeviceWebhookTestApp(stub)

	req := httptest.NewRequest(http.MethodGet, "/devices/dev1/webhook", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	results := respBody["results"].(map[string]any)
	if results["webhook_ignore_groups"] != nil {
		t.Fatalf("expected webhook_ignore_groups=null when never configured, got %v", results["webhook_ignore_groups"])
	}
}
