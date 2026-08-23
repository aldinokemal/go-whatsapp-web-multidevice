package mcp

import (
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func compileSchema(t *testing.T, raw string) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(raw))
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	require.NoError(t, c.AddResource("schema.json", doc))
	s, err := c.Compile("schema.json")
	require.NoError(t, err)
	return s
}

func validate(t *testing.T, s *jsonschema.Schema, payload string) error {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(payload))
	require.NoError(t, err)
	return s.Validate(v)
}

func TestSchemas(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		payload string
		wantErr bool
	}{
		// ---- whatsapp_send ----
		{"send text ok", sendSchema, `{"type":"text","phone":"628","message":"hi"}`, false},
		{"send text missing message", sendSchema, `{"type":"text","phone":"628"}`, true},
		{"send missing phone", sendSchema, `{"type":"text","message":"hi"}`, true},
		{"send bad type", sendSchema, `{"type":"fax","phone":"628"}`, true},
		{"send image ok", sendSchema, `{"type":"image","phone":"628","image_url":"http://x/a.png","caption":"c"}`, false},
		{"send image missing url", sendSchema, `{"type":"image","phone":"628"}`, true},
		{"send video ok", sendSchema, `{"type":"video","phone":"628","video_url":"http://x/a.mp4"}`, false},
		{"send video missing url", sendSchema, `{"type":"video","phone":"628"}`, true},
		{"send audio ok", sendSchema, `{"type":"audio","phone":"628","audio_url":"http://x/a.ogg","ptt":true}`, false},
		{"send audio missing url", sendSchema, `{"type":"audio","phone":"628"}`, true},
		{"send document ok", sendSchema, `{"type":"document","phone":"628","file_url":"http://x/a.pdf"}`, false},
		{"send document missing url", sendSchema, `{"type":"document","phone":"628"}`, true},
		{"send sticker ok", sendSchema, `{"type":"sticker","phone":"628","sticker_url":"http://x/a.png"}`, false},
		{"send sticker missing url", sendSchema, `{"type":"sticker","phone":"628"}`, true},
		{"send location ok", sendSchema, `{"type":"location","phone":"628","latitude":"-6.2","longitude":"106.8"}`, false},
		{"send location missing lng", sendSchema, `{"type":"location","phone":"628","latitude":"-6.2"}`, true},
		{"send contact ok", sendSchema, `{"type":"contact","phone":"628","contact_name":"A","contact_phone":"629"}`, false},
		{"send contact missing name", sendSchema, `{"type":"contact","phone":"628","contact_phone":"629"}`, true},
		{"send poll ok", sendSchema, `{"type":"poll","phone":"628","question":"q","options":["a","b"]}`, false},
		{"send poll one option", sendSchema, `{"type":"poll","phone":"628","question":"q","options":["a"]}`, true},
		{"send link ok", sendSchema, `{"type":"link","phone":"628","link":"http://x","caption":"c"}`, false},
		{"send link missing caption", sendSchema, `{"type":"link","phone":"628","link":"http://x"}`, true},
		{"send forward ok", sendSchema, `{"type":"forward","phone":"628","message_id":"M1"}`, false},
		{"send forward missing id", sendSchema, `{"type":"forward","phone":"628"}`, true},
		{"send with device_id", sendSchema, `{"type":"text","phone":"628","message":"hi","device_id":"dev2"}`, false},

		// ---- whatsapp_message ----
		{"msg react ok", messageSchema, `{"action":"react","phone":"628","message_id":"M1","emoji":"👍"}`, false},
		{"msg react no emoji ok (removes reaction)", messageSchema, `{"action":"react","phone":"628","message_id":"M1"}`, false},
		{"msg edit ok", messageSchema, `{"action":"edit","phone":"628","message_id":"M1","message":"new"}`, false},
		{"msg edit missing message", messageSchema, `{"action":"edit","phone":"628","message_id":"M1"}`, true},
		{"msg revoke ok", messageSchema, `{"action":"revoke","phone":"628","message_id":"M1"}`, false},
		{"msg delete ok", messageSchema, `{"action":"delete","phone":"628","message_id":"M1"}`, false},
		{"msg mark_read ok", messageSchema, `{"action":"mark_read","phone":"628","message_id":"M1"}`, false},
		{"msg mark_played ok", messageSchema, `{"action":"mark_played","phone":"628","message_id":"M1"}`, false},
		{"msg star ok", messageSchema, `{"action":"star","phone":"628","message_id":"M1"}`, false},
		{"msg unstar ok", messageSchema, `{"action":"unstar","phone":"628","message_id":"M1"}`, false},
		{"msg download_media ok", messageSchema, `{"action":"download_media","phone":"628","message_id":"M1"}`, false},
		{"msg missing message_id", messageSchema, `{"action":"react","phone":"628"}`, true},
		{"msg bad action", messageSchema, `{"action":"pin","phone":"628","message_id":"M1"}`, true},

		// ---- whatsapp_chat ----
		{"chat list_chats ok", chatSchema, `{"action":"list_chats","limit":10,"search":"bob"}`, false},
		{"chat list_contacts ok", chatSchema, `{"action":"list_contacts"}`, false},
		{"chat get_messages ok", chatSchema, `{"action":"get_messages","chat_jid":"628@s.whatsapp.net","media_only":true}`, false},
		{"chat get_messages missing jid", chatSchema, `{"action":"get_messages"}`, true},
		{"chat archive ok", chatSchema, `{"action":"archive","chat_jid":"628@s.whatsapp.net","archived":true}`, false},
		{"chat archive missing flag", chatSchema, `{"action":"archive","chat_jid":"628@s.whatsapp.net"}`, true},
		{"chat bad action", chatSchema, `{"action":"nuke"}`, true},

		// ---- whatsapp_group ----
		{"group create ok", groupSchema, `{"action":"create","title":"T","participants":["628"]}`, false},
		{"group create missing title", groupSchema, `{"action":"create"}`, true},
		{"group join ok", groupSchema, `{"action":"join_with_link","invite_link":"http://chat.whatsapp.com/x"}`, false},
		{"group join missing link", groupSchema, `{"action":"join_with_link"}`, true},
		{"group leave ok", groupSchema, `{"action":"leave","group_id":"123@g.us"}`, false},
		{"group leave missing id", groupSchema, `{"action":"leave"}`, true},
		{"group info ok", groupSchema, `{"action":"info","group_id":"123@g.us"}`, false},
		{"group participants ok", groupSchema, `{"action":"participants","group_id":"123@g.us"}`, false},
		{"group add ok", groupSchema, `{"action":"add_participants","group_id":"123@g.us","participants":["628"]}`, false},
		{"group add missing participants", groupSchema, `{"action":"add_participants","group_id":"123@g.us"}`, true},
		{"group remove ok", groupSchema, `{"action":"remove_participants","group_id":"123@g.us","participants":["628"]}`, false},
		{"group promote ok", groupSchema, `{"action":"promote","group_id":"123@g.us","participants":["628"]}`, false},
		{"group demote ok", groupSchema, `{"action":"demote","group_id":"123@g.us","participants":["628"]}`, false},
		{"group invite_link ok", groupSchema, `{"action":"invite_link","group_id":"123@g.us","reset":true}`, false},
		{"group set_name ok", groupSchema, `{"action":"set_name","group_id":"123@g.us","name":"N"}`, false},
		{"group set_name missing name", groupSchema, `{"action":"set_name","group_id":"123@g.us"}`, true},
		{"group set_topic ok", groupSchema, `{"action":"set_topic","group_id":"123@g.us","topic":"T"}`, false},
		{"group set_settings announce", groupSchema, `{"action":"set_settings","group_id":"123@g.us","announce":true}`, false},
		{"group set_settings locked", groupSchema, `{"action":"set_settings","group_id":"123@g.us","locked":false}`, false},
		{"group set_settings neither", groupSchema, `{"action":"set_settings","group_id":"123@g.us"}`, true},
		{"group join_requests ok", groupSchema, `{"action":"join_requests","group_id":"123@g.us"}`, false},
		{"group manage_join_requests ok", groupSchema, `{"action":"manage_join_requests","group_id":"123@g.us","participants":["628"],"request_action":"approve"}`, false},
		{"group manage_join_requests bad verb", groupSchema, `{"action":"manage_join_requests","group_id":"123@g.us","participants":["628"],"request_action":"ban"}`, true},
		{"group manage_join_requests missing verb", groupSchema, `{"action":"manage_join_requests","group_id":"123@g.us","participants":["628"]}`, true},

		// ---- whatsapp_app ----
		{"app status ok", appSchema, `{"action":"status"}`, false},
		{"app login_qr ok", appSchema, `{"action":"login_qr"}`, false},
		{"app login_code ok", appSchema, `{"action":"login_code","phone":"+628123"}`, false},
		{"app login_code missing phone", appSchema, `{"action":"login_code"}`, true},
		{"app logout ok", appSchema, `{"action":"logout"}`, false},
		{"app reconnect ok", appSchema, `{"action":"reconnect"}`, false},
		{"app bad action", appSchema, `{"action":"restart"}`, true},
	}

	// compile each schema once
	compiled := map[string]*jsonschema.Schema{}
	for _, raw := range []string{sendSchema, messageSchema, chatSchema, groupSchema, appSchema} {
		compiled[raw] = compileSchema(t, raw)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(t, compiled[tt.schema], tt.payload)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
