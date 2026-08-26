package utils

import (
	"testing"

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
			if got == nil {
				t.Fatal("ExtractPollCreationMessage returned nil")
			}
			if version != tt.wantVersion {
				t.Fatalf("version = %q, want %q", version, tt.wantVersion)
			}
			if got.GetName() != tt.wantVersion {
				t.Fatalf("question = %q, want %q", got.GetName(), tt.wantVersion)
			}
		})
	}
}

func TestExtractPollCreationMessageHandlesNilAndWrappedMessages(t *testing.T) {
	if poll, version := ExtractPollCreationMessage(nil); poll != nil || version != "" {
		t.Fatalf("nil message returned poll=%v version=%q", poll, version)
	}

	wrapper := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("wrapped")}},
	}}
	poll, version := ExtractPollCreationMessage(wrapper)
	if poll == nil || poll.GetName() != "wrapped" || version != "v3" {
		t.Fatalf("wrapped result poll=%v version=%q", poll, version)
	}
}
