package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// useAppTimezone sets config.AppLocation for one test and restores it afterwards.
func useAppTimezone(t *testing.T, name string) {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	previous := config.AppLocation
	config.AppLocation = location
	t.Cleanup(func() { config.AppLocation = previous })
}

func newTimezoneChatFixture(t *testing.T, at time.Time) (context.Context, *chatUsecaseRepoStub, string) {
	t.Helper()
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	deviceID := accountJID.String()
	chatJID := "628123456789@s.whatsapp.net"
	repo := &chatUsecaseRepoStub{
		chat: &domainChatStorage.Chat{DeviceID: deviceID, JID: chatJID, Name: "Alice", LastMessageTime: at, CreatedAt: at, UpdatedAt: at},
		messages: []*domainChatStorage.Message{{
			ID: "msg-1", ChatJID: chatJID, DeviceID: deviceID, Sender: chatJID, Content: "hello",
			Timestamp: at, CreatedAt: at, UpdatedAt: at,
		}},
	}
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID}}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))
	return ctx, repo, chatJID
}

func TestGetChatMessagesFormatsTimestampsInAppTimezone(t *testing.T) {
	useAppTimezone(t, "Europe/Paris")
	ctx, repo, chatJID := newTimezoneChatFixture(t, time.Date(2026, time.November, 18, 9, 0, 0, 0, time.UTC))

	response, err := NewChatService(repo).GetChatMessages(ctx, domainChat.GetChatMessagesRequest{ChatJID: chatJID, Limit: 50})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}

	const want = "2026-11-18T10:00:00+01:00"
	if got := response.Data[0].Timestamp; got != want {
		t.Errorf("message timestamp = %q, want %q", got, want)
	}
	if got := response.ChatInfo.LastMessageTime; got != want {
		t.Errorf("chat last_message_time = %q, want %q", got, want)
	}
}

func TestGetChatMessagesBindsTimeFiltersInUTC(t *testing.T) {
	ctx, repo, chatJID := newTimezoneChatFixture(t, time.Date(2026, time.November, 18, 9, 0, 0, 0, time.UTC))
	start, end := "2026-11-18T10:00:00+01:00", "2026-11-18T12:30:00+01:00"

	_, err := NewChatService(repo).GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
		ChatJID: chatJID, Limit: 50, StartTime: &start, EndTime: &end,
	})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}

	// Stored timestamps are UTC text, so a bound with another offset would compare wrongly.
	if got := repo.messageFilter.StartTime; got.Location() != time.UTC || !got.Equal(time.Date(2026, time.November, 18, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("start bound = %v, want 2026-11-18 09:00 UTC", got)
	}
	if got := repo.messageFilter.EndTime; got.Location() != time.UTC || !got.Equal(time.Date(2026, time.November, 18, 11, 30, 0, 0, time.UTC)) {
		t.Errorf("end bound = %v, want 2026-11-18 11:30 UTC", got)
	}
}

func TestFormatTimestampLeavesZeroTimeUnchanged(t *testing.T) {
	useAppTimezone(t, "Europe/Paris")

	if got, want := formatTimestamp(time.Time{}), "0001-01-01T00:00:00Z"; got != want {
		t.Errorf("formatTimestamp(zero) = %q, want %q", got, want)
	}
}
