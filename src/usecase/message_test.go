package usecase

import (
	"context"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
)

type downloadMessageRepoStub struct {
	domainChatStorage.IChatStorageRepository
	unscopedMessage *domainChatStorage.Message
	scopedMessage   *domainChatStorage.Message
	unscopedCalls   int
	scopedCalls     int
	gotDeviceID     string
	gotMessageID    string
}

func (r *downloadMessageRepoStub) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	r.unscopedCalls++
	return r.unscopedMessage, nil
}

func (r *downloadMessageRepoStub) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.scopedCalls++
	r.gotDeviceID = deviceID
	r.gotMessageID = id
	return r.scopedMessage, nil
}

func TestLookupDownloadMediaMessageUsesActiveDeviceScope(t *testing.T) {
	const (
		activeDeviceID = "receiver-device"
		messageID      = "shared-message-id"
	)

	want := &domainChatStorage.Message{
		ID:       messageID,
		ChatJID:  "sender@s.whatsapp.net",
		DeviceID: activeDeviceID,
	}
	repo := &downloadMessageRepoStub{
		// The same WhatsApp message ID can be stored by both the sending and
		// receiving devices. The global lookup deliberately returns the wrong
		// device's row so the test fails if download lookup loses its scope.
		unscopedMessage: &domainChatStorage.Message{
			ID:       messageID,
			ChatJID:  "receiver@s.whatsapp.net",
			DeviceID: "sending-device",
		},
		scopedMessage: want,
	}
	service := serviceMessage{chatStorageRepo: repo}
	ctx := whatsapp.ContextWithDevice(
		context.Background(),
		whatsapp.NewDeviceInstance(activeDeviceID, nil, nil),
	)

	got, err := service.lookupDownloadMediaMessage(ctx, messageID)
	if err != nil {
		t.Fatalf("lookup download media message: %v", err)
	}
	if got != want {
		t.Fatalf("lookup returned %+v, want active-device row %+v", got, want)
	}
	if repo.unscopedCalls != 0 {
		t.Fatalf("global message lookup called %d times, want 0", repo.unscopedCalls)
	}
	if repo.scopedCalls != 1 {
		t.Fatalf("device-scoped lookup called %d times, want 1", repo.scopedCalls)
	}
	if repo.gotDeviceID != activeDeviceID {
		t.Fatalf("lookup device = %q, want %q", repo.gotDeviceID, activeDeviceID)
	}
	if repo.gotMessageID != messageID {
		t.Fatalf("lookup message = %q, want %q", repo.gotMessageID, messageID)
	}
}
