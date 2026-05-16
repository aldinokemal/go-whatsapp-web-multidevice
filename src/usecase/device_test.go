package usecase

import (
	"context"
	"errors"
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
	if !errors.Is(err, context.Canceled) && err.Error() != "device manager not initialized" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetDeviceWebhook_InvalidManager(t *testing.T) {
	svc := &serviceDevice{manager: nil}
	_, err := svc.GetDeviceWebhook(context.Background(), "test-device")
	if err == nil {
		t.Fatal("expected error when manager is nil")
	}
	if !errors.Is(err, context.Canceled) && err.Error() != "device manager not initialized" {
		t.Fatalf("unexpected error: %v", err)
	}
}


