package group

import "context"

type IGroupService interface {
	JoinGroupWithLink(ctx context.Context, request JoinGroupWithLinkRequest) (groupID string, err error)
	LeaveGroup(ctx context.Context, request LeaveGroupRequest) (err error)
}

type JoinGroupWithLinkRequest struct {
	Link string `json:"link" form:"link"`
}

type LeaveGroupRequest struct {
	GroupID string `json:"group_id" form:"group_id"`
}
