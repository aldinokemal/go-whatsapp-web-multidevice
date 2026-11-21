package whatsapp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const extraTestDeviceID = "5541999990000@s.whatsapp.net"

var extraTestTime = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func assertExtraEnvelope(t *testing.T, body map[string]any, eventName, timestamp, payloadJSON string) {
	t.Helper()
	if body == nil {
		t.Fatal("expected a webhook body, got nil")
	}
	if got := body["event"]; got != eventName {
		t.Errorf("event = %v, want %s", got, eventName)
	}
	if got := body["device_id"]; got != extraTestDeviceID {
		t.Errorf("device_id = %v, want %s", got, extraTestDeviceID)
	}
	if timestamp != "" {
		if got := body["timestamp"]; got != timestamp {
			t.Errorf("timestamp = %v, want %s", got, timestamp)
		}
	} else if ts, _ := body["timestamp"].(string); ts == "" {
		t.Error("timestamp is missing")
	} else if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", ts, err)
	}
	if len(body) != 4 {
		t.Errorf("envelope has keys %v, want only event, device_id, timestamp and payload", body)
	}
	got, err := json.Marshal(body["payload"])
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if err := json.Unmarshal([]byte(payloadJSON), &wantValue); err != nil {
		t.Fatalf("unmarshal expected payload: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("payload = %s\nwant      %s", got, payloadJSON)
	}
}

func TestAppStatePayload(t *testing.T) {
	chat := types.NewJID("5541988887777", types.DefaultUserServer)
	group := types.NewJID("120363402106000000", types.GroupServer)
	tests := []struct {
		name string
		evt  any
		want string
	}{
		{
			name: "archive",
			evt:  &events.Archive{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}},
			want: `{"action":"archive","chat_id":"5541988887777@s.whatsapp.net","value":true}`,
		},
		{
			name: "unarchive keeps false value",
			evt:  &events.Archive{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(false)}},
			want: `{"action":"archive","chat_id":"5541988887777@s.whatsapp.net","value":false}`,
		},
		{
			name: "pin",
			evt:  &events.Pin{JID: group, Timestamp: extraTestTime, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}},
			want: `{"action":"pin","chat_id":"120363402106000000@g.us","value":true}`,
		},
		{
			name: "unpin keeps false value",
			evt:  &events.Pin{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.PinAction{Pinned: proto.Bool(false)}},
			want: `{"action":"pin","chat_id":"5541988887777@s.whatsapp.net","value":false}`,
		},
		{
			name: "mute until a time in milliseconds",
			evt: &events.Mute{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.MuteAction{
				Muted:            proto.Bool(true),
				MuteEndTimestamp: proto.Int64(extraTestTime.Add(8 * time.Hour).UnixMilli()),
			}},
			want: `{"action":"mute","chat_id":"5541988887777@s.whatsapp.net","until":"2026-10-22T20:00:00Z","value":true}`,
		},
		{
			name: "mute forever omits until",
			evt:  &events.Mute{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.MuteAction{Muted: proto.Bool(true), MuteEndTimestamp: proto.Int64(-1)}},
			want: `{"action":"mute","chat_id":"5541988887777@s.whatsapp.net","value":true}`,
		},
		{
			name: "unmute",
			evt:  &events.Mute{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.MuteAction{Muted: proto.Bool(false)}},
			want: `{"action":"mute","chat_id":"5541988887777@s.whatsapp.net","value":false}`,
		},
		{
			name: "clear chat",
			evt:  &events.ClearChat{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.ClearChatAction{}},
			want: `{"action":"clear","chat_id":"5541988887777@s.whatsapp.net"}`,
		},
		{
			name: "delete chat",
			evt:  &events.DeleteChat{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.DeleteChatAction{}},
			want: `{"action":"delete_chat","chat_id":"5541988887777@s.whatsapp.net"}`,
		},
		{
			name: "saved contact",
			evt: &events.Contact{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.ContactAction{
				FullName:  proto.String("Maria Silva"),
				FirstName: proto.String("Maria"),
			}},
			want: `{"action":"contact","chat_id":"5541988887777@s.whatsapp.net","contact":{"first_name":"Maria","full_name":"Maria Silva"}}`,
		},
		{
			name: "contact without names omits contact",
			evt:  &events.Contact{JID: chat, Timestamp: extraTestTime, Action: &waSyncAction.ContactAction{}},
			want: `{"action":"contact","chat_id":"5541988887777@s.whatsapp.net"}`,
		},
		{
			name: "lid chat keeps chat_lid",
			evt:  &events.ClearChat{JID: types.NewJID("123456789", types.HiddenUserServer), Timestamp: extraTestTime},
			want: `{"action":"clear","chat_id":"123456789@lid","chat_lid":"123456789@lid"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := appStatePayload(context.Background(), tt.evt, extraTestDeviceID, nil)
			assertExtraEnvelope(t, body, "app_state", "2026-10-22T12:00:00Z", tt.want)
		})
	}
}

func TestAppStatePayloadSkipsFullSync(t *testing.T) {
	chat := types.NewJID("5541988887777", types.DefaultUserServer)
	for _, evt := range []any{
		&events.Archive{JID: chat, FromFullSync: true, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}},
		&events.Pin{JID: chat, FromFullSync: true, Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}},
		&events.Mute{JID: chat, FromFullSync: true, Action: &waSyncAction.MuteAction{Muted: proto.Bool(true)}},
		&events.ClearChat{JID: chat, FromFullSync: true},
		&events.DeleteChat{JID: chat, FromFullSync: true},
		&events.Contact{JID: chat, FromFullSync: true, Action: &waSyncAction.ContactAction{FullName: proto.String("Maria")}},
	} {
		if body := appStatePayload(context.Background(), evt, extraTestDeviceID, nil); body != nil {
			t.Errorf("%T from full sync produced %v, want nil", evt, body)
		}
	}
}

func TestAppStatePayloadIgnoresOtherEvents(t *testing.T) {
	if body := appStatePayload(context.Background(), &events.Presence{}, extraTestDeviceID, nil); body != nil {
		t.Errorf("unexpected body %v", body)
	}
}

func TestContactUpdatePayload(t *testing.T) {
	user := types.NewJID("5541988887777", types.DefaultUserServer)
	tests := []struct {
		name      string
		evt       any
		timestamp string
		want      string
	}{
		{
			name:      "push name",
			evt:       &events.PushName{JID: user, Message: &types.MessageInfo{Timestamp: extraTestTime}, OldPushName: "Mari", NewPushName: "Maria"},
			timestamp: "2026-10-22T12:00:00Z",
			want:      `{"from":"5541988887777@s.whatsapp.net","kind":"push_name","push_name":"Maria"}`,
		},
		{
			name:      "picture changed",
			evt:       &events.Picture{JID: user, Timestamp: extraTestTime, PictureID: "1234"},
			timestamp: "2026-10-22T12:00:00Z",
			want:      `{"from":"5541988887777@s.whatsapp.net","kind":"picture"}`,
		},
		{
			name:      "picture removed",
			evt:       &events.Picture{JID: user, Timestamp: extraTestTime, Remove: true},
			timestamp: "2026-10-22T12:00:00Z",
			want:      `{"from":"5541988887777@s.whatsapp.net","kind":"picture","picture_removed":true}`,
		},
		{
			name:      "about",
			evt:       &events.UserAbout{JID: user, Status: "Disponível", Timestamp: extraTestTime},
			timestamp: "2026-10-22T12:00:00Z",
			want:      `{"about":"Disponível","from":"5541988887777@s.whatsapp.net","kind":"about"}`,
		},
		{
			name: "business name without message",
			evt:  &events.BusinessName{JID: user, OldBusinessName: "Loja", NewBusinessName: "Loja da Maria"},
			want: `{"business_name":"Loja da Maria","from":"5541988887777@s.whatsapp.net","kind":"business_name"}`,
		},
		{
			name:      "lid sender keeps from_lid",
			evt:       &events.PushName{JID: types.NewJID("123456789", types.HiddenUserServer), Message: &types.MessageInfo{Timestamp: extraTestTime}, NewPushName: "Maria"},
			timestamp: "2026-10-22T12:00:00Z",
			want:      `{"from":"123456789@lid","from_lid":"123456789@lid","kind":"push_name","push_name":"Maria"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := contactUpdatePayload(context.Background(), tt.evt, extraTestDeviceID, nil)
			assertExtraEnvelope(t, body, "contact.update", tt.timestamp, tt.want)
		})
	}
}

func TestContactUpdatePayloadIgnoresOtherEvents(t *testing.T) {
	if body := contactUpdatePayload(context.Background(), &events.Archive{}, extraTestDeviceID, nil); body != nil {
		t.Errorf("unexpected body %v", body)
	}
}

func TestCallAcceptPayload(t *testing.T) {
	caller := types.NewJID("5541988887777", types.DefaultUserServer)
	tests := []struct {
		name string
		evt  *events.CallAccept
		want string
	}{
		{
			name: "direct call",
			evt: &events.CallAccept{BasicCallMeta: types.BasicCallMeta{
				From: caller, CallCreator: caller, CallID: "ABC123", Timestamp: extraTestTime,
			}},
			want: `{"call_id":"ABC123","from":"5541988887777@s.whatsapp.net"}`,
		},
		{
			name: "group call",
			evt: &events.CallAccept{BasicCallMeta: types.BasicCallMeta{
				From: caller, CallCreator: caller, CallID: "ABC123", Timestamp: extraTestTime,
				GroupJID: types.NewJID("120363402106000000", types.GroupServer),
			}},
			want: `{"call_id":"ABC123","from":"5541988887777@s.whatsapp.net","group_jid":"120363402106000000@g.us"}`,
		},
		{
			name: "lid creator keeps from_lid",
			evt: &events.CallAccept{BasicCallMeta: types.BasicCallMeta{
				From: caller, CallCreator: types.JID{User: "123456789", Device: 12, Server: types.HiddenUserServer}, CallID: "ABC123", Timestamp: extraTestTime,
			}},
			want: `{"call_id":"ABC123","from":"123456789@lid","from_lid":"123456789@lid"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := callAcceptPayload(context.Background(), tt.evt, extraTestDeviceID, nil)
			assertExtraEnvelope(t, body, "call.accept", "2026-10-22T12:00:00Z", tt.want)
		})
	}
}

func TestCallTerminatePayload(t *testing.T) {
	caller := types.NewJID("5541988887777", types.DefaultUserServer)
	tests := []struct {
		name string
		evt  *events.CallTerminate
		want string
	}{
		{
			name: "with reason",
			evt: &events.CallTerminate{BasicCallMeta: types.BasicCallMeta{
				From: caller, CallCreator: caller, CallID: "ABC123", Timestamp: extraTestTime,
				GroupJID: types.NewJID("120363402106000000", types.GroupServer),
			}, Reason: "timeout"},
			want: `{"call_id":"ABC123","from":"5541988887777@s.whatsapp.net","group_jid":"120363402106000000@g.us","reason":"timeout"}`,
		},
		{
			name: "without reason",
			evt: &events.CallTerminate{BasicCallMeta: types.BasicCallMeta{
				From: caller, CallCreator: caller, CallID: "ABC123", Timestamp: extraTestTime,
			}},
			want: `{"call_id":"ABC123","from":"5541988887777@s.whatsapp.net"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := callTerminatePayload(context.Background(), tt.evt, extraTestDeviceID, nil)
			assertExtraEnvelope(t, body, "call.terminate", "2026-10-22T12:00:00Z", tt.want)
		})
	}
}

func TestExtraEventsRespectIgnoredJIDs(t *testing.T) {
	ignored := types.NewJID("5541988887777", types.DefaultUserServer)
	group := types.NewJID("120363402106000000", types.GroupServer)
	ctx := context.Background()

	appState := appStatePayload(ctx, &events.Archive{JID: group, Action: &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)}}, extraTestDeviceID, nil)
	if runIgnoreJidForward(t, []string{"@g.us"}, "app_state", appState) {
		t.Error("app_state for an ignored chat was forwarded")
	}

	lidChat := appStatePayload(ctx, &events.Pin{JID: types.NewJID("123456789", types.HiddenUserServer), Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}}, extraTestDeviceID, nil)
	if runIgnoreJidForward(t, []string{"@lid"}, "app_state", lidChat) {
		t.Error("app_state for an ignored LID chat was forwarded")
	}

	contact := contactUpdatePayload(ctx, &events.UserAbout{JID: ignored, Status: "x"}, extraTestDeviceID, nil)
	if runIgnoreJidForward(t, []string{ignored.String()}, "contact.update", contact) {
		t.Error("contact.update from an ignored JID was forwarded")
	}

	call := callTerminatePayload(ctx, &events.CallTerminate{BasicCallMeta: types.BasicCallMeta{CallCreator: ignored, CallID: "ABC123"}}, extraTestDeviceID, nil)
	if runIgnoreJidForward(t, []string{ignored.String()}, "call.terminate", call) {
		t.Error("call.terminate from an ignored JID was forwarded")
	}

	allowed := contactUpdatePayload(ctx, &events.UserAbout{JID: types.NewJID("5541900000000", types.DefaultUserServer), Status: "x"}, extraTestDeviceID, nil)
	if !runIgnoreJidForward(t, []string{ignored.String()}, "contact.update", allowed) {
		t.Error("contact.update from a JID outside the ignore list was not forwarded")
	}
}
