package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
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

func assertExtraPayload(t *testing.T, body map[string]any, payloadJSON string) {
	t.Helper()
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

func extraTestClient() *whatsmeow.Client {
	own := types.NewJID("5541999990000", types.DefaultUserServer)
	own.Device = 7
	return &whatsmeow.Client{Store: &store.Device{ID: &own, LID: types.NewJID("200000000000001", types.HiddenUserServer)}}
}

func TestGroupInfoPayload(t *testing.T) {
	group := types.NewJID("120363402106000000", types.GroupServer)
	admin := types.NewJID("5541988887777", types.DefaultUserServer)
	adminLID := types.NewJID("100000000000002", types.HiddenUserServer)

	tests := []struct {
		name string
		evt  any
		want string
	}{
		{
			name: "name left to the existing group.info event, announce kept",
			evt: &events.GroupInfo{
				JID:       group,
				Sender:    &admin,
				Timestamp: extraTestTime,
				Name:      &types.GroupName{Name: "Equipe"},
				Announce:  &types.GroupAnnounce{IsAnnounce: true},
			},
			want: `{"chat_id":"120363402106000000@g.us","sender":"5541988887777@s.whatsapp.net","announce":true}`,
		},
		{
			name: "settings turned off are still changes",
			evt: &events.GroupInfo{
				JID:       group,
				Sender:    &admin,
				Timestamp: extraTestTime,
				Topic:     &types.GroupTopic{Topic: "", TopicDeleted: true},
				Locked:    &types.GroupLocked{IsLocked: false},
				Announce:  &types.GroupAnnounce{IsAnnounce: false},
				Ephemeral: &types.GroupEphemeral{IsEphemeral: false},
			},
			want: `{"chat_id":"120363402106000000@g.us","sender":"5541988887777@s.whatsapp.net","topic":"","locked":false,"announce":false,"ephemeral_seconds":0}`,
		},
		{
			name: "ephemeral timer and lid sender resolved through SenderPN",
			evt: &events.GroupInfo{
				JID:       group,
				Sender:    &adminLID,
				SenderPN:  &admin,
				Timestamp: extraTestTime,
				Ephemeral: &types.GroupEphemeral{IsEphemeral: true, DisappearingTimer: 604800},
			},
			want: `{"chat_id":"120363402106000000@g.us","sender":"5541988887777@s.whatsapp.net","ephemeral_seconds":604800}`,
		},
		{
			name: "missing sender is omitted",
			evt:  &events.GroupInfo{JID: group, Timestamp: extraTestTime, Topic: &types.GroupTopic{Topic: "Avisos"}},
			want: `{"chat_id":"120363402106000000@g.us","topic":"Avisos"}`,
		},
		{
			name: "group picture change",
			evt:  &events.Picture{JID: group, Author: admin, Timestamp: extraTestTime, PictureID: "123"},
			want: `{"chat_id":"120363402106000000@g.us","sender":"5541988887777@s.whatsapp.net","picture_changed":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := groupInfoPayload(context.Background(), tt.evt, extraTestDeviceID, nil)
			assertExtraEnvelope(t, body, "group.info", "2026-10-22T12:00:00Z", tt.want)
		})
	}
}

func TestGroupInfoPayloadSkipsEventsWithoutInfoChanges(t *testing.T) {
	group := types.NewJID("120363402106000000", types.GroupServer)
	member := types.NewJID("5541988887777", types.DefaultUserServer)
	skipped := []any{
		&events.GroupInfo{JID: group, Join: []types.JID{member}},
		&events.GroupInfo{JID: group, Name: &types.GroupName{Name: "Equipe"}},
		&events.Picture{JID: member, Timestamp: extraTestTime},
		&events.PushName{JID: member},
	}
	for _, evt := range skipped {
		if body := groupInfoPayload(context.Background(), evt, extraTestDeviceID, nil); body != nil {
			t.Errorf("%T produced a group.info body: %v", evt, body)
		}
	}
}

func TestGroupLeftPayload(t *testing.T) {
	group := types.NewJID("120363402106000000", types.GroupServer)
	admin := types.NewJID("5541988887777", types.DefaultUserServer)
	client := extraTestClient()
	ownPhone := client.Store.ID.ToNonAD()
	ownLID := client.Store.GetLID()
	other := types.NewJID("5541900000000", types.DefaultUserServer)

	t.Run("own phone removed", func(t *testing.T) {
		body := groupLeftPayload(context.Background(), &events.GroupInfo{JID: group, Sender: &admin, Timestamp: extraTestTime, Leave: []types.JID{other, ownPhone}}, extraTestDeviceID, client)
		assertExtraEnvelope(t, body, "group.left", "2026-10-22T12:00:00Z", `{"chat_id":"120363402106000000@g.us","sender":"5541988887777@s.whatsapp.net"}`)
	})
	t.Run("own lid left by itself", func(t *testing.T) {
		body := groupLeftPayload(context.Background(), &events.GroupInfo{JID: group, Sender: &ownLID, SenderPN: &ownPhone, Timestamp: extraTestTime, Leave: []types.JID{ownLID}}, extraTestDeviceID, client)
		assertExtraEnvelope(t, body, "group.left", "2026-10-22T12:00:00Z", `{"chat_id":"120363402106000000@g.us","sender":"5541999990000@s.whatsapp.net"}`)
	})
	t.Run("someone else left", func(t *testing.T) {
		if body := groupLeftPayload(context.Background(), &events.GroupInfo{JID: group, Sender: &admin, Leave: []types.JID{other}}, extraTestDeviceID, client); body != nil {
			t.Errorf("unexpected group.left body: %v", body)
		}
	})
	t.Run("no client", func(t *testing.T) {
		if body := groupLeftPayload(context.Background(), &events.GroupInfo{JID: group, Leave: []types.JID{ownPhone}}, extraTestDeviceID, nil); body != nil {
			t.Errorf("unexpected group.left body without a client: %v", body)
		}
	})
}

func TestConnectionPayload(t *testing.T) {
	evt := &events.PairSuccess{ID: types.JID{User: "5541999990000", Device: 7, Server: types.DefaultUserServer}}
	body := connectionPayload(evt, "session-1", extraTestTime)
	if got := body["event"]; got != "connection" {
		t.Errorf("event = %v, want connection", got)
	}
	if got := body["device_id"]; got != extraTestDeviceID {
		t.Errorf("device_id = %v, want %s", got, extraTestDeviceID)
	}
	if got := body["session_id"]; got != "session-1" {
		t.Errorf("session_id = %v, want session-1", got)
	}
	if got := body["timestamp"]; got != "2026-10-22T12:00:00Z" {
		t.Errorf("timestamp = %v", got)
	}
	assertExtraPayload(t, body, `{"state":"pair_success","phone":"5541999990000@s.whatsapp.net"}`)
}

func TestQRPayload(t *testing.T) {
	body := qrPayload("2@abc,def,ghi", 60*time.Second, "", "session-1")
	if got := body["event"]; got != "qr" {
		t.Errorf("event = %v, want qr", got)
	}
	if _, ok := body["device_id"]; ok {
		t.Errorf("device_id must be omitted before pairing, got %v", body["device_id"])
	}
	if got := body["session_id"]; got != "session-1" {
		t.Errorf("session_id = %v, want session-1", got)
	}
	assertExtraPayload(t, body, `{"code":"2@abc,def,ghi","timeout_seconds":60}`)
}

// A re-pair after LoggedOut emits a fresh sequence of codes; every one must reach the webhook.
func TestForwardLoginQRCodeForwardsEveryCode(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	config.WhatsappWebhook = []string{"https://test.com"}
	config.WhatsappWebhookEvents = []string{"qr"}
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
	}()

	forwarded := make(chan map[string]any, 2)
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(_ context.Context, payload map[string]any, _ string, _ *domainChatStorage.DeviceWebhookConfig) error {
		forwarded <- payload
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	instance := NewDeviceInstance("session-1", nil, nil)
	ForwardLoginQRCode(context.Background(), instance, "2@first", 60*time.Second)
	ForwardLoginQRCode(context.Background(), instance, "2@second", 20*time.Second)

	got := map[string]float64{}
	for range 2 {
		select {
		case body := <-forwarded:
			if body["event"] != "qr" || body["session_id"] != "session-1" {
				t.Fatalf("unexpected qr envelope: %v", body)
			}
			raw, _ := json.Marshal(body["payload"])
			var payload struct {
				Code    string  `json:"code"`
				Timeout float64 `json:"timeout_seconds"`
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("unmarshal qr payload: %v", err)
			}
			got[payload.Code] = payload.Timeout
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 2 qr codes were forwarded", len(got))
		}
	}
	if want := map[string]float64{"2@first": 60, "2@second": 20}; !reflect.DeepEqual(got, want) {
		t.Errorf("forwarded codes = %v, want %v", got, want)
	}
}

func historyTestTextMessage(chat, id, text string, fromMe bool, ts uint64) *waHistorySync.HistorySyncMsg {
	return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chat), FromMe: proto.Bool(fromMe), ID: proto.String(id)},
		MessageTimestamp: proto.Uint64(ts),
		Message:          &waE2E.Message{Conversation: proto.String(text)},
	}}
}

func TestHistorySyncPayloadsReuseMessagePayload(t *testing.T) {
	client := extraTestClient()
	chat := "5541988887777@s.whatsapp.net"
	inbound := historyTestTextMessage(chat, "3A1", "oi", false, 1761134400)
	outbound := historyTestTextMessage(chat, "3A2", "tudo bem?", true, 1761134460)
	reaction := &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chat), ID: proto.String("3A3")},
		MessageTimestamp: proto.Uint64(1761134470),
		Message:          &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("ok"), Key: &waCommon.MessageKey{ID: proto.String("3A1")}}},
	}}
	revoke := &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chat), ID: proto.String("3A4")},
		MessageTimestamp: proto.Uint64(1761134480),
		Message:          &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String("3A2")}}},
	}}
	stub := &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chat), ID: proto.String("3A5")},
		MessageTimestamp: proto.Uint64(1761134490),
	}}
	unparsable := &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:     &waCommon.MessageKey{RemoteJID: proto.String("120363402106000000@g.us"), ID: proto.String("3A6")},
		Message: &waE2E.Message{Conversation: proto.String("sem remetente")},
	}}

	evt := &events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_RECENT.Enum(),
		Progress: proto.Uint32(42),
		Conversations: []*waHistorySync.Conversation{
			{ID: proto.String(chat), Name: proto.String("Maria"), Messages: []*waHistorySync.HistorySyncMsg{inbound, reaction, outbound, revoke, stub}},
			{ID: proto.String("120363402106000000@g.us"), Messages: []*waHistorySync.HistorySyncMsg{unparsable}},
		},
	}}

	bodies := historySyncPayloads(context.Background(), evt, extraTestDeviceID, client)
	if len(bodies) != 1 {
		t.Fatalf("got %d history.sync bodies, want 1", len(bodies))
	}

	var want []map[string]any
	for _, msg := range []*waHistorySync.HistorySyncMsg{inbound, outbound} {
		parsed, err := client.ParseWebMessage(types.NewJID("5541988887777", types.DefaultUserServer), msg.GetMessage())
		if err != nil {
			t.Fatalf("parse web message: %v", err)
		}
		eventType, payload, err := buildEventPayload(context.Background(), client, parsed)
		if err != nil || eventType != EventTypeMessage {
			t.Fatalf("buildEventPayload = %s, %v", eventType, err)
		}
		want = append(want, payload)
	}
	wantJSON, _ := json.Marshal(map[string]any{
		"sync_type":     "RECENT",
		"progress":      42,
		"conversations": []map[string]any{{"chat_id": chat, "name": "Maria", "messages": want}},
	})

	if got := bodies[0]["event"]; got != "history.sync" {
		t.Errorf("event = %v, want history.sync", got)
	}
	if got := bodies[0]["device_id"]; got != extraTestDeviceID {
		t.Errorf("device_id = %v, want %s", got, extraTestDeviceID)
	}
	assertExtraPayload(t, bodies[0], string(wantJSON))
	messages := bodies[0]["payload"].(map[string]any)["conversations"].([]map[string]any)[0]["messages"].([]map[string]any)
	if messages[0]["body"] != "oi" || messages[1]["is_from_me"] != true {
		t.Errorf("messages are not the inbound and outbound texts: %v", messages)
	}
}

func historyTestConversation(chatID, name string, count, textSize int) historySyncConversation {
	conv := historySyncConversation{chatID: chatID, name: name}
	text := strings.Repeat("a", textSize)
	for i := range count {
		conv.messages = append(conv.messages, map[string]any{"id": fmt.Sprintf("%s-%04d", chatID, i), "chat_id": chatID, "body": text})
	}
	return conv
}

func historyIDs(convs ...historySyncConversation) []string {
	var ids []string
	for _, conv := range convs {
		for _, msg := range conv.messages {
			ids = append(ids, msg["id"].(string))
		}
	}
	return ids
}

func historyBodyConversations(body map[string]any) []map[string]any {
	return body["payload"].(map[string]any)["conversations"].([]map[string]any)
}

func historyConversationMessages(conv map[string]any) []map[string]any {
	return conv["messages"].([]map[string]any)
}

func assertHistoryBodies(t *testing.T, bodies []map[string]any, wantIDs []string) {
	t.Helper()
	var gotIDs []string
	for i, body := range bodies {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body %d: %v", i, err)
		}
		count := 0
		for _, conv := range historyBodyConversations(body) {
			for _, msg := range historyConversationMessages(conv) {
				if msg["chat_id"] != conv["chat_id"] {
					t.Errorf("message %v filed under conversation %v", msg["id"], conv["chat_id"])
				}
				gotIDs = append(gotIDs, msg["id"].(string))
				count++
			}
		}
		if count > 1 && len(raw) >= historySyncMaxBodyBytes {
			t.Errorf("body %d has %d bytes, limit is %d", i, len(raw), historySyncMaxBodyBytes)
		}
		if count > historySyncMaxMessages {
			t.Errorf("body %d has %d messages, limit is %d", i, count, historySyncMaxMessages)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("messages were lost or reordered: got %d ids, want %d", len(gotIDs), len(wantIDs))
	}
}

func TestHistorySyncBodiesSplitByMessageCount(t *testing.T) {
	conv := historyTestConversation("5541988887777@s.whatsapp.net", "Maria", 1200, 10)
	bodies := historySyncBodies(extraTestDeviceID, "", "INITIAL_BOOTSTRAP", nil, []historySyncConversation{conv}, extraTestTime)
	if len(bodies) != 3 {
		t.Fatalf("got %d bodies, want 3", len(bodies))
	}
	assertHistoryBodies(t, bodies, historyIDs(conv))
	for i, body := range bodies {
		if _, ok := body["payload"].(map[string]any)["progress"]; ok {
			t.Errorf("body %d has progress although the sync had none", i)
		}
		convs := historyBodyConversations(body)
		if len(convs) != 1 || convs[0]["chat_id"] != conv.chatID || convs[0]["name"] != "Maria" {
			t.Errorf("body %d conversations = %v", i, convs)
		}
	}
}

func TestHistorySyncBodiesSplitConversationAcrossEvents(t *testing.T) {
	first := historyTestConversation("5541988887777@s.whatsapp.net", "Maria", 300, 10)
	second := historyTestConversation("120363402106000000@g.us", "", 300, 10)
	bodies := historySyncBodies(extraTestDeviceID, "", "RECENT", proto.Uint32(10), []historySyncConversation{first, second}, extraTestTime)
	if len(bodies) != 2 {
		t.Fatalf("got %d bodies, want 2", len(bodies))
	}
	assertHistoryBodies(t, bodies, historyIDs(first, second))
	firstConvs := historyBodyConversations(bodies[0])
	secondConvs := historyBodyConversations(bodies[1])
	if len(firstConvs) != 2 || len(historyConversationMessages(firstConvs[1])) != 200 {
		t.Errorf("first body should end with 200 messages of the second chat")
	}
	if len(secondConvs) != 1 || secondConvs[0]["chat_id"] != second.chatID || len(historyConversationMessages(secondConvs[0])) != 100 {
		t.Errorf("second body should carry the remaining 100 messages of the second chat")
	}
	if _, ok := secondConvs[0]["name"]; ok {
		t.Errorf("empty conversation name must be omitted")
	}
	if got := bodies[1]["payload"].(map[string]any)["progress"]; got != uint32(10) {
		t.Errorf("progress = %v, want 10 in every body", got)
	}
}

func TestHistorySyncBodiesSplitBySize(t *testing.T) {
	conv := historyTestConversation("5541988887777@s.whatsapp.net", "Maria", 50, 30*1024)
	// Oversized on purpose: an unmeasured session_id would push the fullest body over the limit.
	sessionID := strings.Repeat("s", 60_000)
	bodies := historySyncBodies(extraTestDeviceID, sessionID, "RECENT", nil, []historySyncConversation{conv}, extraTestTime)
	if len(bodies) < 2 {
		t.Fatalf("got %d bodies, want the 1.5 MB history split by size", len(bodies))
	}
	assertHistoryBodies(t, bodies, historyIDs(conv))
	for i, body := range bodies {
		if body["session_id"] != sessionID {
			t.Errorf("body %d has no session_id", i)
		}
		raw, _ := json.Marshal(body)
		if len(raw) >= historySyncMaxBodyBytes {
			t.Errorf("body %d has %d bytes, limit is %d", i, len(raw), historySyncMaxBodyBytes)
		}
	}
}

func TestHistorySyncBodiesOversizedMessageGoesAlone(t *testing.T) {
	conv := historyTestConversation("5541988887777@s.whatsapp.net", "Maria", 3, 10)
	conv.messages[1]["body"] = strings.Repeat("a", historySyncMaxBodyBytes)
	bodies := historySyncBodies(extraTestDeviceID, "", "RECENT", nil, []historySyncConversation{conv}, extraTestTime)
	if len(bodies) != 3 {
		t.Fatalf("got %d bodies, want 3", len(bodies))
	}
	assertHistoryBodies(t, bodies, historyIDs(conv))
	middle := historyBodyConversations(bodies[1])
	if len(middle) != 1 || len(historyConversationMessages(middle[0])) != 1 || historyConversationMessages(middle[0])[0]["id"] != conv.messages[1]["id"] {
		t.Errorf("oversized message should travel alone")
	}
}

func TestHistorySyncBodiesEmpty(t *testing.T) {
	if bodies := historySyncBodies(extraTestDeviceID, "", "PUSH_NAME", nil, nil, extraTestTime); len(bodies) != 0 {
		t.Errorf("got %d bodies for a sync without messages, want 0", len(bodies))
	}
}

func TestHistorySyncPayloadsSkipIgnoredChats(t *testing.T) {
	originalIgnore := config.WhatsappWebhookIgnoreJids
	config.WhatsappWebhookIgnoreJids = []string{"@g.us"}
	defer func() { config.WhatsappWebhookIgnoreJids = originalIgnore }()

	chat := "5541988887777@s.whatsapp.net"
	group := "120363402106000000@g.us"
	evt := &events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_RECENT.Enum(),
		Conversations: []*waHistorySync.Conversation{
			{ID: proto.String(group), Messages: []*waHistorySync.HistorySyncMsg{historyTestTextMessage(group, "3B1", "grupo", true, 1761134400)}},
			{ID: proto.String(chat), Messages: []*waHistorySync.HistorySyncMsg{historyTestTextMessage(chat, "3B2", "direto", true, 1761134400)}},
		},
	}}

	bodies := historySyncPayloads(context.Background(), evt, extraTestDeviceID, extraTestClient())
	if len(bodies) != 1 {
		t.Fatalf("got %d bodies, want 1", len(bodies))
	}
	convs := historyBodyConversations(bodies[0])
	if len(convs) != 1 || convs[0]["chat_id"] != chat {
		t.Errorf("conversations = %v, want only %s", convs, chat)
	}
}

func TestHistorySyncPayloadsSkipBroadcastChats(t *testing.T) {
	status := "status@broadcast"
	chat := "5541988887777@s.whatsapp.net"
	statusPost := &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(status), ID: proto.String("3C1")},
		Participant:      proto.String(chat),
		MessageTimestamp: proto.Uint64(1761134400),
		Message:          &waE2E.Message{Conversation: proto.String("status")},
	}}
	evt := &events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_RECENT.Enum(),
		Conversations: []*waHistorySync.Conversation{
			{ID: proto.String(status), Messages: []*waHistorySync.HistorySyncMsg{statusPost}},
			{ID: proto.String(chat), Messages: []*waHistorySync.HistorySyncMsg{historyTestTextMessage(chat, "3C2", "direto", false, 1761134400)}},
		},
	}}

	bodies := historySyncPayloads(context.Background(), evt, extraTestDeviceID, extraTestClient())
	if len(bodies) != 1 {
		t.Fatalf("got %d bodies, want 1", len(bodies))
	}
	convs := historyBodyConversations(bodies[0])
	if len(convs) != 1 || convs[0]["chat_id"] != chat {
		t.Errorf("conversations = %v, want only %s", convs, chat)
	}
}

func TestHistorySyncPayloadsSkipIgnoredSenders(t *testing.T) {
	originalIgnore := config.WhatsappWebhookIgnoreJids
	config.WhatsappWebhookIgnoreJids = []string{"5541977776666@s.whatsapp.net"}
	defer func() { config.WhatsappWebhookIgnoreJids = originalIgnore }()

	group := "120363402106000000@g.us"
	groupMessage := func(id, participant string) *waHistorySync.HistorySyncMsg {
		return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:              &waCommon.MessageKey{RemoteJID: proto.String(group), ID: proto.String(id)},
			Participant:      proto.String(participant),
			MessageTimestamp: proto.Uint64(1761134400),
			Message:          &waE2E.Message{Conversation: proto.String("oi")},
		}}
	}
	evt := &events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_RECENT.Enum(),
		Conversations: []*waHistorySync.Conversation{{
			ID:       proto.String(group),
			Messages: []*waHistorySync.HistorySyncMsg{groupMessage("3D1", "5541977776666@s.whatsapp.net"), groupMessage("3D2", "5541988887777@s.whatsapp.net")},
		}},
	}}

	bodies := historySyncPayloads(context.Background(), evt, extraTestDeviceID, extraTestClient())
	if len(bodies) != 1 {
		t.Fatalf("got %d bodies, want 1", len(bodies))
	}
	messages := historyConversationMessages(historyBodyConversations(bodies[0])[0])
	if len(messages) != 1 || messages[0]["id"] != "3D2" {
		t.Errorf("messages = %v, want only 3D2", messages)
	}
}

func TestHasWebhookTargetForEvent(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	originalMerge := config.WhatsappWebhookDeviceMergeGlobal
	originalStorage := webhookStorageForTest
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
		config.WhatsappWebhookDeviceMergeGlobal = originalMerge
		webhookStorageForTest = originalStorage
	}()

	deviceURL := "https://device.test/hook"
	deviceRecord := func(events string) func(string) (*domainChatStorage.DeviceRecord, error) {
		return func(string) (*domainChatStorage.DeviceRecord, error) {
			return &domainChatStorage.DeviceRecord{WebhookURL: &deviceURL, WebhookEvents: events}, nil
		}
	}

	tests := []struct {
		name    string
		global  []string
		events  []string
		merge   bool
		storage func(string) (*domainChatStorage.DeviceRecord, error)
		want    bool
	}{
		{name: "no webhook at all", want: false},
		{name: "global webhook without filter", global: []string{"https://global.test"}, want: true},
		{name: "global filter without history.sync", global: []string{"https://global.test"}, events: []string{"message"}, want: false},
		{name: "global filter with history.sync", global: []string{"https://global.test"}, events: []string{"message", "history.sync"}, want: true},
		{name: "device webhook takes history.sync", storage: deviceRecord("message,history.sync"), want: true},
		{name: "device webhook filters it out", global: []string{"https://global.test"}, storage: deviceRecord("message"), want: false},
		{name: "device filters it out but merged global takes it", global: []string{"https://global.test"}, merge: true, storage: deviceRecord("message"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.WhatsappWebhook = tt.global
			config.WhatsappWebhookEvents = tt.events
			config.WhatsappWebhookDeviceMergeGlobal = tt.merge
			webhookStorageForTest = tt.storage
			if webhookStorageForTest == nil {
				webhookStorageForTest = func(string) (*domainChatStorage.DeviceRecord, error) { return nil, nil }
			}
			if got := hasWebhookTargetForEvent(context.Background(), extraTestDeviceID, "history.sync"); got != tt.want {
				t.Errorf("hasWebhookTargetForEvent = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHistorySyncWebhookBodiesReturnsEarlyWithoutTarget(t *testing.T) {
	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	originalStorage := webhookStorageForTest
	webhookStorageForTest = func(string) (*domainChatStorage.DeviceRecord, error) { return nil, nil }
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
		webhookStorageForTest = originalStorage
	}()

	chat := "5541988887777@s.whatsapp.net"
	evt := &events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType:      waHistorySync.HistorySync_RECENT.Enum(),
		Conversations: []*waHistorySync.Conversation{{ID: proto.String(chat), Messages: []*waHistorySync.HistorySyncMsg{historyTestTextMessage(chat, "3E1", "oi", false, 1761134400)}}},
	}}

	config.WhatsappWebhook = []string{"https://global.test"}
	config.WhatsappWebhookEvents = []string{"message"}
	if bodies := historySyncWebhookBodies(context.Background(), evt, extraTestDeviceID, extraTestClient()); bodies != nil {
		t.Errorf("built %d bodies although no webhook takes history.sync", len(bodies))
	}

	config.WhatsappWebhookEvents = []string{"history.sync"}
	if bodies := historySyncWebhookBodies(context.Background(), evt, extraTestDeviceID, extraTestClient()); len(bodies) != 1 {
		t.Errorf("got %d bodies with a subscribed webhook, want 1", len(bodies))
	}
}
