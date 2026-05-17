package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
