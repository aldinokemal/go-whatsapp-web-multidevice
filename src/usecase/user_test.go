package usecase

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

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
