package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder remembers what each usecase method was asked.
type recorder struct{ calls []string }

func (r *recorder) add(s string) { r.calls = append(r.calls, s) }

type recUser struct {
	domainUser.IUserUsecase
	*recorder
}

func (u recUser) Info(_ context.Context, r domainUser.InfoRequest) (domainUser.InfoResponse, error) {
	u.add("info " + r.Phone)
	return domainUser.InfoResponse{}, nil
}
func (u recUser) Avatar(_ context.Context, r domainUser.AvatarRequest) (domainUser.AvatarResponse, error) {
	u.add("avatar " + r.Phone)
	return domainUser.AvatarResponse{URL: "https://pps.example/a.jpg"}, nil
}
func (u recUser) IsOnWhatsApp(_ context.Context, r domainUser.CheckRequest) (domainUser.CheckResponse, error) {
	u.add("check " + r.Phone)
	return domainUser.CheckResponse{IsOnWhatsApp: true}, nil
}
func (u recUser) BusinessProfile(_ context.Context, r domainUser.BusinessProfileRequest) (domainUser.BusinessProfileResponse, error) {
	u.add("business " + r.Phone)
	return domainUser.BusinessProfileResponse{}, nil
}
func (u recUser) MyPrivacySetting(context.Context) (domainUser.MyPrivacySettingResponse, error) {
	u.add("privacy")
	return domainUser.MyPrivacySettingResponse{}, nil
}
func (u recUser) MyListGroups(context.Context) (domainUser.MyListGroupsResponse, error) {
	u.add("groups")
	return domainUser.MyListGroupsResponse{}, nil
}
func (u recUser) MyListNewsletter(context.Context) (domainUser.MyListNewsletterResponse, error) {
	u.add("newsletters")
	return domainUser.MyListNewsletterResponse{}, nil
}
func (u recUser) ChangePushName(_ context.Context, r domainUser.ChangePushNameRequest) error {
	u.add("pushname " + r.PushName)
	return nil
}

type recChat struct {
	domainChat.IChatUsecase
	*recorder
}

func (c recChat) PinChat(_ context.Context, r domainChat.PinChatRequest) (domainChat.PinChatResponse, error) {
	c.add("pin " + r.ChatJID + " " + map[bool]string{true: "on", false: "off"}[r.Pinned])
	return domainChat.PinChatResponse{Message: "pinned"}, nil
}
func (c recChat) SetDisappearingTimer(_ context.Context, r domainChat.SetDisappearingTimerRequest) (domainChat.SetDisappearingTimerResponse, error) {
	c.add("timer " + r.ChatJID)
	return domainChat.SetDisappearingTimerResponse{Message: "set", TimerSeconds: r.TimerSeconds}, nil
}
func (c recChat) RequestChatHistory(_ context.Context, r domainChat.RequestChatHistoryRequest) (domainChat.RequestChatHistoryResponse, error) {
	c.add("history " + r.ChatJID)
	return domainChat.RequestChatHistoryResponse{Status: "requested", RequestedCount: r.Count}, nil
}

type recSend struct {
	domainSend.ISendUsecase
	*recorder
}

func (s recSend) SendPresence(_ context.Context, r domainSend.PresenceRequest) (domainSend.GenericResponse, error) {
	s.add("presence " + r.Type)
	return domainSend.GenericResponse{Status: "ok"}, nil
}
func (s recSend) SendChatPresence(_ context.Context, r domainSend.ChatPresenceRequest) (domainSend.GenericResponse, error) {
	s.add("typing " + r.Phone + " " + r.Action)
	return domainSend.GenericResponse{Status: "ok"}, nil
}

type recCall struct {
	domainCall.ICallUsecase
	*recorder
}

func (c recCall) RejectCall(_ context.Context, jid, id string) error {
	c.add("reject " + jid + " " + id)
	return nil
}

type recNews struct {
	domainNewsletter.INewsletterUsecase
	*recorder
}

func (n recNews) GetMessages(_ context.Context, r domainNewsletter.GetMessagesRequest) (domainNewsletter.GetMessagesResponse, error) {
	n.add("messages " + r.NewsletterID)
	return domainNewsletter.GetMessagesResponse{}, nil
}
func (n recNews) DownloadMedia(_ context.Context, r domainNewsletter.DownloadMediaRequest) (domainNewsletter.DownloadMediaResponse, error) {
	n.add("download " + r.NewsletterID)
	return domainNewsletter.DownloadMediaResponse{ServerID: r.ServerID, Status: "saved"}, nil
}
func (n recNews) Unfollow(_ context.Context, r domainNewsletter.UnfollowRequest) error {
	n.add("unfollow " + r.NewsletterID)
	return nil
}

type recGroup struct {
	domainGroup.IGroupUsecase
	*recorder
}

func (g recGroup) GetGroupInfoFromLink(_ context.Context, r domainGroup.GetGroupInfoFromLinkRequest) (domainGroup.GetGroupInfoFromLinkResponse, error) {
	g.add("preview " + r.Link)
	return domainGroup.GetGroupInfoFromLinkResponse{Name: "Team"}, nil
}

func wtfDeps() (*recorder, Deps) {
	r := &recorder{}
	return r, Deps{User: recUser{recorder: r}, Chat: recChat{recorder: r}, Send: recSend{recorder: r},
		Call: recCall{recorder: r}, Newsletter: recNews{recorder: r}, Group: recGroup{recorder: r}}
}

// Behaviour: each tool and action reaches the usecase method its REST route
// calls, with the request the REST route would build.
func TestWTFToolsCallTheRESTUsecases(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{ToolContactInfo, map[string]any{"action": "info", "phone": "27825550199"}, "info 27825550199@s.whatsapp.net"},
		{ToolContactInfo, map[string]any{"action": "avatar", "phone": "27825550199"}, "avatar 27825550199@s.whatsapp.net"},
		{ToolContactInfo, map[string]any{"action": "check", "phone": "27825550199"}, "check 27825550199"},
		{ToolContactInfo, map[string]any{"action": "business_profile", "phone": "27825550199"}, "business 27825550199@s.whatsapp.net"},
		{ToolMyWhatsApp, map[string]any{"action": "privacy"}, "privacy"},
		{ToolMyWhatsApp, map[string]any{"action": "groups"}, "groups"},
		{ToolMyWhatsApp, map[string]any{"action": "newsletters"}, "newsletters"},
		{ToolSetPushName, map[string]any{"push_name": "Ari"}, "pushname Ari"},
		{ToolChatSettings, map[string]any{"action": "pin", "chat_jid": "1@s.whatsapp.net", "pinned": true}, "pin 1@s.whatsapp.net on"},
		{ToolChatSettings, map[string]any{"action": "disappearing", "chat_jid": "1@s.whatsapp.net", "timer_seconds": 86400}, "timer 1@s.whatsapp.net"},
		{ToolChatSettings, map[string]any{"action": "request_history", "chat_jid": "1@s.whatsapp.net"}, "history 1@s.whatsapp.net"},
		{ToolPresence, map[string]any{"action": "presence", "type": "available"}, "presence available"},
		{ToolPresence, map[string]any{"action": "chat_presence", "phone": "27825550199", "state": "start"}, "typing 27825550199@s.whatsapp.net start"},
		{ToolRejectCall, map[string]any{"caller_jid": "1@s.whatsapp.net", "call_id": "C1"}, "reject 1@s.whatsapp.net C1"},
		{ToolNewsletter, map[string]any{"action": "messages", "newsletter_id": "9@newsletter"}, "messages 9@newsletter"},
		{ToolNewsletter, map[string]any{"action": "download_media", "newsletter_id": "9@newsletter", "server_id": 3}, "download 9@newsletter"},
		{ToolNewsletter, map[string]any{"action": "unfollow", "newsletter_id": "9@newsletter"}, "unfollow 9@newsletter"},
		{ToolGroupFromLink, map[string]any{"link": "https://chat.whatsapp.com/AbC"}, "preview https://chat.whatsapp.com/AbC"},
	}
	for _, tc := range cases {
		t.Run(tc.tool+" "+tc.want, func(t *testing.T) {
			rec, deps := wtfDeps()
			s := NewServer(deps, &stubResolver{})
			tool := s.GetTool(tc.tool)
			require.NotNil(t, tool, tc.tool)
			res, err := tool.Handler(deviceCtx(), callReq(tc.args))
			require.NoError(t, err)
			require.False(t, res.IsError, "%v", res.Content)
			assert.Equal(t, []string{tc.want}, rec.calls)
		})
	}
}

// Behaviour: with no device to act as, every WTF tool refuses before any
// usecase runs.
func TestWTFToolsNeedADevice(t *testing.T) {
	rec, deps := wtfDeps()
	s := NewServer(deps, &stubResolver{})
	for _, name := range []string{ToolContactInfo, ToolMyWhatsApp, ToolSetPushName, ToolChatSettings, ToolPresence, ToolRejectCall, ToolNewsletter, ToolGroupFromLink} {
		res, err := s.GetTool(name).Handler(context.Background(), callReq(map[string]any{"action": "x"}))
		require.NoError(t, err)
		assert.True(t, res.IsError, name)
	}
	assert.Empty(t, rec.calls)
}

// Shape: every WTF tool says what it does for an AI caller, has a title and
// all three hints set, and its schema takes device_id.
func TestWTFToolsShape(t *testing.T) {
	_, deps := wtfDeps()
	s := NewServer(deps, &stubResolver{})
	n := 0
	for name, st := range s.ListTools() {
		if !strings.HasPrefix(name, "wtf_") {
			continue
		}
		n++
		tool := st.Tool
		assert.GreaterOrEqual(t, len(tool.Description), 60, name)
		assert.NotEmpty(t, tool.Annotations.Title, name)
		require.NotNil(t, tool.Annotations.ReadOnlyHint, name)
		require.NotNil(t, tool.Annotations.DestructiveHint, name)
		require.NotNil(t, tool.Annotations.IdempotentHint, name)
		if *tool.Annotations.ReadOnlyHint {
			assert.False(t, *tool.Annotations.DestructiveHint, name)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(tool.RawInputSchema, &schema), name)
		assert.Contains(t, schema.Properties, "device_id", name)
	}
	assert.Equal(t, 8, n)
}
