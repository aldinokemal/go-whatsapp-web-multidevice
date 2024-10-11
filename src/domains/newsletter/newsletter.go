package newsletter

import "context"

type INewsletterService interface {
	Unfollow(ctx context.Context, request UnfollowRequest) (err error)
}

type UnfollowRequest struct {
	NewsletterID string `json:"newsletter_id" form:"newsletter_id"`
}
