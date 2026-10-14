package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func TestContactDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		contact types.ContactInfo
		want    string
	}{
		{
			name:    "prefers full name over push name and business name",
			contact: types.ContactInfo{FullName: "Saved Name", PushName: "Push Name", BusinessName: "Biz Name"},
			want:    "Saved Name",
		},
		{
			name:    "falls back to push name when full name is empty",
			contact: types.ContactInfo{PushName: "Push Name", BusinessName: "Biz Name"},
			want:    "Push Name",
		},
		{
			name:    "falls back to business name when full and push names are empty",
			contact: types.ContactInfo{BusinessName: "Biz Name"},
			want:    "Biz Name",
		},
		{
			name:    "empty when no names are set",
			contact: types.ContactInfo{},
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contactInfoDisplayName(tt.contact); got != tt.want {
				t.Fatalf("contactInfoDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAvatarErrorTypesTheTwoOutcomesClientsMustTellApart(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   string
		wantStatus int
	}{
		{name: "no picture", err: fmt.Errorf("wrapped: %w", whatsmeow.ErrProfilePictureNotSet), wantCode: "AVATAR_NOT_SET", wantStatus: http.StatusNotFound},
		{name: "hidden by privacy", err: fmt.Errorf("wrapped: %w", whatsmeow.ErrProfilePictureUnauthorized), wantCode: "AVATAR_HIDDEN", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := avatarError(tt.err).(pkgError.GenericError)
			if !ok {
				t.Fatalf("avatarError(%v) is not a GenericError", tt.err)
			}
			if got.ErrCode() != tt.wantCode || got.StatusCode() != tt.wantStatus {
				t.Fatalf("got %s/%d, want %s/%d", got.ErrCode(), got.StatusCode(), tt.wantCode, tt.wantStatus)
			}
			// Clients matching on the message text keep working.
			if got.Error() != tt.err.Error() {
				t.Fatalf("message changed: %q, want %q", got.Error(), tt.err.Error())
			}
		})
	}

	other := errors.New("websocket not connected")
	if avatarError(other) != other {
		t.Fatalf("unrelated errors must pass through unchanged")
	}
}

func fakeAvatarContext() context.Context {
	client := &whatsmeow.Client{}
	instance := whatsapp.NewDeviceInstance("device-test", client, nil)
	return whatsapp.ContextWithDevice(context.Background(), instance)
}

func TestAvatarDirectLookup(t *testing.T) {
	ctx := fakeAvatarContext()

	tests := []struct {
		name       string
		lookupErr  error
		wantCode   string
		wantStatus int
		rawErr     bool
	}{
		{
			name:       "wrapped ErrProfilePictureNotSet returns 404 AVATAR_NOT_SET",
			lookupErr:  fmt.Errorf("lookup failed: %w", whatsmeow.ErrProfilePictureNotSet),
			wantCode:   "AVATAR_NOT_SET",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "wrapped ErrProfilePictureUnauthorized returns 403 AVATAR_HIDDEN",
			lookupErr:  fmt.Errorf("lookup failed: %w", whatsmeow.ErrProfilePictureUnauthorized),
			wantCode:   "AVATAR_HIDDEN",
			wantStatus: http.StatusForbidden,
		},
		{
			name:      "unrelated lookup error passes through unclassified",
			lookupErr: errors.New("connection reset by peer"),
			rawErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := serviceUser{
				validateJIDFn: func(_ *whatsmeow.Client, phone string) (types.JID, error) {
					return types.NewJID(phone, types.DefaultUserServer), nil
				},
				getProfilePictureInfoFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
					if params.IsCommunity {
						t.Fatal("expected IsCommunity=false for non-community lookup")
					}
					return nil, tt.lookupErr
				},
			}

			_, err := svc.Avatar(ctx, domainUser.AvatarRequest{Phone: "628123456789"})
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if tt.rawErr {
				if _, ok := err.(pkgError.GenericError); ok {
					t.Fatalf("expected unclassified error, got GenericError: %v", err)
				}
				if !errors.Is(err, tt.lookupErr) {
					t.Fatalf("got %v, want %v", err, tt.lookupErr)
				}
				return
			}

			genericErr, ok := err.(pkgError.GenericError)
			if !ok {
				t.Fatalf("expected GenericError, got %T: %v", err, err)
			}
			if genericErr.ErrCode() != tt.wantCode || genericErr.StatusCode() != tt.wantStatus {
				t.Fatalf("got %s/%d, want %s/%d", genericErr.ErrCode(), genericErr.StatusCode(), tt.wantCode, tt.wantStatus)
			}
		})
	}
}

func TestAvatarCommunityFallback(t *testing.T) {
	ctx := fakeAvatarContext()
	groupJID := types.NewJID("120363025@g.us", types.GroupServer)

	t.Run("both attempts agree on ErrProfilePictureNotSet returns 404 AVATAR_NOT_SET", func(t *testing.T) {
		var calls []bool
		svc := serviceUser{
			validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return groupJID, nil
			},
			getProfilePictureInfoFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
				calls = append(calls, params.IsCommunity)
				return nil, whatsmeow.ErrProfilePictureNotSet
			},
		}

		_, err := svc.Avatar(ctx, domainUser.AvatarRequest{Phone: "120363025@g.us", IsCommunity: true})
		if len(calls) != 2 || calls[0] != true || calls[1] != false {
			t.Fatalf("expected [true, false] calls, got %v", calls)
		}
		genericErr, ok := err.(pkgError.GenericError)
		if !ok {
			t.Fatalf("expected GenericError, got %T: %v", err, err)
		}
		if genericErr.ErrCode() != "AVATAR_NOT_SET" || genericErr.StatusCode() != http.StatusNotFound {
			t.Fatalf("got %s/%d, want AVATAR_NOT_SET/404", genericErr.ErrCode(), genericErr.StatusCode())
		}
	})

	t.Run("both attempts agree on ErrProfilePictureUnauthorized returns 403 AVATAR_HIDDEN", func(t *testing.T) {
		var calls []bool
		svc := serviceUser{
			validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return groupJID, nil
			},
			getProfilePictureInfoFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
				calls = append(calls, params.IsCommunity)
				return nil, whatsmeow.ErrProfilePictureUnauthorized
			},
		}

		_, err := svc.Avatar(ctx, domainUser.AvatarRequest{Phone: "120363025@g.us", IsCommunity: true})
		if len(calls) != 2 || calls[0] != true || calls[1] != false {
			t.Fatalf("expected [true, false] calls, got %v", calls)
		}
		genericErr, ok := err.(pkgError.GenericError)
		if !ok {
			t.Fatalf("expected GenericError, got %T: %v", err, err)
		}
		if genericErr.ErrCode() != "AVATAR_HIDDEN" || genericErr.StatusCode() != http.StatusForbidden {
			t.Fatalf("got %s/%d, want AVATAR_HIDDEN/403", genericErr.ErrCode(), genericErr.StatusCode())
		}
	})

	t.Run("fallback ErrProfilePictureUnauthorized mismatch does not misclassify as AVATAR_HIDDEN", func(t *testing.T) {
		// When initial community query returns ErrProfilePictureNotSet, the fallback with
		// IsCommunity=false may trigger a 401 (ErrProfilePictureUnauthorized) from WhatsApp
		// simply because the JID is a community. This must NOT be exposed as 403 AVATAR_HIDDEN.
		var calls []bool
		svc := serviceUser{
			validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return groupJID, nil
			},
			getProfilePictureInfoFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
				calls = append(calls, params.IsCommunity)
				if params.IsCommunity {
					return nil, whatsmeow.ErrProfilePictureNotSet
				}
				return nil, whatsmeow.ErrProfilePictureUnauthorized
			},
		}

		_, err := svc.Avatar(ctx, domainUser.AvatarRequest{Phone: "120363025@g.us", IsCommunity: true})
		if len(calls) != 2 || calls[0] != true || calls[1] != false {
			t.Fatalf("expected [true, false] calls, got %v", calls)
		}
		if _, ok := err.(pkgError.GenericError); ok {
			t.Fatalf("expected unclassified error to prevent false cache signals, got GenericError: %v", err)
		}
		if !errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) {
			t.Fatalf("expected raw fallback error, got %v", err)
		}
	})

	t.Run("fallback query mode error does not misclassify when initial query had generic error", func(t *testing.T) {
		var calls []bool
		initialErr := errors.New("upstream timeout")
		svc := serviceUser{
			validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return groupJID, nil
			},
			getProfilePictureInfoFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
				calls = append(calls, params.IsCommunity)
				if params.IsCommunity {
					return nil, initialErr
				}
				return nil, whatsmeow.ErrProfilePictureUnauthorized
			},
		}

		_, err := svc.Avatar(ctx, domainUser.AvatarRequest{Phone: "120363025@g.us", IsCommunity: true})
		if len(calls) != 2 || calls[0] != true || calls[1] != false {
			t.Fatalf("expected [true, false] calls, got %v", calls)
		}
		if _, ok := err.(pkgError.GenericError); ok {
			t.Fatalf("expected unclassified error, got GenericError: %v", err)
		}
		if !errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) {
			t.Fatalf("expected raw fallback error, got %v", err)
		}
	})

	t.Run("fallback succeeds and returns picture URL", func(t *testing.T) {
		var calls []bool
		svc := serviceUser{
			validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return groupJID, nil
			},
			getProfilePictureInfoFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
				calls = append(calls, params.IsCommunity)
				if params.IsCommunity {
					return nil, whatsmeow.ErrProfilePictureNotSet
				}
				return &types.ProfilePictureInfo{URL: "https://example.com/pic.jpg", ID: "pic-123", Type: "image"}, nil
			},
		}

		resp, err := svc.Avatar(ctx, domainUser.AvatarRequest{Phone: "120363025@g.us", IsCommunity: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(calls) != 2 || calls[0] != true || calls[1] != false {
			t.Fatalf("expected [true, false] calls, got %v", calls)
		}
		if resp.URL != "https://example.com/pic.jpg" || resp.ID != "pic-123" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("regular user JID forces IsCommunity to false and skips fallback", func(t *testing.T) {
		var calls []bool
		userJID := types.NewJID("628123456789", types.DefaultUserServer)
		svc := serviceUser{
			validateJIDFn: func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return userJID, nil
			},
			getProfilePictureInfoFn: func(_ context.Context, _ *whatsmeow.Client, _ types.JID, params *whatsmeow.GetProfilePictureParams) (*types.ProfilePictureInfo, error) {
				calls = append(calls, params.IsCommunity)
				return nil, whatsmeow.ErrProfilePictureNotSet
			},
		}

		_, err := svc.Avatar(ctx, domainUser.AvatarRequest{Phone: "628123456789", IsCommunity: true})
		if len(calls) != 1 || calls[0] != false {
			t.Fatalf("expected single call with IsCommunity=false, got %v", calls)
		}
		genericErr, ok := err.(pkgError.GenericError)
		if !ok {
			t.Fatalf("expected GenericError, got %T: %v", err, err)
		}
		if genericErr.ErrCode() != "AVATAR_NOT_SET" || genericErr.StatusCode() != http.StatusNotFound {
			t.Fatalf("got %s/%d, want AVATAR_NOT_SET/404", genericErr.ErrCode(), genericErr.StatusCode())
		}
	})
}
