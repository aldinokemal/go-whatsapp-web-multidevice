package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
)

func TestLockLogin_SerializesPerDevice(t *testing.T) {
	svc := &serviceApp{}
	unlock, err := svc.lockLogin(context.Background(), "dev1")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	// Another device is independent.
	unlockOther, err := svc.lockLogin(context.Background(), "dev2")
	if err != nil {
		t.Fatalf("other device lock: %v", err)
	}
	unlockOther()

	// A second login on the same device waits and gives up with its context.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.lockLogin(ctx, "dev1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded while locked, got %v", err)
	}

	unlock()

	// A cancelled request is rejected even when the lock is free, and leaves it free.
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	for range 100 {
		if _, err := svc.lockLogin(cancelled, "dev1"); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled for a cancelled request, got %v", err)
		}
	}

	unlockAgain, err := svc.lockLogin(context.Background(), "dev1")
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlockAgain()
}

// TestMalformedPairLabelOnlyBlocksCodePairing pins that a malformed
// APP_PAIR_DISPLAY_NAME is rejected by code pairing before the client is
// touched or connected, while QR login and reconnect go through their own
// checks without consulting the label.
func TestMalformedPairLabelOnlyBlocksCodePairing(t *testing.T) {
	original := config.AppPairDisplayName
	t.Cleanup(func() { config.AppPairDisplayName = original })
	config.AppPairDisplayName = "GOWA"

	// An unpaired slot with a client that has never connected. The manager
	// has no store container, so nothing here can reach WhatsApp.
	client := whatsmeow.NewClient(&store.Device{}, nil)
	instance := whatsapp.NewDeviceInstance("dev", client, nil)
	manager := whatsapp.NewDeviceManager(nil, nil, nil)
	manager.AddDevice(instance)
	service := &serviceApp{deviceManager: manager}

	_, err := service.LoginWithCode(context.Background(), "dev", "6281234567890")
	var validationErr pkgError.ValidationError
	if !errors.As(err, &validationErr) || !strings.Contains(err.Error(), "APP_PAIR_DISPLAY_NAME") {
		t.Fatalf("code pairing: expected the pair label validation error, got %v", err)
	}
	if instance.GetClient() != client || client.IsConnected() {
		t.Fatal("code pairing touched the client before rejecting the label")
	}

	err = service.Reconnect(context.Background(), "dev")
	if err == nil || strings.Contains(err.Error(), "APP_PAIR_DISPLAY_NAME") || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("reconnect: expected its own session check, got %v", err)
	}

	_, err = service.Login(context.Background(), "dev")
	if err == nil || strings.Contains(err.Error(), "APP_PAIR_DISPLAY_NAME") || !strings.Contains(err.Error(), "store container is nil") {
		t.Fatalf("QR login: expected it to reach client setup, got %v", err)
	}
}
