package usecase

import (
	"context"
	"testing"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
)

func TestDeviceServiceInterface(t *testing.T) {
	var _ domainDevice.IDeviceUsecase = (*serviceDevice)(nil)
}

func TestSetDeviceWebhook_InvalidManager(t *testing.T) {
	svc := &serviceDevice{manager: nil}
	err := svc.SetDeviceWebhook(context.Background(), "test-device", "https://example.com/webhook")
	if err == nil {
		t.Fatal("expected error when manager is nil")
	}
}

func TestGetDeviceWebhook_InvalidManager(t *testing.T) {
	svc := &serviceDevice{manager: nil}
	_, err := svc.GetDeviceWebhook(context.Background(), "test-device")
	if err == nil {
		t.Fatal("expected error when manager is nil")
	}
}

func TestGetDeviceWebhook_DeviceNotFound(t *testing.T) {
	svc := &serviceDevice{manager: nil}
	_, err := svc.GetDeviceWebhook(context.Background(), "nonexistent-device")
	if err == nil {
		t.Fatal("expected error when device not found")
	}
}

func TestSetDeviceWebhook_DeviceNotFound(t *testing.T) {
	svc := &serviceDevice{manager: nil}
	err := svc.SetDeviceWebhook(context.Background(), "nonexistent-device", "https://example.com/webhook")
	if err == nil {
		t.Fatal("expected error when device not found")
	}
}
