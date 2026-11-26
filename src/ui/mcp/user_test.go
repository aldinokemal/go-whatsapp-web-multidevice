package mcp

import (
	"context"
	"errors"
	"testing"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

// stubUserToolService records the last call; it also implements the optional
// IUserContactsDetailed capability.
type stubUserToolService struct {
	domainUser.IUserUsecase
	called   string
	phone    string
	avatar   domainUser.AvatarRequest
	pushName string
	ret      any // response the last call returned
	err      error
}

func (s *stubUserToolService) record(name, phone string) { s.called, s.phone = name, phone }

func (s *stubUserToolService) Info(_ context.Context, r domainUser.InfoRequest) (domainUser.InfoResponse, error) {
	s.record("info", r.Phone)
	r2 := domainUser.InfoResponse{ResolvedPhone: "628123"}
	s.ret = r2
	return r2, s.err
}
func (s *stubUserToolService) IsOnWhatsApp(_ context.Context, r domainUser.CheckRequest) (domainUser.CheckResponse, error) {
	s.record("check", r.Phone)
	r2 := domainUser.CheckResponse{IsOnWhatsApp: true}
	s.ret = r2
	return r2, s.err
}
func (s *stubUserToolService) BusinessProfile(_ context.Context, r domainUser.BusinessProfileRequest) (domainUser.BusinessProfileResponse, error) {
	s.record("business_profile", r.Phone)
	r2 := domainUser.BusinessProfileResponse{JID: r.Phone, Email: "biz@example.com"}
	s.ret = r2
	return r2, s.err
}
func (s *stubUserToolService) Avatar(_ context.Context, r domainUser.AvatarRequest) (domainUser.AvatarResponse, error) {
	s.record("avatar", r.Phone)
	s.avatar = r
	r2 := domainUser.AvatarResponse{URL: "https://pps.whatsapp.net/x"}
	s.ret = r2
	return r2, s.err
}
func (s *stubUserToolService) SubscribePresence(_ context.Context, r domainUser.SubscribePresenceRequest) (domainUser.SubscribePresenceResponse, error) {
	s.record("subscribe_presence", r.Phone)
	r2 := domainUser.SubscribePresenceResponse{Subscribed: true, JID: r.Phone}
	s.ret = r2
	return r2, s.err
}
func (s *stubUserToolService) ChangePushName(_ context.Context, r domainUser.ChangePushNameRequest) error {
	s.record("set_push_name", "")
	s.pushName = r.PushName
	return s.err
}
func (s *stubUserToolService) MyPrivacySetting(context.Context) (domainUser.MyPrivacySettingResponse, error) {
	s.record("my_privacy", "")
	r := domainUser.MyPrivacySettingResponse{LastSeen: "contacts"}
	s.ret = r
	return r, s.err
}
func (s *stubUserToolService) MyListGroups(context.Context) (domainUser.MyListGroupsResponse, error) {
	s.record("my_groups", "")
	r := domainUser.MyListGroupsResponse{Data: []types.GroupInfo{{JID: types.NewJID("123", types.GroupServer)}}}
	s.ret = r
	return r, s.err
}
func (s *stubUserToolService) MyListNewsletter(context.Context) (domainUser.MyListNewsletterResponse, error) {
	s.record("my_newsletters", "")
	r := domainUser.MyListNewsletterResponse{Data: []types.NewsletterMetadata{{ID: types.NewJID("1", types.NewsletterServer)}}}
	s.ret = r
	return r, s.err
}
func (s *stubUserToolService) MyListContactsDetailed(context.Context) (domainUser.MyListContactsDetailedResponse, error) {
	s.record("my_contacts_detailed", "")
	r := domainUser.MyListContactsDetailedResponse{Data: []domainUser.MyListContactsDetailedData{{JID: "628999@s.whatsapp.net", FullName: "Bob"}}}
	s.ret = r
	return r, s.err
}

func TestHandleUserDispatch(t *testing.T) {
	t.Run("phone actions get a sanitized phone and a structured result", func(t *testing.T) {
		for _, action := range []string{"info", "business_profile", "avatar", "subscribe_presence"} {
			svc := &stubUserToolService{}
			res, err := InitMcpUser(svc, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": action, "phone": " 628123 "}))
			require.NoError(t, err)
			assert.False(t, res.IsError, action)
			assert.Equal(t, action, svc.called)
			assert.Equal(t, "628123@s.whatsapp.net", svc.phone, action)
			require.NotNil(t, svc.ret, action)
			assert.Equal(t, svc.ret, res.StructuredContent, action)
		}
	})

	t.Run("check passes the number to the registration lookup without inferring a group", func(t *testing.T) {
		for _, phone := range []string{"628123", "1234567890123456"} {
			svc := &stubUserToolService{}
			res, err := InitMcpUser(svc, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": "check", "phone": " " + phone + " "}))
			require.NoError(t, err)
			require.False(t, res.IsError)
			require.Equal(t, "check", svc.called)
			require.Equal(t, phone, svc.phone)
			require.Equal(t, svc.ret, res.StructuredContent)
		}
	})

	t.Run("avatar passes flags", func(t *testing.T) {
		svc := &stubUserToolService{}
		_, err := InitMcpUser(svc, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{
			"action": "avatar", "phone": "120363025246125486@g.us", "is_preview": true, "is_community": true,
		}))
		require.NoError(t, err)
		assert.Equal(t, domainUser.AvatarRequest{Phone: "120363025246125486@g.us", IsPreview: true, IsCommunity: true}, svc.avatar)
	})

	t.Run("own-account listings", func(t *testing.T) {
		for _, action := range []string{"my_privacy", "my_groups", "my_newsletters", "my_contacts_detailed"} {
			svc := &stubUserToolService{}
			res, err := InitMcpUser(svc, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": action}))
			require.NoError(t, err)
			assert.False(t, res.IsError, action)
			assert.Equal(t, action, svc.called)
			require.NotNil(t, svc.ret, action)
			assert.Equal(t, svc.ret, res.StructuredContent, action)
		}
	})

	t.Run("my_contacts_detailed returns split names", func(t *testing.T) {
		res, err := InitMcpUser(&stubUserToolService{}, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": "my_contacts_detailed"}))
		require.NoError(t, err)
		assert.Contains(t, resultText(t, res), `"full_name":"Bob"`)
	})

	t.Run("my_contacts_detailed without the capability errors", func(t *testing.T) {
		res, err := InitMcpUser(&stubUserService{}, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": "my_contacts_detailed"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, resultText(t, res), "not supported")
	})

	t.Run("set_push_name", func(t *testing.T) {
		svc := &stubUserToolService{}
		res, err := InitMcpUser(svc, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": "set_push_name", "push_name": "Aldo"}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.Equal(t, "Aldo", svc.pushName)
		assert.Contains(t, resultText(t, res), "Aldo")
	})

	t.Run("usecase errors surface", func(t *testing.T) {
		for _, args := range []map[string]any{
			{"action": "info", "phone": "628"},
			{"action": "set_push_name", "push_name": "x"},
			{"action": "my_groups"},
		} {
			res, err := InitMcpUser(&stubUserToolService{err: errors.New("boom")}, &stubResolver{}).handleUser(deviceCtx(), callReq(args))
			require.NoError(t, err)
			assert.True(t, res.IsError, args["action"])
			assert.Equal(t, "boom", resultText(t, res))
		}
	})

	t.Run("no device errors before any call", func(t *testing.T) {
		svc := &stubUserToolService{}
		res, err := InitMcpUser(svc, &stubResolver{}).handleUser(context.Background(), callReq(map[string]any{"action": "my_privacy"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Empty(t, svc.called)
	})

	t.Run("unknown action", func(t *testing.T) {
		res, err := InitMcpUser(&stubUserToolService{}, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": "change_avatar"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})
}
