package group

import "context"

type IGroupService interface {
	JoinGroupWithLink(ctx context.Context, request JoinGroupWithLinkRequest) (response JoinGroupWithLinkResponse, err error)
}

type JoinGroupWithLinkRequest struct {
	Link string `json:"link" query:"link"`
}

type JoinGroupWithLinkResponse struct {
	JID string `json:"jid"`
}
