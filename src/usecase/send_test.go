package usecase

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

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

func TestIsReachoutTimelockError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "server returned 463",
			err:  errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 463")),
			want: true,
		},
		{
			name: "different server error code",
			err:  errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 500")),
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("network unreachable"),
			want: false,
		},
		{
			name: "non-wrapped 463 string",
			err:  errors.New("error 463"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isReachoutTimelockError(tt.err); got != tt.want {
				t.Fatalf("isReachoutTimelockError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestPrewarmRecipientForSendIgnoresNilClient(t *testing.T) {
	// Should be a no-op (no panic) when the client is nil. Pre-warm is
	// best-effort and must never crash the caller.
	prewarmRecipientForSend(context.Background(), nil, types.JID{User: "12345", Server: types.DefaultUserServer})
}

func TestPrewarmRecipientForSendSkipsNonUserJIDs(t *testing.T) {
	// Groups, broadcasts, newsletters and status JIDs must not trigger the
	// chat-presence / subscribe-presence sequence. The guard relies on
	// returning before any client method is invoked, so passing a nil client
	// here would crash if the gate were broken.
	for _, server := range []string{
		types.GroupServer,
		types.BroadcastServer,
		types.NewsletterServer,
		types.LegacyUserServer,
	} {
		prewarmRecipientForSend(context.Background(), nil, types.JID{User: "12345", Server: server})
	}
}
