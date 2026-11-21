package usecase

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestGetChatMessagesMapsReactions(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	deviceID := accountJID.String()
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
			{
				ID:        "msg-2",
				ChatJID:   chatJID,
				DeviceID:  deviceID,
				Sender:    accountJID.String(),
				Content:   "reply",
				IsFromMe:  true,
				Timestamp: now.Add(2 * time.Minute),
				CreatedAt: now.Add(2 * time.Minute),
				UpdatedAt: now.Add(2 * time.Minute),
				Reactions: []domainChatStorage.Reaction{
					{
						MessageID:  "msg-2",
						ChatJID:    chatJID,
						DeviceID:   deviceID,
						ReactorJID: "628111111111@s.whatsapp.net",
						Emoji:      "\U0001f44d",
						IsFromMe:   false,
						Timestamp:  now.Add(3 * time.Minute),
					},
				},
			},
			{
				ID:        "msg-3",
				ChatJID:   chatJID,
				DeviceID:  deviceID,
				Sender:    "628123456789@s.whatsapp.net",
				Content:   "again",
				Timestamp: now.Add(4 * time.Minute),
				CreatedAt: now.Add(4 * time.Minute),
				UpdatedAt: now.Add(4 * time.Minute),
				Reactions: []domainChatStorage.Reaction{
					{
						MessageID:  "msg-3",
						ChatJID:    chatJID,
						DeviceID:   deviceID,
						ReactorJID: accountJID.String(),
						Emoji:      "\U0001f389",
						IsFromMe:   true,
						Timestamp:  now.Add(5 * time.Minute),
					},
				},
			},
		},
	}
	service := NewChatService(repo)
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID, PushName: "Primary Account"}}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))

	response, err := service.GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
		ChatJID: chatJID,
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}
	if len(response.Data) != 3 {
		t.Fatalf("expected three messages, got %d", len(response.Data))
	}
	firstMessage := response.Data[0]
	if firstMessage.SenderJID != "628123456789@s.whatsapp.net" {
		t.Fatalf("expected inbound sender JID to be mapped, got %q", firstMessage.SenderJID)
	}
	if firstMessage.SenderDisplayName != "628123456789" {
		t.Fatalf("expected inbound sender display name, got %q", firstMessage.SenderDisplayName)
	}
	if firstMessage.IsFromMe {
		t.Fatal("expected inbound message is_from_me to be mapped")
	}
	if firstMessage.Timestamp != now.Format(time.RFC3339) {
		t.Fatalf("expected inbound timestamp to be mapped, got %q", firstMessage.Timestamp)
	}

	reactions := firstMessage.Reactions
	if len(reactions) != 1 {
		t.Fatalf("expected one reaction, got %d", len(reactions))
	}
	if reactions[0].Emoji != "\U0001f44d" {
		t.Fatalf("expected reaction emoji to be mapped, got %q", reactions[0].Emoji)
	}
	if reactions[0].SenderJID != "628111111111@s.whatsapp.net" {
		t.Fatalf("expected reactor JID to be mapped, got %q", reactions[0].SenderJID)
	}
	if reactions[0].SenderDisplayName != "628111111111" {
		t.Fatalf("expected reactor display name, got %q", reactions[0].SenderDisplayName)
	}
	if reactions[0].IsFromMe {
		t.Fatal("expected inbound reaction is_from_me to be mapped")
	}
	if reactions[0].Timestamp != now.Add(time.Minute).Format(time.RFC3339) {
		t.Fatalf("expected reaction timestamp to be mapped, got %q", reactions[0].Timestamp)
	}

	secondMessage := response.Data[1]
	if secondMessage.SenderJID != accountJID.String() {
		t.Fatalf("expected outbound sender JID to be mapped, got %q", secondMessage.SenderJID)
	}
	if secondMessage.SenderDisplayName != "Primary Account" {
		t.Fatalf("expected outbound sender display name, got %q", secondMessage.SenderDisplayName)
	}
	if !secondMessage.IsFromMe {
		t.Fatal("expected outbound message is_from_me to be mapped")
	}
	if secondMessage.Timestamp != now.Add(2*time.Minute).Format(time.RFC3339) {
		t.Fatalf("expected outbound timestamp to be mapped, got %q", secondMessage.Timestamp)
	}
	if len(secondMessage.Reactions) != 1 {
		t.Fatalf("expected one repeated reaction, got %d", len(secondMessage.Reactions))
	}
	if secondMessage.Reactions[0].SenderJID != "628111111111@s.whatsapp.net" {
		t.Fatalf("expected repeated reactor JID to be mapped, got %q", secondMessage.Reactions[0].SenderJID)
	}
	if secondMessage.Reactions[0].SenderDisplayName != "628111111111" {
		t.Fatalf("expected repeated reactor display name, got %q", secondMessage.Reactions[0].SenderDisplayName)
	}
	if secondMessage.Reactions[0].Emoji != "\U0001f44d" || secondMessage.Reactions[0].IsFromMe || secondMessage.Reactions[0].Timestamp != now.Add(3*time.Minute).Format(time.RFC3339) {
		t.Fatalf("expected repeated reaction fields to be mapped, got %#v", secondMessage.Reactions[0])
	}

	thirdMessage := response.Data[2]
	if thirdMessage.SenderJID != "628123456789@s.whatsapp.net" || thirdMessage.SenderDisplayName != "628123456789" || thirdMessage.IsFromMe || thirdMessage.Timestamp != now.Add(4*time.Minute).Format(time.RFC3339) {
		t.Fatalf("expected repeated inbound message fields to be mapped, got %#v", thirdMessage)
	}
	if len(thirdMessage.Reactions) != 1 {
		t.Fatalf("expected one outbound reaction, got %d", len(thirdMessage.Reactions))
	}
	if thirdMessage.Reactions[0].SenderJID != accountJID.String() || thirdMessage.Reactions[0].SenderDisplayName != "Primary Account" || !thirdMessage.Reactions[0].IsFromMe || thirdMessage.Reactions[0].Emoji != "\U0001f389" || thirdMessage.Reactions[0].Timestamp != now.Add(5*time.Minute).Format(time.RFC3339) {
		t.Fatalf("expected outbound reaction fields to be mapped, got %#v", thirdMessage.Reactions[0])
	}
}

func TestGetChatMessagesCountsOnlySelectedDevice(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	deviceID := accountJID.String()
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.August, 21, 8, 0, 0, 0, time.UTC)
	repo := &chatUsecaseRepoStub{
		chat: &domainChatStorage.Chat{
			DeviceID:        deviceID,
			JID:             chatJID,
			Name:            "Alice",
			LastMessageTime: now,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		messages: []*domainChatStorage.Message{{
			ID:        "msg-1",
			ChatJID:   chatJID,
			DeviceID:  deviceID,
			Sender:    chatJID,
			Content:   "hello",
			Timestamp: now,
			CreatedAt: now,
			UpdatedAt: now,
		}},
		globalMessageCount: 15,
		deviceMessageCounts: map[string]int64{
			deviceID + "\x00" + chatJID: 1,
		},
	}
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID}}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))

	response, err := NewChatService(repo).GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
		ChatJID: chatJID,
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}
	if response.Pagination.Total != 1 {
		t.Fatalf("pagination total = %d, want selected device's count 1", response.Pagination.Total)
	}
}

func TestGetChatMessagesScopesSenderDisplayNameCacheToOneResponse(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	senderJID := types.NewJID("628123456789", types.DefaultUserServer)
	deviceID := accountJID.String()
	chatJID := "120363999000111@g.us"
	now := time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC)
	contacts := &countingContactStore{
		contacts: map[types.JID]types.ContactInfo{
			senderJID: {
				Found:    true,
				FullName: "Saved Participant",
			},
		},
	}
	repo := &chatUsecaseRepoStub{
		chat: &domainChatStorage.Chat{
			DeviceID:        deviceID,
			JID:             chatJID,
			Name:            "Study Group",
			LastMessageTime: now,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		messages: []*domainChatStorage.Message{
			{
				ID:        "msg-1",
				ChatJID:   chatJID,
				DeviceID:  deviceID,
				Sender:    senderJID.String(),
				Content:   "hello",
				Timestamp: now,
				CreatedAt: now,
				UpdatedAt: now,
				Reactions: []domainChatStorage.Reaction{
					{
						MessageID:  "msg-1",
						ChatJID:    chatJID,
						DeviceID:   deviceID,
						ReactorJID: senderJID.String(),
						Emoji:      "\U0001f44d",
						Timestamp:  now.Add(time.Minute),
					},
				},
			},
			{
				ID:        "msg-2",
				ChatJID:   chatJID,
				DeviceID:  deviceID,
				Sender:    senderJID.String(),
				Content:   "again",
				Timestamp: now.Add(2 * time.Minute),
				CreatedAt: now.Add(2 * time.Minute),
				UpdatedAt: now.Add(2 * time.Minute),
			},
		},
	}
	client := &whatsmeow.Client{
		Store: &store.Device{
			ID:       &accountJID,
			PushName: "Primary Account",
			Contacts: contacts,
		},
	}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))
	service := NewChatService(repo)
	request := domainChat.GetChatMessagesRequest{
		ChatJID: chatJID,
		Limit:   50,
	}

	firstResponse, err := service.GetChatMessages(ctx, request)
	if err != nil {
		t.Fatalf("first GetChatMessages: %v", err)
	}
	if len(firstResponse.Data) != 2 || len(firstResponse.Data[0].Reactions) != 1 {
		t.Fatalf("first response messages/reactions = %#v", firstResponse.Data)
	}
	if firstResponse.Data[0].SenderDisplayName != "Saved Participant" ||
		firstResponse.Data[0].Reactions[0].SenderDisplayName != "Saved Participant" ||
		firstResponse.Data[1].SenderDisplayName != "Saved Participant" {
		t.Fatalf("first response sender display names = %#v", firstResponse.Data)
	}
	if contacts.reads != 1 {
		t.Fatalf("contact reads after first response = %d, want 1", contacts.reads)
	}

	secondResponse, err := service.GetChatMessages(ctx, request)
	if err != nil {
		t.Fatalf("second GetChatMessages: %v", err)
	}
	if len(secondResponse.Data) != 2 || len(secondResponse.Data[0].Reactions) != 1 {
		t.Fatalf("second response messages/reactions = %#v", secondResponse.Data)
	}
	if secondResponse.Data[0].SenderDisplayName != "Saved Participant" ||
		secondResponse.Data[0].Reactions[0].SenderDisplayName != "Saved Participant" ||
		secondResponse.Data[1].SenderDisplayName != "Saved Participant" {
		t.Fatalf("second response sender display names = %#v", secondResponse.Data)
	}
	if contacts.reads != 2 {
		t.Fatalf("contact reads after second response = %d, want 2", contacts.reads)
	}
}

func TestChatSenderDisplayNameJSONContract(t *testing.T) {
	payload, err := json.Marshal(domainChat.MessageInfo{
		SenderJID:         "628123456789@s.whatsapp.net",
		SenderDisplayName: "Alice",
		Reactions: []domainChat.ReactionInfo{{
			Emoji:             "\U0001f44d",
			SenderJID:         "628111111111@s.whatsapp.net",
			SenderDisplayName: "Bob",
		}},
	})
	if err != nil {
		t.Fatalf("marshal message payload: %v", err)
	}

	var message map[string]any
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("unmarshal message payload: %v", err)
	}
	if message["sender_jid"] != "628123456789@s.whatsapp.net" {
		t.Fatalf("message sender_jid = %#v", message["sender_jid"])
	}
	if message["sender_display_name"] != "Alice" {
		t.Fatalf("message sender_display_name = %#v", message["sender_display_name"])
	}
	reactions, ok := message["reactions"].([]any)
	if !ok || len(reactions) != 1 {
		t.Fatalf("message reactions = %#v", message["reactions"])
	}
	reaction, ok := reactions[0].(map[string]any)
	if !ok {
		t.Fatalf("reaction payload = %#v", reactions[0])
	}
	if reaction["sender_jid"] != "628111111111@s.whatsapp.net" {
		t.Fatalf("reaction sender_jid = %#v", reaction["sender_jid"])
	}
	if reaction["sender_display_name"] != "Bob" {
		t.Fatalf("reaction sender_display_name = %#v", reaction["sender_display_name"])
	}
}

func TestGetChatMessagesExposesReferralMetadata(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	deviceID := accountJID.String()
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC)
	referralMetadata := `{"ctwa_clid":"clid_123","source_app":"whatsapp"}`
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
				ID:               "msg-ad",
				ChatJID:          chatJID,
				DeviceID:         deviceID,
				Sender:           chatJID,
				Content:          "hello from the ad",
				Timestamp:        now,
				CreatedAt:        now,
				UpdatedAt:        now,
				ReferralMetadata: referralMetadata,
			},
			{
				ID:        "msg-plain",
				ChatJID:   chatJID,
				DeviceID:  deviceID,
				Sender:    chatJID,
				Content:   "hello again",
				Timestamp: now.Add(time.Minute),
				CreatedAt: now.Add(time.Minute),
				UpdatedAt: now.Add(time.Minute),
			},
		},
	}
	service := NewChatService(repo)
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID, PushName: "Primary Account"}}
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))

	response, err := service.GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
		ChatJID: chatJID,
		Limit:   50,
	})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("expected two messages, got %d", len(response.Data))
	}
	if response.Data[0].ReferralMetadata != referralMetadata {
		t.Fatalf("referral metadata = %q, want %q", response.Data[0].ReferralMetadata, referralMetadata)
	}
	if response.Data[1].ReferralMetadata != "" {
		t.Fatalf("expected empty referral metadata, got %q", response.Data[1].ReferralMetadata)
	}

	payload, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatalf("marshal messages payload: %v", err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(payload, &messages); err != nil {
		t.Fatalf("unmarshal messages payload: %v", err)
	}
	if messages[0]["referral_metadata"] != referralMetadata {
		t.Fatalf("message referral_metadata = %#v", messages[0]["referral_metadata"])
	}
	if _, ok := messages[1]["referral_metadata"]; ok {
		t.Fatalf("referral_metadata must be omitted when empty, got %#v", messages[1])
	}
}

type chatUsecaseRepoStub struct {
	domainChatStorage.IChatStorageRepository
	chat                  *domainChatStorage.Chat
	chats                 []*domainChatStorage.Chat
	messages              []*domainChatStorage.Message
	globalMessageCount    int64
	deviceMessageCounts   map[string]int64
	oldestMessage         *domainChatStorage.Message
	oldestMessageErr      error
	oldestMessageCalled   bool
	oldestMessageDeviceID string
	oldestMessageChatJID  string
}

func (r *chatUsecaseRepoStub) GetOldestMessageByDevice(deviceID, chatJID string) (*domainChatStorage.Message, error) {
	r.oldestMessageCalled = true
	r.oldestMessageDeviceID = deviceID
	r.oldestMessageChatJID = chatJID
	return r.oldestMessage, r.oldestMessageErr
}

func (r *chatUsecaseRepoStub) GetChatByDevice(_, _ string) (*domainChatStorage.Chat, error) {
	return r.chat, nil
}

func (r *chatUsecaseRepoStub) GetChats(*domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	return r.chats, nil
}

func (r *chatUsecaseRepoStub) GetFilteredChatCount(*domainChatStorage.ChatFilter) (int64, error) {
	return int64(len(r.chats)), nil
}

func (r *chatUsecaseRepoStub) GetMessages(*domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return r.messages, nil
}

func (r *chatUsecaseRepoStub) GetChatMessageCount(string) (int64, error) {
	if r.globalMessageCount != 0 {
		return r.globalMessageCount, nil
	}
	return int64(len(r.messages)), nil
}

func (r *chatUsecaseRepoStub) GetChatMessageCountByDevice(deviceID, chatJID string) (int64, error) {
	if r.deviceMessageCounts == nil {
		return int64(len(r.messages)), nil
	}
	return r.deviceMessageCounts[deviceID+"\x00"+chatJID], nil
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

// TestRequestChatHistoryValidation pins the request-body validation (bad
// chat_jid, out-of-range count) that RequestChatHistory delegates to
// validations.ValidateRequestChatHistory before touching storage or the
// network.
func TestRequestChatHistoryValidation(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	deviceID := accountJID.String()
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID}}
	repo := &chatUsecaseRepoStub{}
	service := NewChatService(repo)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))

	_, err := service.RequestChatHistory(ctx, domainChat.RequestChatHistoryRequest{
		ChatJID: "",
		Count:   50,
	})
	if err == nil {
		t.Fatal("expected validation error for empty chat_jid, got nil")
	}
	if repo.oldestMessageCalled {
		t.Fatal("expected anchor lookup to be skipped when validation fails")
	}

	_, err = service.RequestChatHistory(ctx, domainChat.RequestChatHistoryRequest{
		ChatJID: "628123456789@s.whatsapp.net",
		Count:   501,
	})
	if err == nil {
		t.Fatal("expected validation error for count above cap, got nil")
	}
}

// TestRequestChatHistoryRequiresClient mirrors the other chat mutation
// usecases (PinChat/ArchiveChat): without a WhatsApp client in context, the
// call must fail with ErrWaCLI instead of attempting anything else.
func TestRequestChatHistoryRequiresClient(t *testing.T) {
	deviceID := "628999999999@s.whatsapp.net"
	repo := &chatUsecaseRepoStub{}
	service := NewChatService(repo)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	_, err := service.RequestChatHistory(ctx, domainChat.RequestChatHistoryRequest{
		ChatJID: "628123456789@s.whatsapp.net",
		Count:   50,
	})
	if err != pkgError.ErrWaCLI {
		t.Fatalf("expected ErrWaCLI, got %v", err)
	}
	if repo.oldestMessageCalled {
		t.Fatal("expected anchor lookup to be skipped without a client")
	}
}

// TestRequestChatHistoryNoStoredMessages pins the anchor-resolution rule: the
// phone needs an oldest known message ID/timestamp to know where to start
// looking for older history, so a chat with nothing stored yet must fail
// validation instead of sending an unanchored request.
func TestRequestChatHistoryNoStoredMessages(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	deviceID := accountJID.String()
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID}}
	chatJID := "628123456789@s.whatsapp.net"
	repo := &chatUsecaseRepoStub{oldestMessage: nil}
	service := NewChatService(repo)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, client, nil))

	_, err := service.RequestChatHistory(ctx, domainChat.RequestChatHistoryRequest{
		ChatJID: chatJID,
		Count:   50,
	})
	if err == nil {
		t.Fatal("expected error when chat has no stored messages, got nil")
	}
	if _, ok := err.(pkgError.ValidationError); !ok {
		t.Fatalf("expected a ValidationError, got %T: %v", err, err)
	}
	if !repo.oldestMessageCalled {
		t.Fatal("expected anchor lookup to be attempted")
	}
	if repo.oldestMessageDeviceID != deviceID || repo.oldestMessageChatJID != chatJID {
		t.Fatalf("anchor lookup used device=%q chat=%q, want device=%q chat=%q",
			repo.oldestMessageDeviceID, repo.oldestMessageChatJID, deviceID, chatJID)
	}
}
