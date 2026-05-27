package usecase

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
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
