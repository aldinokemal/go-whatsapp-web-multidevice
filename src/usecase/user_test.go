package usecase

import (
	"testing"

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
