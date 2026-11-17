package validations

import (
	"context"
	"math"
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

func TestValidateDownloadNewsletterMedia(t *testing.T) {
	tests := []struct {
		name    string
		request domainNewsletter.DownloadMediaRequest
		err     any
	}{
		{
			name: "should accept valid request",
			request: domainNewsletter.DownloadMediaRequest{
				NewsletterID: "120363123456789@newsletter",
				ServerID:     8872,
			},
		},
		{
			name:    "should reject empty newsletter id",
			request: domainNewsletter.DownloadMediaRequest{ServerID: 8872},
			err:     pkgError.ValidationError("newsletter_id: cannot be blank."),
		},
		{
			name: "should reject non-newsletter jid",
			request: domainNewsletter.DownloadMediaRequest{
				NewsletterID: "120363123456789@g.us",
				ServerID:     8872,
			},
			err: pkgError.ValidationError("newsletter_id: must end with @newsletter."),
		},
		{
			name: "should reject zero server id",
			request: domainNewsletter.DownloadMediaRequest{
				NewsletterID: "120363123456789@newsletter",
			},
			err: pkgError.ValidationError("server_id: must be no less than 1."),
		},
		{
			name: "should reject negative server id",
			request: domainNewsletter.DownloadMediaRequest{
				NewsletterID: "120363123456789@newsletter",
				ServerID:     -1,
			},
			err: pkgError.ValidationError("server_id: must be no less than 1."),
		},
		{
			name: "should reject server id that would overflow lookup boundary",
			request: domainNewsletter.DownloadMediaRequest{
				NewsletterID: "120363123456789@newsletter",
				ServerID:     math.MaxInt,
			},
			err: pkgError.ValidationError("server_id: must be less than the maximum integer value."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDownloadNewsletterMedia(context.Background(), tt.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateFollowNewsletter(t *testing.T) {
	assert.NoError(t, ValidateFollowNewsletter(context.Background(), domainNewsletter.FollowRequest{NewsletterID: "120363123456789@newsletter"}))
	assert.Equal(t, pkgError.ValidationError("newsletter_id: cannot be blank."),
		ValidateFollowNewsletter(context.Background(), domainNewsletter.FollowRequest{}))
	assert.Equal(t, pkgError.ValidationError("newsletter_id: must end with @newsletter."),
		ValidateFollowNewsletter(context.Background(), domainNewsletter.FollowRequest{NewsletterID: "120363123456789@g.us"}))
}

func TestValidateGetNewsletterInfo(t *testing.T) {
	tests := []struct {
		name    string
		request domainNewsletter.GetInfoRequest
		err     error
	}{
		{name: "by jid", request: domainNewsletter.GetInfoRequest{NewsletterID: "120363123456789@newsletter"}},
		{name: "by invite link", request: domainNewsletter.GetInfoRequest{Invite: "https://whatsapp.com/channel/0029VaABCDEF"}},
		{
			name: "neither",
			err:  pkgError.ValidationError("exactly one of newsletter_id or invite must be provided."),
		},
		{
			name:    "both",
			request: domainNewsletter.GetInfoRequest{NewsletterID: "120363123456789@newsletter", Invite: "0029VaABCDEF"},
			err:     pkgError.ValidationError("exactly one of newsletter_id or invite must be provided."),
		},
		{
			name:    "non-newsletter jid",
			request: domainNewsletter.GetInfoRequest{NewsletterID: "6289685028129@s.whatsapp.net"},
			err:     pkgError.ValidationError("newsletter_id: must end with @newsletter."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.err, ValidateGetNewsletterInfo(context.Background(), tt.request))
		})
	}
}

func TestValidateGetNewsletterMessageUpdates(t *testing.T) {
	request := domainNewsletter.GetMessageUpdatesRequest{NewsletterID: "120363123456789@newsletter", After: 100}
	assert.NoError(t, ValidateGetNewsletterMessageUpdates(context.Background(), &request))
	assert.Equal(t, 50, request.Count)

	request = domainNewsletter.GetMessageUpdatesRequest{NewsletterID: "120363123456789@newsletter"}
	assert.Equal(t, pkgError.ValidationError("after: must be a message server ID; WhatsApp does not answer message_updates without it."),
		ValidateGetNewsletterMessageUpdates(context.Background(), &request))

	request = domainNewsletter.GetMessageUpdatesRequest{NewsletterID: "120363123456789@newsletter", After: -5}
	assert.Equal(t, pkgError.ValidationError("after: must be no less than 1."),
		ValidateGetNewsletterMessageUpdates(context.Background(), &request))

	request = domainNewsletter.GetMessageUpdatesRequest{NewsletterID: "120363123456789@newsletter", After: 100, Count: 101}
	assert.Equal(t, pkgError.ValidationError("count: must be no greater than 100."),
		ValidateGetNewsletterMessageUpdates(context.Background(), &request))

	request = domainNewsletter.GetMessageUpdatesRequest{NewsletterID: "120363123456789@newsletter", After: 100, Since: -1}
	assert.Equal(t, pkgError.ValidationError("since: must be no less than 0."),
		ValidateGetNewsletterMessageUpdates(context.Background(), &request))
}

func TestValidateNewsletterLiveUpdates(t *testing.T) {
	assert.NoError(t, ValidateNewsletterLiveUpdates(context.Background(), domainNewsletter.LiveUpdatesRequest{NewsletterID: "120363123456789@newsletter"}))
	assert.Equal(t, pkgError.ValidationError("newsletter_id: cannot be blank."),
		ValidateNewsletterLiveUpdates(context.Background(), domainNewsletter.LiveUpdatesRequest{}))
}

func TestValidateNewsletterDirectory(t *testing.T) {
	request := domainNewsletter.DirectoryRequest{CountryCodes: []string{" br,id ", "us"}, Categories: []string{"news"}}
	assert.NoError(t, ValidateNewsletterDirectory(context.Background(), &request))
	assert.Equal(t, "RECOMMENDED", request.View)
	assert.Equal(t, 20, request.Limit)
	assert.Equal(t, []string{"BR", "ID", "US"}, request.CountryCodes)
	assert.Equal(t, []string{"NEWS"}, request.Categories)

	request = domainNewsletter.DirectoryRequest{View: "trending", CountryCodes: []string{"BR"}}
	assert.NoError(t, ValidateNewsletterDirectory(context.Background(), &request))
	assert.Equal(t, "TRENDING", request.View)

	request = domainNewsletter.DirectoryRequest{View: "hot", CountryCodes: []string{"BR"}}
	assert.Equal(t, pkgError.ValidationError("view: must be a valid value."),
		ValidateNewsletterDirectory(context.Background(), &request))

	request = domainNewsletter.DirectoryRequest{Limit: 51, CountryCodes: []string{"BR"}}
	assert.Equal(t, pkgError.ValidationError("limit: must be no greater than 50."),
		ValidateNewsletterDirectory(context.Background(), &request))

	request = domainNewsletter.DirectoryRequest{CountryCodes: []string{"BRA"}}
	assert.Error(t, ValidateNewsletterDirectory(context.Background(), &request))

	request = domainNewsletter.DirectoryRequest{View: "POPULAR"}
	assert.Equal(t, pkgError.ValidationError("country_codes: at least one country code is required to browse the directory."),
		ValidateNewsletterDirectory(context.Background(), &request))

	request = domainNewsletter.DirectoryRequest{SearchText: "news", Categories: []string{"news"}}
	assert.NoError(t, ValidateNewsletterDirectory(context.Background(), &request))

	request = domainNewsletter.DirectoryRequest{SearchText: "news", Categories: []string{"POLITICS"}}
	assert.Error(t, ValidateNewsletterDirectory(context.Background(), &request))
}
