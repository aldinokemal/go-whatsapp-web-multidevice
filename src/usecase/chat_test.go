package usecase

import (
	"context"
	"testing"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow/types/events"
)

func TestGetChatMessagesMapsReactions(t *testing.T) {
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC)
	repo := &chatUsecaseRepoStub{
		chat: &domainChatStorage.Chat{
			DeviceID:        deviceID,
			JID:             chatJID,
			Name:            "Alice",
			LastMessageTime: now,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		messages: []*domainChatStorage.Message{
			{
				ID:        "msg-1",
				ChatJID:   chatJID,
				DeviceID:  deviceID,
				Sender:    "628123456789@s.whatsapp.net",
				Content:   "hello",
				Timestamp: now,
				CreatedAt: now,
				UpdatedAt: now,
				Reactions: []domainChatStorage.Reaction{
					{
						MessageID:  "msg-1",
						ChatJID:    chatJID,
						DeviceID:   deviceID,
						ReactorJID: "628111111111@s.whatsapp.net",
						Emoji:      "\U0001f44d",
						IsFromMe:   false,
						Timestamp:  now.Add(time.Minute),
					},
				},
			},
		},
	}
	service := NewChatService(repo)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	response, err := service.GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
		ChatJID: chatJID,
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("expected one message, got %d", len(response.Data))
	}
	reactions := response.Data[0].Reactions
	if len(reactions) != 1 {
		t.Fatalf("expected one reaction, got %d", len(reactions))
	}
	if reactions[0].Emoji != "\U0001f44d" {
		t.Fatalf("expected reaction emoji to be mapped, got %q", reactions[0].Emoji)
	}
	if reactions[0].SenderJID != "628111111111@s.whatsapp.net" {
		t.Fatalf("expected reactor JID to be mapped, got %q", reactions[0].SenderJID)
	}
	if reactions[0].Timestamp != now.Add(time.Minute).Format(time.RFC3339) {
		t.Fatalf("expected reaction timestamp to be mapped, got %q", reactions[0].Timestamp)
	}
}

type chatUsecaseRepoStub struct {
	domainChatStorage.IChatStorageRepository
	chat     *domainChatStorage.Chat
	messages []*domainChatStorage.Message
}

func (r *chatUsecaseRepoStub) GetChatByDevice(_, _ string) (*domainChatStorage.Chat, error) {
	return r.chat, nil
}

func (r *chatUsecaseRepoStub) GetMessages(*domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return r.messages, nil
}

func (r *chatUsecaseRepoStub) GetChatMessageCount(string) (int64, error) {
	return int64(len(r.messages)), nil
}

func (r *chatUsecaseRepoStub) CreateReaction(context.Context, *events.Message) error {
	return nil
}

// TestChatDisplayName pins the chat-list name fallback (issue #675): a stored
// name is returned verbatim, but an empty name must never leak to the API as a
// blank string — it falls back to a JID-derived label so the sender stays
// identifiable.
func TestChatDisplayName(t *testing.T) {
	cases := []struct {
		name string
		jid  string
		in   string
		want string
	}{
		{"keeps non-empty 1:1 name", "628123456789@s.whatsapp.net", "Alice", "Alice"},
		{"empty 1:1 falls back to phone", "628123456789@s.whatsapp.net", "", "628123456789"},
		{"empty group falls back to Group id", "120363999000111@g.us", "", "Group 120363999000111"},
		{"keeps non-empty group name", "120363999000111@g.us", "Family", "Family"},
		{"empty newsletter falls back to Newsletter id", "120363111@newsletter", "", "Newsletter 120363111"},
		{"empty lid falls back to lid local part", "1234567890abcd@lid", "", "1234567890abcd"},
		{"empty status broadcast titled Status not local part", "status@broadcast", "", "Status"},
		{"keeps non-empty status broadcast name", "status@broadcast", "Status", "Status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatDisplayName(tc.jid, tc.in); got != tc.want {
				t.Fatalf("chatDisplayName(%q, %q) = %q, want %q", tc.jid, tc.in, got, tc.want)
			}
		})
	}
}
