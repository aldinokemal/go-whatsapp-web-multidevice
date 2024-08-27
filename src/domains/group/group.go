package group

import (
	"context"
	"go.mau.fi/whatsmeow"
)

type IGroupService interface {
	JoinGroupWithLink(ctx context.Context, request JoinGroupWithLinkRequest) (groupID string, err error)
	LeaveGroup(ctx context.Context, request LeaveGroupRequest) (err error)
	CreateGroup(ctx context.Context, request CreateGroupRequest) (groupID string, err error)
	ManageParticipant(ctx context.Context, request ParticipantRequest) (result []ParticipantStatus, err error)
}

type JoinGroupWithLinkRequest struct {
	Link string `json:"link" form:"link"`
}

type LeaveGroupRequest struct {
	GroupID string `json:"group_id" form:"group_id"`
}

type CreateGroupRequest struct {
	Title        string   `json:"title" form:"title"`
	Participants []string `json:"participants" form:"participants"`
}

type ParticipantRequest struct {
	GroupID      string                      `json:"group_id" form:"group_id"`
	Participants []string                    `json:"participants" form:"participants"`
	Action       whatsmeow.ParticipantChange `json:"action" form:"action"`
}

type ParticipantStatus struct {
	Participant string `json:"participant"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}
