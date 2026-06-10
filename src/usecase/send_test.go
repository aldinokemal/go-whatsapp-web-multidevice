package usecase

import (
	"context"
	"errors"
	"net/http"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

type replyMessageRepo struct {
	domainChatStorage.IChatStorageRepository
	message     *domainChatStorage.Message
	err         error
	gotID       string
	gotDeviceID string
}

func (r *replyMessageRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.gotDeviceID = deviceID
	r.gotID = id
	return r.message, r.err
}

func TestWithoutCancelPreservesDeviceContext(t *testing.T) {
	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	storeCtx := context.WithoutCancel(cancelledCtx)
	inst, ok := whatsapp.DeviceFromContext(storeCtx)
	if !ok || inst == nil {
		t.Fatal("expected device instance to remain in detached context")
	}
	if got := inst.ID(); got != deviceID {
		t.Fatalf("expected device id %q, got %q", deviceID, got)
	}
}

func TestResolveDocumentMIME(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantMIME string
	}{
		{
			name:     "Docx",
			filename: "document.docx",
			wantMIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:     "Xlsx",
			filename: "spreadsheet.xlsx",
			wantMIME: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		},
		{
			name:     "Pptx",
			filename: "presentation.pptx",
			wantMIME: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		},
		{
			name:     "Zip",
			filename: "archive.zip",
			wantMIME: "application/zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDocumentMIME(tt.filename, []byte("dummy"))
			if got != tt.wantMIME {
				t.Fatalf("resolveDocumentMIME() = %q, want %q", got, tt.wantMIME)
			}
		})
	}
}

func TestNormalizeSendErrorMapsReachoutTimelock(t *testing.T) {
	err := normalizeSendError(errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 463")))

	genericErr, ok := err.(pkgError.GenericError)
	if !ok {
		t.Fatalf("expected generic error, got %T", err)
	}
	if got := genericErr.ErrCode(); got != "WA_REACHOUT_TIMELOCK" {
		t.Fatalf("expected WA_REACHOUT_TIMELOCK code, got %q", got)
	}
	if got := genericErr.StatusCode(); got != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, got)
	}
	if got := genericErr.Error(); got != string(pkgError.ErrWaReachoutTimelock) {
		t.Fatalf("unexpected error message: %q", got)
	}
}

func TestMergeReplyContextAddsQuoteFields(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			Sender:  "628123456789@s.whatsapp.net",
			Content: "quoted message body",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	contextInfo := &waE2E.ContextInfo{}

	deviceID := "6289605618749@s.whatsapp.net"
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
	got := service.mergeReplyContext(ctx, contextInfo, &replyID)

	if got != contextInfo {
		t.Fatal("expected existing context info to be reused")
	}
	if repo.gotID != replyID {
		t.Fatalf("expected lookup for reply ID %q, got %q", replyID, repo.gotID)
	}
	if repo.gotDeviceID != deviceID {
		t.Fatalf("expected device-scoped lookup for %q, got %q", deviceID, repo.gotDeviceID)
	}
	if got.GetStanzaID() != replyID {
		t.Fatalf("expected stanza ID %q, got %q", replyID, got.GetStanzaID())
	}
	if got.GetParticipant() != "628123456789@s.whatsapp.net" {
		t.Fatalf("unexpected participant: %q", got.GetParticipant())
	}
	if got.GetQuotedMessage().GetConversation() != "quoted message body" {
		t.Fatalf("unexpected quoted body: %q", got.GetQuotedMessage().GetConversation())
	}
}

func TestMergeReplyContextPreservesExistingContext(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"
	repo := &replyMessageRepo{
		message: &domainChatStorage.Message{
			Sender:  "628123456789@s.whatsapp.net",
			Content: "quoted message body",
		},
	}
	service := serviceSend{chatStorageRepo: repo}
	contextInfo := &waE2E.ContextInfo{
		IsForwarded:     proto.Bool(true),
		ForwardingScore: proto.Uint32(100),
		Expiration:      proto.Uint32(3600),
		MentionedJID:    []string{"628999999999@s.whatsapp.net"},
	}

	got := service.mergeReplyContext(context.Background(), contextInfo, &replyID)

	if !got.GetIsForwarded() {
		t.Fatal("expected forwarded flag to be preserved")
	}
	if got.GetForwardingScore() != 100 {
		t.Fatalf("expected forwarding score 100, got %d", got.GetForwardingScore())
	}
	if got.GetExpiration() != 3600 {
		t.Fatalf("expected expiration 3600, got %d", got.GetExpiration())
	}
	if len(got.GetMentionedJID()) != 1 || got.GetMentionedJID()[0] != "628999999999@s.whatsapp.net" {
		t.Fatalf("expected mentioned JIDs to be preserved, got %#v", got.GetMentionedJID())
	}
	if got.GetQuotedMessage().GetConversation() != "quoted message body" {
		t.Fatalf("unexpected quoted body: %q", got.GetQuotedMessage().GetConversation())
	}
}

func TestMergeReplyContextLeavesExistingContextWhenReplyUnavailable(t *testing.T) {
	replyID := "3EB089B9D6ADD58153C561"

	tests := []struct {
		name    string
		replyID *string
		message *domainChatStorage.Message
		err     error
	}{
		{
			name: "nil reply ID",
		},
		{
			name:    "empty reply ID",
			replyID: proto.String(""),
		},
		{
			name:    "message not found",
			replyID: &replyID,
		},
		{
			name:    "lookup error",
			replyID: &replyID,
			err:     errors.New("storage unavailable"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contextInfo := &waE2E.ContextInfo{Expiration: proto.Uint32(3600)}
			service := serviceSend{chatStorageRepo: &replyMessageRepo{
				message: tt.message,
				err:     tt.err,
			}}

			got := service.mergeReplyContext(context.Background(), contextInfo, tt.replyID)

			if got != contextInfo {
				t.Fatal("expected existing context info to be reused")
			}
			if got.GetExpiration() != 3600 {
				t.Fatalf("expected expiration to remain 3600, got %d", got.GetExpiration())
			}
			if got.GetStanzaID() != "" {
				t.Fatalf("expected no stanza ID, got %q", got.GetStanzaID())
			}
			if got.GetQuotedMessage() != nil {
				t.Fatalf("expected no quoted message, got %#v", got.GetQuotedMessage())
			}
		})
	}
}
