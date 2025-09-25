package validations

import (
	"context"
	"mime/multipart"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidateSendSticker(t *testing.T) {
	sticker := &multipart.FileHeader{
		Filename: "sample-sticker.png",
		Size:     100,
		Header:   map[string][]string{"Content-Type": {"image/png"}},
	}

	type args struct {
		request domainSend.StickerRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with sticker file",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
					Sticker:     sticker,
				},
			},
			err: nil,
		},
		{
			name: "should success with sticker URL",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
					StickerURL:  func() *string { s := "https://example.com/sticker.png"; return &s }(),
				},
			},
			err: nil,
		},
		{
			name: "should error without phone",
			args: args{
				request: domainSend.StickerRequest{
					Sticker: sticker,
				},
			},
			err: pkgError.ValidationError("Phone: cannot be blank."),
		},
		{
			name: "should error without sticker and sticker_url",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
				},
			},
			err: pkgError.ValidationError("either Sticker or StickerURL must be provided"),
		},
		{
			name: "should error with both sticker and sticker_url",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
					Sticker:     sticker,
					StickerURL:  func() *string { s := "https://example.com/sticker.png"; return &s }(),
				},
			},
			err: pkgError.ValidationError("cannot provide both Sticker file and StickerURL"),
		},
		{
			name: "should error with invalid URL",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
					StickerURL:  func() *string { s := "not-a-valid-url"; return &s }(),
				},
			},
			err: pkgError.ValidationError("StickerURL must be a valid URL"),
		},
		{
			name: "should success with WebP sticker",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
					Sticker: &multipart.FileHeader{
						Filename: "sample-sticker.webp",
						Size:     100,
						Header:   map[string][]string{"Content-Type": {"image/webp"}},
					},
				},
			},
			err: nil,
		},
		{
			name: "should success with GIF sticker",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
					Sticker: &multipart.FileHeader{
						Filename: "sample-sticker.gif",
						Size:     100,
						Header:   map[string][]string{"Content-Type": {"image/gif"}},
					},
				},
			},
			err: nil,
		},
		{
			name: "should error with unsupported file type",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{Phone: "+6289123456"},
					Sticker: &multipart.FileHeader{
						Filename: "sample-sticker.bmp",
						Size:     100,
						Header:   map[string][]string{"Content-Type": {"image/bmp"}},
					},
				},
			},
			err: pkgError.ValidationError("your sticker is not allowed. please use jpg/jpeg/png/webp/gif"),
		},
		{
			name: "should success with valid duration",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{
						Phone:    "+6289123456",
						Duration: func() *int { d := 3600; return &d }(),
					},
					Sticker: sticker,
				},
			},
			err: nil,
		},
		{
			name: "should error with invalid duration",
			args: args{
				request: domainSend.StickerRequest{
					BaseRequest: domainSend.BaseRequest{
						Phone:    "+6289123456",
						Duration: func() *int { d := -1; return &d }(),
					},
					Sticker: sticker,
				},
			},
			err: pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendSticker(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}