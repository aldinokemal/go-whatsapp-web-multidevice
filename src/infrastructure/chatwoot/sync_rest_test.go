package chatwoot

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func TestGroupNameResolver_NilClientReturnsEmpty(t *testing.T) {
	// resolve must short-circuit to "" when there is no WhatsApp client,
	// because the caller (SyncHistory) falls back to the stored chat name in
	// that case. Crucially this also lets the resolver be exercised without a
	// live *whatsmeow.Client — the nil guard is the first statement.
	r := newGroupNameResolver()
	if got := r.resolve(context.Background(), nil, "120363123456789@g.us"); got != "" {
		t.Errorf("resolve(nil client) = %q, want empty string", got)
	}
}

func TestPgImporterForSync_EmptyURIReturnsNil(t *testing.T) {
	// pgImporterForSync returns nil when the direct-Postgres import feature is
	// not configured (blank or whitespace-only URI), which is what makes
	// SyncHistory use the REST path. We mutate the package config global and
	// restore it so other tests/packages see no leak.
	orig := config.ChatwootImportDBURI
	defer func() { config.ChatwootImportDBURI = orig }()

	// allowPgImport=true exercises the URI branch (the gate is tested separately).
	s := NewSyncService(nil, nil)
	s.allowPgImport = true

	tests := []struct {
		name string
		uri  string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config.ChatwootImportDBURI = tc.uri
			imp, err := s.pgImporterForSync(context.Background())
			if err != nil {
				t.Fatalf("pgImporterForSync() error = %v, want nil", err)
			}
			if imp != nil {
				t.Errorf("pgImporterForSync() = %v, want nil for URI %q", imp, tc.uri)
			}
			// The cached importer must stay nil so a later configured run can
			// still initialize it.
			if s.pgImporter != nil {
				t.Errorf("s.pgImporter = %v, want nil (must not be cached)", s.pgImporter)
			}
		})
	}
}

func TestPgImporterForSync_ConfiguredURIRequiresAccountAndInbox(t *testing.T) {
	origURI := config.ChatwootImportDBURI
	origAccountID := config.ChatwootAccountID
	origInboxID := config.ChatwootInboxID
	defer func() {
		config.ChatwootImportDBURI = origURI
		config.ChatwootAccountID = origAccountID
		config.ChatwootInboxID = origInboxID
	}()

	config.ChatwootImportDBURI = "postgres://chatwoot:secret@localhost/chatwoot"
	config.ChatwootAccountID = 0
	config.ChatwootInboxID = 99

	s := NewSyncService(nil, nil)
	s.allowPgImport = true // legacy/env service: direct-Postgres import enabled
	imp, err := s.pgImporterForSync(context.Background())
	if err == nil {
		t.Fatal("pgImporterForSync() error = nil, want configured import failure")
	}
	if imp != nil {
		t.Errorf("pgImporterForSync() importer = %v, want nil on failure", imp)
	}
	if s.pgImporter != nil {
		t.Errorf("s.pgImporter = %v, want nil (must not cache failed importer)", s.pgImporter)
	}
}

func TestHasDownloadableChatwootMedia(t *testing.T) {
	tests := []struct {
		name string
		msg  *domainChatStorage.Message
		want bool
	}{
		{
			name: "media with url and key",
			msg:  &domainChatStorage.Message{MediaType: "image", URL: "https://mmg.whatsapp.net/file", MediaKey: []byte("key")},
			want: true,
		},
		{
			name: "media with direct path and key",
			msg:  &domainChatStorage.Message{MediaType: "image", DirectPath: "/v/t62.7118-24/file.enc?ccb=11-4", MediaKey: []byte("key")},
			want: true,
		},
		{
			name: "text message",
			msg:  &domainChatStorage.Message{Content: "hello"},
			want: false,
		},
		{
			name: "media without url or direct path",
			msg:  &domainChatStorage.Message{MediaType: "image", MediaKey: []byte("key")},
			want: false,
		},
		{
			name: "media without key",
			msg:  &domainChatStorage.Message{MediaType: "image", URL: "https://mmg.whatsapp.net/file"},
			want: false,
		},
		{
			name: "nil",
			msg:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasDownloadableChatwootMedia(tt.msg); got != tt.want {
				t.Fatalf("hasDownloadableChatwootMedia() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChatwootRESTMediaCandidates(t *testing.T) {
	prev := config.ChatwootImportMediaWithREST
	defer func() { config.ChatwootImportMediaWithREST = prev }()

	messages := []*domainChatStorage.Message{
		{ID: "text", Content: "hello"},
		{ID: "media", MediaType: "image", URL: "https://mmg.whatsapp.net/file", MediaKey: []byte("key")},
	}

	config.ChatwootImportMediaWithREST = false
	if got := chatwootRESTMediaCandidates(messages, SyncOptions{IncludeMedia: true}); len(got) != 0 {
		t.Fatalf("candidates with config disabled = %d, want 0", len(got))
	}

	config.ChatwootImportMediaWithREST = true
	if got := chatwootRESTMediaCandidates(messages, SyncOptions{IncludeMedia: false}); len(got) != 0 {
		t.Fatalf("candidates with media disabled = %d, want 0", len(got))
	}
	got := chatwootRESTMediaCandidates(messages, SyncOptions{IncludeMedia: true})
	if len(got) != 1 || got[0].ID != "media" {
		t.Fatalf("candidates = %+v, want only media", got)
	}
}
