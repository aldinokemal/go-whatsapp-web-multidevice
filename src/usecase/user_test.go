package usecase

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

func TestDecodeAvatarImageRejectsOversizedHeaderBeforeDecode(t *testing.T) {
	img, err := decodeAvatarImage(bytes.NewReader(pngWithIHDR(20000, 20000)))
	require.ErrorIs(t, err, utils.ErrImageTooLarge)
	require.Nil(t, img)
}

func TestDecodeAvatarImageAcceptsSmallPNG(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 8, 8))))
	img, err := decodeAvatarImage(bytes.NewReader(encoded.Bytes()))
	require.NoError(t, err)
	require.Equal(t, 8, img.Bounds().Dx())
	require.Equal(t, 8, img.Bounds().Dy())
}

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
