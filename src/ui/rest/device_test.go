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
	// Omitted must NOT resolve to the stored value here: preservation is now applied
	// atomically by the repository update (WebhookIgnoreGroupsSet=false), not read back
	// and forwarded as an explicit value, which could race a concurrent explicit update.
	if stub.receivedConfig.WebhookIgnoreGroupsSet {
		t.Fatalf("expected WebhookIgnoreGroupsSet=false for an omitted field, got true (value %v)", stub.receivedConfig.WebhookIgnoreGroups)
	}
	if stub.receivedConfig.WebhookIgnoreGroups != nil {
		t.Fatalf("expected WebhookIgnoreGroups=nil for an omitted field, got %v", *stub.receivedConfig.WebhookIgnoreGroups)
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

// TestUpdateDeviceWebhook_IgnoreGroupsTriState pins the whole absent/null/value contract
// of webhook_ignore_groups in one place: a PATCH body must be able to walk a device
// through true -> null -> false without the omitted case ever clobbering the stored value.
func TestUpdateDeviceWebhook_IgnoreGroupsTriState(t *testing.T) {
	stored := true

	cases := []struct {
		name string
		body string
		// want is the response's expected effective value in every case, and also the
		// value the usecase should receive when the field was explicitly set (wantSet).
		// For the omitted case the usecase must receive it unset -- preservation is now
		// the repository's job -- while the response still reports the stored value,
		// fetched after the write.
		want    *bool
		wantSet bool
	}{
		{
			name:    "omitted key keeps the stored override",
			body:    `{"webhook_url": "https://hook.example.com"}`,
			want:    &stored,
			wantSet: false,
		},
		{
			name:    "explicit null clears the override back to NULL",
			body:    `{"webhook_url": "https://hook.example.com", "webhook_ignore_groups": null}`,
			want:    nil,
			wantSet: true,
		},
		{
			name:    "explicit false sets the override",
			body:    `{"webhook_url": "https://hook.example.com", "webhook_ignore_groups": false}`,
			want:    new(bool),
			wantSet: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &deviceWebhookStubUsecase{
				getConfig: &chatstorage.DeviceWebhookConfig{WebhookIgnoreGroups: &stored},
			}
			app := newDeviceWebhookTestApp(stub)

			req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/webhook", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200, got %d", resp.StatusCode)
			}

			if stub.receivedConfig == nil {
				t.Fatal("expected webhook config to be forwarded to the usecase, got nil")
			}
			if stub.receivedConfig.WebhookIgnoreGroupsSet != tc.wantSet {
				t.Fatalf("expected WebhookIgnoreGroupsSet=%v, got %v", tc.wantSet, stub.receivedConfig.WebhookIgnoreGroupsSet)
			}
			got := stub.receivedConfig.WebhookIgnoreGroups
			switch {
			case !tc.wantSet && got != nil:
				t.Fatalf("expected the usecase to receive nil for an omitted field, got %v", *got)
			case tc.wantSet && tc.want == nil && got != nil:
				t.Fatalf("expected the usecase to receive nil, got %v", *got)
			case tc.wantSet && tc.want != nil && got == nil:
				t.Fatalf("expected the usecase to receive %v, got nil", *tc.want)
			case tc.wantSet && tc.want != nil && *got != *tc.want:
				t.Fatalf("expected the usecase to receive %v, got %v", *tc.want, *got)
			}

			var respBody map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			results, ok := respBody["results"].(map[string]any)
			if !ok {
				t.Fatalf("expected results object in response, got %v", respBody)
			}
			if tc.want == nil {
				if results["webhook_ignore_groups"] != nil {
					t.Fatalf("expected webhook_ignore_groups=null in response, got %v", results["webhook_ignore_groups"])
				}
				return
			}
			if echoed, ok := results["webhook_ignore_groups"].(bool); !ok || echoed != *tc.want {
				t.Fatalf("expected webhook_ignore_groups=%v in response, got %v", *tc.want, results["webhook_ignore_groups"])
			}
		})
	}
}
