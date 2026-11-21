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

// storageSettingsStubUsecase implements domainDevice.IDeviceUsecase, recording the
// patch received by SetDeviceStorageSettings and serving a fixed GetDeviceStorageSettings
// result so PATCH's read-after-write response can be asserted too.
type storageSettingsStubUsecase struct {
	domainDevice.IDeviceUsecase
	receivedPatch chatstorage.DeviceStoragePatch
	setDeviceID   string
	getDeviceID   string
	patched       bool
	settings      *chatstorage.DeviceStorageSettings
	getErr        error
}

func (s *storageSettingsStubUsecase) SetDeviceStorageSettings(_ context.Context, deviceID string, patch chatstorage.DeviceStoragePatch) error {
	s.receivedPatch = patch
	s.setDeviceID = deviceID
	s.patched = true
	return nil
}

func (s *storageSettingsStubUsecase) GetDeviceStorageSettings(_ context.Context, deviceID string) (*chatstorage.DeviceStorageSettings, error) {
	s.getDeviceID = deviceID
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.settings == nil {
		return &chatstorage.DeviceStorageSettings{}, nil
	}
	return s.settings, nil
}

func newDeviceStorageSettingsTestApp(stub *storageSettingsStubUsecase) *fiber.App {
	app := fiber.New()
	app.Use(middleware.Recovery())
	controller := Device{Service: stub}
	app.Patch("/devices/:device_id/settings", controller.UpdateDeviceStorageSettings)
	app.Get("/devices/:device_id/settings", controller.GetDeviceStorageSettings)
	return app
}

func TestUpdateDeviceStorageSettings_SetsBothFields(t *testing.T) {
	stub := &storageSettingsStubUsecase{}
	app := newDeviceStorageSettingsTestApp(stub)

	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/settings", strings.NewReader(`{"chat_storage": false, "auto_download_media": true}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if !stub.receivedPatch.HasChatStorage || stub.receivedPatch.ChatStorage == nil || *stub.receivedPatch.ChatStorage != false {
		t.Fatalf("expected chat_storage=false to be forwarded, got %+v", stub.receivedPatch)
	}
	if !stub.receivedPatch.HasAutoDownloadMedia || stub.receivedPatch.AutoDownloadMedia == nil || *stub.receivedPatch.AutoDownloadMedia != true {
		t.Fatalf("expected auto_download_media=true to be forwarded, got %+v", stub.receivedPatch)
	}
}

// A field explicitly sent as null must be forwarded as "present but nil" (clear the
// override), not silently dropped as if absent.
func TestUpdateDeviceStorageSettings_ExplicitNullClearsOverride(t *testing.T) {
	stub := &storageSettingsStubUsecase{}
	app := newDeviceStorageSettingsTestApp(stub)

	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/settings", strings.NewReader(`{"chat_storage": null}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if !stub.receivedPatch.HasChatStorage {
		t.Fatal("expected chat_storage to be marked present even though its value is null")
	}
	if stub.receivedPatch.ChatStorage != nil {
		t.Fatalf("expected chat_storage value to be nil (clear), got %v", *stub.receivedPatch.ChatStorage)
	}
	if stub.receivedPatch.HasAutoDownloadMedia {
		t.Fatal("expected auto_download_media to be left untouched (absent from the request)")
	}
}

// An empty body (neither field present) is rejected: there is nothing to update.
func TestUpdateDeviceStorageSettings_EmptyBodyRejected(t *testing.T) {
	stub := &storageSettingsStubUsecase{}
	app := newDeviceStorageSettingsTestApp(stub)

	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/settings", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

// A non-boolean, non-null value for either field is rejected with 400 rather than
// silently coerced.
func TestUpdateDeviceStorageSettings_InvalidTypeRejected(t *testing.T) {
	stub := &storageSettingsStubUsecase{}
	app := newDeviceStorageSettingsTestApp(stub)

	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/settings", strings.NewReader(`{"chat_storage": "yes"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestGetDeviceStorageSettings_NoOverrideReturnsNulls(t *testing.T) {
	stub := &storageSettingsStubUsecase{settings: &chatstorage.DeviceStorageSettings{}}
	app := newDeviceStorageSettingsTestApp(stub)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/devices/dev1/settings", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var parsed struct {
		Results map[string]any `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if parsed.Results["chat_storage"] != nil {
		t.Fatalf("expected chat_storage null, got %v", parsed.Results["chat_storage"])
	}
	if parsed.Results["auto_download_media"] != nil {
		t.Fatalf("expected auto_download_media null, got %v", parsed.Results["auto_download_media"])
	}
	if parsed.Results["device_id"] != "dev1" {
		t.Fatalf("expected device_id dev1, got %v", parsed.Results["device_id"])
	}
}

func TestGetDeviceStorageSettings_WithOverrideReturnsValues(t *testing.T) {
	chatStorage := false
	autoDownload := true
	stub := &storageSettingsStubUsecase{settings: &chatstorage.DeviceStorageSettings{
		ChatStorage:       &chatStorage,
		AutoDownloadMedia: &autoDownload,
	}}
	app := newDeviceStorageSettingsTestApp(stub)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/devices/dev1/settings", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var parsed struct {
		Results map[string]any `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if parsed.Results["chat_storage"] != false {
		t.Fatalf("expected chat_storage=false, got %v", parsed.Results["chat_storage"])
	}
	if parsed.Results["auto_download_media"] != true {
		t.Fatalf("expected auto_download_media=true, got %v", parsed.Results["auto_download_media"])
	}
}

// A registered id with a space reaches the usecase decoded, on both GET and PATCH.
func TestDeviceStorageSettings_DecodesPercentEncodedDeviceID(t *testing.T) {
	stub := &storageSettingsStubUsecase{}
	app := newDeviceStorageSettingsTestApp(stub)

	req := httptest.NewRequest(http.MethodPatch, "/devices/sales%20team/settings", strings.NewReader(`{"chat_storage": false}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if stub.setDeviceID != "sales team" || stub.getDeviceID != "sales team" {
		t.Fatalf("expected decoded id on PATCH, got set=%q get=%q", stub.setDeviceID, stub.getDeviceID)
	}

	stub.getDeviceID = ""
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/devices/sales%20team/settings", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if stub.getDeviceID != "sales team" {
		t.Fatalf("expected decoded id on GET, got %q", stub.getDeviceID)
	}
}

// Fields outside the documented schema are rejected instead of silently ignored.
func TestUpdateDeviceStorageSettings_UnknownFieldRejected(t *testing.T) {
	stub := &storageSettingsStubUsecase{}
	app := newDeviceStorageSettingsTestApp(stub)

	req := httptest.NewRequest(http.MethodPatch, "/devices/dev1/settings", strings.NewReader(`{"chat_storage": false, "chat_storag": true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
	if stub.patched {
		t.Fatal("expected no update when the body has an unknown field")
	}
}
