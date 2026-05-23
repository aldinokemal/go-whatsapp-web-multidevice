package whatsapp

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestBuildLabelAppStatePayload(t *testing.T) {
	tests := []struct {
		name      string
		evt       *events.AppState
		eventName string
		payload   map[string]any
	}{
		{
			name: "label edit",
			evt: &events.AppState{
				Index: []string{"label_edit", "9"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelEditAction: &waSyncAction.LabelEditAction{
						Name:        proto.String("Accounts"),
						Color:       proto.Int32(8),
						Deleted:     proto.Bool(false),
						OrderIndex:  proto.Int32(9),
						IsActive:    proto.Bool(true),
						IsImmutable: proto.Bool(false),
						Type:        waSyncAction.LabelEditAction_CUSTOM.Enum(),
					},
				},
			},
			eventName: eventTypeLabelEdit,
			payload: map[string]any{
				"label_id":    "9",
				"name":        "Accounts",
				"color":       int32(8),
				"deleted":     false,
				"order_index": int32(9),
				"is_active":   true,
				"is_immutable": false,
				"type":        "CUSTOM",
			},
		},
		{
			name: "label association",
			evt: &events.AppState{
				Index: []string{"label_jid", "9", "120363424051089958@g.us"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelAssociationAction: &waSyncAction.LabelAssociationAction{
						Labeled: proto.Bool(true),
					},
				},
			},
			eventName: eventTypeLabelAssociation,
			payload: map[string]any{
				"label_id": "9",
				"labeled":  true,
				"chat_id":  "120363424051089958@g.us",
			},
		},
		{
			name: "label association with lid",
			evt: &events.AppState{
				Index: []string{"label_jid", "9", "223754944819424@lid"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelAssociationAction: &waSyncAction.LabelAssociationAction{
						Labeled: proto.Bool(false),
					},
				},
			},
			eventName: eventTypeLabelAssociation,
			payload: map[string]any{
				"label_id": "9",
				"labeled":  false,
				"chat_id":  "223754944819424@lid",
				"chat_lid": "223754944819424@lid",
			},
		},
		{
			name: "label association with unparsable chat id",
			evt: &events.AppState{
				Index: []string{"label_jid", "9", "invalid-chat-id"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelAssociationAction: &waSyncAction.LabelAssociationAction{
						Labeled: proto.Bool(true),
					},
				},
			},
			eventName: eventTypeLabelAssociation,
			payload: map[string]any{
				"label_id": "9",
				"labeled":  true,
				"chat_id":  "invalid-chat-id",
			},
		},
		{
			name: "unrelated appstate",
			evt: &events.AppState{
				Index: []string{"mute", "120363424051089958@g.us"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelAssociationAction: &waSyncAction.LabelAssociationAction{
						Labeled: proto.Bool(true),
					},
				},
			},
		},
		// Defensive guard cases
		{
			name: "nil evt",
			evt:  nil,
		},
		{
			name: "nil SyncActionValue",
			evt: &events.AppState{
				Index:           []string{"label_edit", "9"},
				SyncActionValue: nil,
			},
		},
		{
			name: "label edit with short index",
			evt: &events.AppState{
				Index: []string{"label_edit"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelEditAction: &waSyncAction.LabelEditAction{
						Name: proto.String("Test"),
					},
				},
			},
		},
		{
			name: "label edit with nil LabelEditAction",
			evt: &events.AppState{
				Index: []string{"label_edit", "9"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelEditAction: nil,
				},
			},
		},
		{
			name: "label association with short index",
			evt: &events.AppState{
				Index: []string{"label_jid", "9"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelAssociationAction: &waSyncAction.LabelAssociationAction{
						Labeled: proto.Bool(true),
					},
				},
			},
		},
		{
			name: "label association with nil LabelAssociationAction",
			evt: &events.AppState{
				Index: []string{"label_jid", "9", "120363424051089958@g.us"},
				SyncActionValue: &waSyncAction.SyncActionValue{
					LabelAssociationAction: nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventName, payload := buildLabelAppStatePayload(context.Background(), tt.evt, nil)
			if eventName != tt.eventName {
				t.Fatalf("expected event %q, got %q", tt.eventName, eventName)
			}
			assertPayloadEqual(t, payload, tt.payload)
		})
	}
}

func TestIsLabelAppState(t *testing.T) {
	if !isLabelAppState(&events.AppState{Index: []string{"label_edit", "9"}}) {
		t.Fatal("expected label_edit to be recognized")
	}
	if !isLabelAppState(&events.AppState{Index: []string{"label_jid", "9", "120363424051089958@g.us"}}) {
		t.Fatal("expected label_jid to be recognized")
	}
	if isLabelAppState(&events.AppState{Index: []string{"mute", "120363424051089958@g.us"}}) {
		t.Fatal("expected unrelated appstate to be ignored")
	}
}

func TestLabelAppStateTimestamp(t *testing.T) {
	t.Run("nil evt falls back to now", func(t *testing.T) {
		before := time.Now().UTC().Truncate(time.Second)
		result := labelAppStateTimestamp(&events.AppState{})
		parsed, err := time.Parse(time.RFC3339, result)
		if err != nil {
			t.Fatalf("expected RFC3339 timestamp, got %q: %v", result, err)
		}
		if parsed.Before(before) {
			t.Fatalf("expected fallback timestamp to be >= %v, got %v", before, parsed)
		}
	})

	t.Run("zero timestamp falls back to now", func(t *testing.T) {
		before := time.Now().UTC().Truncate(time.Second)
		result := labelAppStateTimestamp(&events.AppState{
			SyncActionValue: &waSyncAction.SyncActionValue{
				Timestamp: proto.Int64(0),
			},
		})
		parsed, err := time.Parse(time.RFC3339, result)
		if err != nil {
			t.Fatalf("expected RFC3339 timestamp, got %q: %v", result, err)
		}
		if parsed.Before(before) {
			t.Fatalf("expected fallback timestamp to be >= %v, got %v", before, parsed)
		}
	})

	t.Run("valid ms timestamp is formatted correctly", func(t *testing.T) {
		// 2024-01-15T10:30:00Z in milliseconds
		ms := int64(1705314600000)
		expected := "2024-01-15T10:30:00Z"
		result := labelAppStateTimestamp(&events.AppState{
			SyncActionValue: &waSyncAction.SyncActionValue{
				Timestamp: proto.Int64(ms),
			},
		})
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})
}

func assertPayloadEqual(t *testing.T, actual map[string]any, expected map[string]any) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Fatalf("expected payload length %d, got %d: %#v", len(expected), len(actual), actual)
	}
	for key, expectedValue := range expected {
		actualValue, ok := actual[key]
		if !ok {
			t.Fatalf("expected payload key %q", key)
		}
		if !reflect.DeepEqual(actualValue, expectedValue) {
			t.Fatalf("expected payload[%q] to be %#v, got %#v", key, expectedValue, actualValue)
		}
	}
}
