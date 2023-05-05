package group

import "context"

type IGroupService interface {
	JoinGroupWithLink(ctx context.Context, request JoinGroupWithLinkRequest) (groupID string, err error)
	LeaveGroup(ctx context.Context, groupID string) (err error)
}

type JoinGroupWithLinkRequest struct {
	Link string `json:"link" form:"link"`
}

type JoinGroupWithLinkResponse struct {
	JID string `json:"jid"`
}
