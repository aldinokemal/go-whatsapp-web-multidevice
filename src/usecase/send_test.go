package usecase

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type sentMessageStoreContextKey string

func TestBuildSentMessageStoreContextPreservesValuesAndDetachesCancellation(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "preserves values and detaches parent cancellation",
			timeout: time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := context.WithValue(context.Background(), sentMessageStoreContextKey("device"), "device-123")
			parent, cancelParent := context.WithCancel(parent)

			storeCtx, cancel := buildSentMessageStoreContext(parent, tt.timeout)
			defer cancel()

			cancelParent()

			require.Equal(t, "device-123", storeCtx.Value(sentMessageStoreContextKey("device")))
			assert.NoError(t, storeCtx.Err())
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
