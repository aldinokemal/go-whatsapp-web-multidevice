package validations

import (
	"context"
	"testing"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidateUnfollowNewsletter(t *testing.T) {
	type args struct {
		request domainNewsletter.UnfollowRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid newsletter id",
			args: args{request: domainNewsletter.UnfollowRequest{
				NewsletterID: "120363123456789@newsletter",
			}},
			err: nil,
		},
		{
			name: "should success with different newsletter id format",
			args: args{request: domainNewsletter.UnfollowRequest{
				NewsletterID: "newsletter-abc123xyz",
			}},
			err: nil,
		},
		{
			name: "should error with empty newsletter id",
			args: args{request: domainNewsletter.UnfollowRequest{
				NewsletterID: "",
			}},
			err: pkgError.ValidationError("newsletter_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUnfollowNewsletter(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateGetNewsletterMessages(t *testing.T) {
	tests := []struct {
		name       string
		request    domainNewsletter.GetMessagesRequest
		err        any
		wantCount  int
		wantBefore int
	}{
		{
			name:      "should success with valid newsletter id and default count",
			request:   domainNewsletter.GetMessagesRequest{NewsletterID: "120363123456789@newsletter"},
			err:       nil,
			wantCount: 50,
		},
		{
			name:       "should success with explicit count and before",
			request:    domainNewsletter.GetMessagesRequest{NewsletterID: "120363123456789@newsletter", Count: 10, Before: 100},
			err:        nil,
			wantCount:  10,
			wantBefore: 100,
		},
		{
			name:    "should error with empty newsletter id",
			request: domainNewsletter.GetMessagesRequest{},
			err:     pkgError.ValidationError("newsletter_id: cannot be blank."),
		},
		{
			name:      "should error when count exceeds max",
			request:   domainNewsletter.GetMessagesRequest{NewsletterID: "120363123456789@newsletter", Count: 101},
			err:       pkgError.ValidationError("count: must be no greater than 100."),
			wantCount: 101,
		},
		{
			name:      "should error with negative before",
			request:   domainNewsletter.GetMessagesRequest{NewsletterID: "120363123456789@newsletter", Before: -1},
			err:       pkgError.ValidationError("before: must be no less than 0."),
			wantCount: 50,
		},
		{
			name:      "should error with a non-newsletter jid (phone)",
			request:   domainNewsletter.GetMessagesRequest{NewsletterID: "6289685028129@s.whatsapp.net"},
			err:       pkgError.ValidationError("newsletter_id: must end with @newsletter."),
			wantCount: 50,
		},
		{
			name:      "should error with a non-newsletter jid (group)",
			request:   domainNewsletter.GetMessagesRequest{NewsletterID: "120363123456789@g.us"},
			err:       pkgError.ValidationError("newsletter_id: must end with @newsletter."),
			wantCount: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.request
			err := ValidateGetNewsletterMessages(context.Background(), &req)
			assert.Equal(t, tt.err, err)
			if tt.wantCount != 0 {
				assert.Equal(t, tt.wantCount, req.Count)
			}
			if tt.wantBefore != 0 {
				assert.Equal(t, tt.wantBefore, req.Before)
			}
		})
	}
}
