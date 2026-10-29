package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractPollCreationMessageSupportsAllVersions(t *testing.T) {
	poll := func(question string) *waE2E.PollCreationMessage {
		return &waE2E.PollCreationMessage{Name: proto.String(question)}
	}

	tests := []struct {
		name        string
		message     *waE2E.Message
		wantVersion string
	}{
		{name: "v1", message: &waE2E.Message{PollCreationMessage: poll("v1")}, wantVersion: "v1"},
		{name: "v2", message: &waE2E.Message{PollCreationMessageV2: poll("v2")}, wantVersion: "v2"},
		{name: "v3", message: &waE2E.Message{PollCreationMessageV3: poll("v3")}, wantVersion: "v3"},
		{
			name: "v4 future proof wrapper",
			message: &waE2E.Message{PollCreationMessageV4: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{PollCreationMessage: poll("v4")},
			}},
			wantVersion: "v4",
		},
		{name: "v5", message: &waE2E.Message{PollCreationMessageV5: poll("v5")}, wantVersion: "v5"},
		{name: "v6", message: &waE2E.Message{PollCreationMessageV6: poll("v6")}, wantVersion: "v6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, version := ExtractPollCreationMessage(tt.message)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantVersion, version)
			assert.Equal(t, tt.wantVersion, got.GetName())
		})
	}
}

func TestExtractPollCreationMessageHandlesNilAndWrappedMessages(t *testing.T) {
	poll, version := ExtractPollCreationMessage(nil)
	assert.Nil(t, poll)
	assert.Empty(t, version)

	wrapper := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("wrapped")}},
	}}
	poll, version = ExtractPollCreationMessage(wrapper)
	require.NotNil(t, poll)
	assert.Equal(t, "wrapped", poll.GetName())
	assert.Equal(t, "v3", version)
}
