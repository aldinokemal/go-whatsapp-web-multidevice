package usecase

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestBuildPinMessage(t *testing.T) {
	key := &waCommon.MessageKey{ID: proto.String("3EB0789ABC123456")}
	now := time.UnixMilli(1700000000123)

	t.Run("pin carries type, timestamp and duration", func(t *testing.T) {
		msg := buildPinMessage(key, true, 86400, now)
		require.NotNil(t, msg.GetPinInChatMessage())
		assert.Equal(t, waE2E.PinInChatMessage_PIN_FOR_ALL, msg.GetPinInChatMessage().GetType())
		assert.Equal(t, int64(1700000000123), msg.GetPinInChatMessage().GetSenderTimestampMS())
		assert.Same(t, key, msg.GetPinInChatMessage().GetKey())
		assert.Equal(t, uint32(86400), msg.GetMessageContextInfo().GetMessageAddOnDurationInSecs())
	})

	t.Run("unpin has no duration", func(t *testing.T) {
		msg := buildPinMessage(key, false, 0, now)
		assert.Equal(t, waE2E.PinInChatMessage_UNPIN_FOR_ALL, msg.GetPinInChatMessage().GetType())
		assert.Nil(t, msg.GetMessageContextInfo())
	})
}
