package mcp

import (
	"context"
	"testing"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
)

type stubGroupService struct {
	domainGroup.IGroupUsecase
	created      *domainGroup.CreateGroupRequest
	joined       *domainGroup.JoinGroupWithLinkRequest
	left         *domainGroup.LeaveGroupRequest
	infoReq      *domainGroup.GroupInfoRequest
	partsReq     *domainGroup.GetGroupParticipantsRequest
	managed      *domainGroup.ParticipantRequest
	inviteReq    *domainGroup.GetGroupInviteLinkRequest
	namedReq     *domainGroup.SetGroupNameRequest
	topicReq     *domainGroup.SetGroupTopicRequest
	announceReq  *domainGroup.SetGroupAnnounceRequest
	lockedReq    *domainGroup.SetGroupLockedRequest
	joinReqsReq  *domainGroup.GetGroupRequestParticipantsRequest
	managedJoins *domainGroup.GroupRequestParticipantsRequest
}

func (s *stubGroupService) CreateGroup(_ context.Context, r domainGroup.CreateGroupRequest) (string, error) {
	s.created = &r
	return "123@g.us", nil
}
func (s *stubGroupService) JoinGroupWithLink(_ context.Context, r domainGroup.JoinGroupWithLinkRequest) (string, error) {
	s.joined = &r
	return "123@g.us", nil
}
func (s *stubGroupService) LeaveGroup(_ context.Context, r domainGroup.LeaveGroupRequest) error {
	s.left = &r
	return nil
}
func (s *stubGroupService) GroupInfo(_ context.Context, r domainGroup.GroupInfoRequest) (domainGroup.GroupInfoResponse, error) {
	s.infoReq = &r
	return domainGroup.GroupInfoResponse{}, nil
}
func (s *stubGroupService) GetGroupParticipants(_ context.Context, r domainGroup.GetGroupParticipantsRequest) (domainGroup.GetGroupParticipantsResponse, error) {
	s.partsReq = &r
	return domainGroup.GetGroupParticipantsResponse{}, nil
}
func (s *stubGroupService) ManageParticipant(_ context.Context, r domainGroup.ParticipantRequest) ([]domainGroup.ParticipantStatus, error) {
	s.managed = &r
	return nil, nil
}
func (s *stubGroupService) GetGroupInviteLink(_ context.Context, r domainGroup.GetGroupInviteLinkRequest) (domainGroup.GetGroupInviteLinkResponse, error) {
	s.inviteReq = &r
	return domainGroup.GetGroupInviteLinkResponse{InviteLink: "http://x"}, nil
}
func (s *stubGroupService) SetGroupName(_ context.Context, r domainGroup.SetGroupNameRequest) error {
	s.namedReq = &r
	return nil
}
func (s *stubGroupService) SetGroupTopic(_ context.Context, r domainGroup.SetGroupTopicRequest) error {
	s.topicReq = &r
	return nil
}
func (s *stubGroupService) SetGroupAnnounce(_ context.Context, r domainGroup.SetGroupAnnounceRequest) error {
	s.announceReq = &r
	return nil
}
func (s *stubGroupService) SetGroupLocked(_ context.Context, r domainGroup.SetGroupLockedRequest) error {
	s.lockedReq = &r
	return nil
}
func (s *stubGroupService) GetGroupRequestParticipants(_ context.Context, r domainGroup.GetGroupRequestParticipantsRequest) ([]domainGroup.GetGroupRequestParticipantsResponse, error) {
	s.joinReqsReq = &r
	return nil, nil
}
func (s *stubGroupService) ManageGroupRequestParticipants(_ context.Context, r domainGroup.GroupRequestParticipantsRequest) ([]domainGroup.ParticipantStatus, error) {
	s.managedJoins = &r
	return nil, nil
}

func TestHandleGroupDispatch(t *testing.T) {
	newHandler := func() (*stubGroupService, *GroupHandler) {
		svc := &stubGroupService{}
		return svc, InitMcpGroup(svc, &stubResolver{})
	}

	t.Run("create", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "create", "title": " T ", "participants": []any{"628"},
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.created)
		assert.Equal(t, "T", svc.created.Title)
		assert.Equal(t, []string{"628"}, svc.created.Participants)
	})

	t.Run("join_with_link", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "join_with_link", "invite_link": "http://chat.whatsapp.com/x",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.joined)
	})

	t.Run("leave", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{"action": "leave", "group_id": "123@g.us"}))
		require.NoError(t, err)
		require.NotNil(t, svc.left)
	})

	t.Run("info", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{"action": "info", "group_id": "123@g.us"}))
		require.NoError(t, err)
		require.NotNil(t, svc.infoReq)
	})

	t.Run("participants", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{"action": "participants", "group_id": "123@g.us"}))
		require.NoError(t, err)
		require.NotNil(t, svc.partsReq)
	})

	t.Run("participant changes map to whatsmeow actions", func(t *testing.T) {
		cases := map[string]whatsmeow.ParticipantChange{
			"add_participants":    whatsmeow.ParticipantChangeAdd,
			"remove_participants": whatsmeow.ParticipantChangeRemove,
			"promote":             whatsmeow.ParticipantChangePromote,
			"demote":              whatsmeow.ParticipantChangeDemote,
		}
		for action, want := range cases {
			svc, h := newHandler()
			_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
				"action": action, "group_id": "123@g.us", "participants": []any{"628"},
			}))
			require.NoError(t, err, action)
			require.NotNil(t, svc.managed, action)
			assert.Equal(t, want, svc.managed.Action, action)
		}
	})

	t.Run("invite_link with reset", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "invite_link", "group_id": "123@g.us", "reset": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.inviteReq)
		assert.True(t, svc.inviteReq.Reset)
	})

	t.Run("set_name", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "set_name", "group_id": "123@g.us", "name": "N",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.namedReq)
	})

	t.Run("set_topic", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "set_topic", "group_id": "123@g.us", "topic": "T",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.topicReq)
	})

	t.Run("set_settings applies both flags", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "set_settings", "group_id": "123@g.us", "announce": true, "locked": false,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.announceReq)
		assert.True(t, svc.announceReq.Announce)
		require.NotNil(t, svc.lockedReq)
		assert.False(t, svc.lockedReq.Locked)
	})

	t.Run("set_settings announce only", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "set_settings", "group_id": "123@g.us", "announce": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.announceReq)
		assert.Nil(t, svc.lockedReq)
	})

	t.Run("join_requests", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{"action": "join_requests", "group_id": "123@g.us"}))
		require.NoError(t, err)
		require.NotNil(t, svc.joinReqsReq)
	})

	t.Run("manage_join_requests", func(t *testing.T) {
		svc, h := newHandler()
		_, err := h.handleGroup(deviceCtx(), callReq(map[string]any{
			"action": "manage_join_requests", "group_id": "123@g.us",
			"participants": []any{"628"}, "request_action": "approve",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.managedJoins)
		assert.Equal(t, whatsmeow.ParticipantChangeApprove, svc.managedJoins.Action)
	})
}
