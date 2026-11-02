package whatsapp

import (
	"errors"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// replyLinkSpy records the filters that reach the base repository.
type replyLinkSpy struct {
	domainChatStorage.IChatStorageRepository
	filters []domainChatStorage.ChatwootReplyLinkFilter
	link    *domainChatStorage.ChatwootMessageLink
	err     error
}

func (s *replyLinkSpy) GetChatwootMessageLinkForReply(filter domainChatStorage.ChatwootReplyLinkFilter) (*domainChatStorage.ChatwootMessageLink, error) {
	s.filters = append(s.filters, filter)
	return s.link, s.err
}

// The wrapper fills an empty device with its own, keeps an explicit one,
// passes the other fields unchanged and returns the base result as is.
func TestDeviceChatStorageGetChatwootMessageLinkForReply(t *testing.T) {
	filter := domainChatStorage.ChatwootReplyLinkFilter{
		ChatwootMessageID: 41, ChatwootConversationID: 7, ChatwootAccountID: 3,
		ChatwootInboxID: 5, ChatwootConfigID: 11, AllowLegacyAccountZero: true,
	}
	for _, tc := range []struct{ device, want string }{
		{"", "device-a@s.whatsapp.net"},
		{"device-b@s.whatsapp.net", "device-b@s.whatsapp.net"},
	} {
		spy := &replyLinkSpy{link: &domainChatStorage.ChatwootMessageLink{WhatsAppMessageID: "WA-ORIG"}, err: errors.New("database is locked")}
		in := filter
		in.DeviceID = tc.device

		link, err := newDeviceChatStorage("device-a@s.whatsapp.net", spy).GetChatwootMessageLinkForReply(in)

		want := filter
		want.DeviceID = tc.want
		if len(spy.filters) != 1 || spy.filters[0] != want {
			t.Fatalf("device %q: base filters = %+v, want [%+v]", tc.device, spy.filters, want)
		}
		if link != spy.link || !errors.Is(err, spy.err) {
			t.Fatalf("device %q: got (%v, %v), want the base result", tc.device, link, err)
		}
	}
}
