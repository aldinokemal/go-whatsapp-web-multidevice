package pgimport

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func TestIsGroupJID(t *testing.T) {
	tests := []struct {
		jid  string
		want bool
	}{
		{"1234567890@s.whatsapp.net", false},
		{"120363123456789@g.us", true},
		{"foo@lid", false},
		{"status@broadcast", false},
	}
	for _, tt := range tests {
		if got := isGroupJID(tt.jid); got != tt.want {
			t.Errorf("isGroupJID(%q) = %v, want %v", tt.jid, got, tt.want)
		}
	}
}

func TestIsLidJID(t *testing.T) {
	if !isLidJID("abc@lid") {
		t.Error("expected @lid to be true")
	}
	if isLidJID("abc@s.whatsapp.net") {
		t.Error("expected s.whatsapp.net to be false")
	}
}

func TestContactIdentity_IndividualChat(t *testing.T) {
	phone, identifier, name := contactIdentity("6281234567890@s.whatsapp.net", "Alice")
	if phone == "" {
		t.Errorf("expected phone to be normalized, got empty")
	}
	if identifier != "" {
		t.Errorf("expected empty identifier for 1:1 chat, got %q", identifier)
	}
	if name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", name)
	}
}

func TestContactIdentity_Group(t *testing.T) {
	jid := "120363123456789@g.us"
	phone, identifier, name := contactIdentity(jid, "Project Team")
	if phone != "" {
		t.Errorf("expected empty phone for group, got %q", phone)
	}
	if identifier != jid {
		t.Errorf("expected identifier == jid for group, got %q", identifier)
	}
	if name != "Project Team" {
		t.Errorf("expected name 'Project Team', got %q", name)
	}
}

func TestContactIdentity_Lid(t *testing.T) {
	jid := "987654321@lid"
	phone, identifier, _ := contactIdentity(jid, "Masked User")
	if phone != "" {
		t.Errorf("expected empty phone for @lid, got %q", phone)
	}
	if identifier != jid {
		t.Errorf("expected identifier == jid for @lid, got %q", identifier)
	}
}

func TestContactIdentity_FallbackName(t *testing.T) {
	_, _, name := contactIdentity("6281234567890@s.whatsapp.net", "")
	if name == "" {
		t.Error("expected fallback name derived from JID, got empty")
	}
}

func TestMessageTypeForWA(t *testing.T) {
	if messageTypeForWA(true) != messageTypeOutgoing {
		t.Errorf("expected outgoing for IsFromMe=true")
	}
	if messageTypeForWA(false) != messageTypeIncoming {
		t.Errorf("expected incoming for IsFromMe=false")
	}
}

func TestBuildContent_PlainText(t *testing.T) {
	msg := &domainChatStorage.Message{Content: "hello world"}
	got := buildContent(msg, false)
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

func TestBuildContent_NoDateStampingInBody(t *testing.T) {
	// Regression guard for issue #580: unlike the REST path, the direct-DB
	// importer must NOT prepend a "[YYYY-MM-DD HH:MM]" marker to the body,
	// because Chatwoot's own created_at column now holds the real timestamp.
	msg := &domainChatStorage.Message{Content: "hello"}
	got := buildContent(msg, false)
	if got != "hello" {
		t.Errorf("expected raw body 'hello', got %q", got)
	}
}

func TestBuildContent_GroupPrefix(t *testing.T) {
	msg := &domainChatStorage.Message{
		Content: "morning!",
		Sender:  "6281234567890@s.whatsapp.net",
	}
	got := buildContent(msg, true)
	if got != "6281234567890: morning!" {
		t.Errorf("unexpected group body: %q", got)
	}
}

func TestBuildContent_MediaPlaceholder(t *testing.T) {
	prev := config.ChatwootImportPlaceholderMediaMessage
	defer func() { config.ChatwootImportPlaceholderMediaMessage = prev }()

	config.ChatwootImportPlaceholderMediaMessage = true
	msg := &domainChatStorage.Message{MediaType: "image"}
	if got := buildContent(msg, false); got != "[image]" {
		t.Errorf("expected '[image]' placeholder, got %q", got)
	}

	config.ChatwootImportPlaceholderMediaMessage = false
	if got := buildContent(msg, false); got != "" {
		t.Errorf("expected empty body when placeholder disabled, got %q", got)
	}
}
