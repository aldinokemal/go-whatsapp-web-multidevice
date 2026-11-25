package mcp

import (
	"context"
	"errors"
	"testing"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubUserToolService records the last call; it also implements the optional
// IUserContactsDetailed capability.
type stubUserToolService struct {
	domainUser.IUserUsecase
	called   string
	phone    string
	avatar   domainUser.AvatarRequest
	pushName string
	err      error
}

func (s *stubUserToolService) record(name, phone string) { s.called, s.phone = name, phone }

func (s *stubUserToolService) Info(_ context.Context, r domainUser.InfoRequest) (domainUser.InfoResponse, error) {
	s.record("info", r.Phone)
	return domainUser.InfoResponse{ResolvedPhone: "628123"}, s.err
}
func (s *stubUserToolService) IsOnWhatsApp(_ context.Context, r domainUser.CheckRequest) (domainUser.CheckResponse, error) {
	s.record("check", r.Phone)
	return domainUser.CheckResponse{IsOnWhatsApp: true}, s.err
}
func (s *stubUserToolService) BusinessProfile(_ context.Context, r domainUser.BusinessProfileRequest) (domainUser.BusinessProfileResponse, error) {
	s.record("business_profile", r.Phone)
	return domainUser.BusinessProfileResponse{JID: r.Phone}, s.err
}
func (s *stubUserToolService) Avatar(_ context.Context, r domainUser.AvatarRequest) (domainUser.AvatarResponse, error) {
	s.record("avatar", r.Phone)
	s.avatar = r
	return domainUser.AvatarResponse{URL: "https://pps.whatsapp.net/x"}, s.err
}
func (s *stubUserToolService) SubscribePresence(_ context.Context, r domainUser.SubscribePresenceRequest) (domainUser.SubscribePresenceResponse, error) {
	s.record("subscribe_presence", r.Phone)
	return domainUser.SubscribePresenceResponse{Subscribed: true, JID: r.Phone}, s.err
}
func (s *stubUserToolService) ChangePushName(_ context.Context, r domainUser.ChangePushNameRequest) error {
	s.record("set_push_name", "")
	s.pushName = r.PushName
	return s.err
}
func (s *stubUserToolService) MyPrivacySetting(context.Context) (domainUser.MyPrivacySettingResponse, error) {
	s.record("my_privacy", "")
	return domainUser.MyPrivacySettingResponse{LastSeen: "contacts"}, s.err
}
func (s *stubUserToolService) MyListGroups(context.Context) (domainUser.MyListGroupsResponse, error) {
	s.record("my_groups", "")
	return domainUser.MyListGroupsResponse{}, s.err
}
func (s *stubUserToolService) MyListNewsletter(context.Context) (domainUser.MyListNewsletterResponse, error) {
	s.record("my_newsletters", "")
	return domainUser.MyListNewsletterResponse{}, s.err
}
func (s *stubUserToolService) MyListContactsDetailed(context.Context) (domainUser.MyListContactsDetailedResponse, error) {
	s.record("my_contacts_detailed", "")
	return domainUser.MyListContactsDetailedResponse{Data: []domainUser.MyListContactsDetailedData{{JID: "628999@s.whatsapp.net", FullName: "Bob"}}}, s.err
}

func TestHandleUserDispatch(t *testing.T) {
	t.Run("phone actions get a sanitized phone and a structured result", func(t *testing.T) {
		for _, action := range []string{"info", "check", "business_profile", "avatar", "subscribe_presence"} {
			svc := &stubUserToolService{}
			res, err := InitMcpUser(svc, &stubResolver{}).handleUser(deviceCtx(), callReq(map[string]any{"action": action, "phone": " 628123 "}))
			require.NoError(t, err)
			assert.False(t, res.IsError, action)
			assert.Equal(t, action, svc.called)
			assert.Equal(t, "628123@s.whatsapp.net", svc.phone, action)
			assert.NotNil(t, res.StructuredContent, action)
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
			assert.NotNil(t, res.StructuredContent, action)
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
