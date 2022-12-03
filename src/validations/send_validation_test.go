package validations

import (
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
	"mime/multipart"
	"testing"
)

func TestValidateSendMessage(t *testing.T) {
	type args struct {
		request domainSend.MessageRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with phone and message",
			args: args{request: domainSend.MessageRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Message: "Hello this is testing",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.MessageRequest{
				Phone:   "",
				Message: "Hello this is testing",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message",
			args: args{request: domainSend.MessageRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Message: "",
			}},
			err: pkgError.ValidationError("message: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendMessage(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendImage(t *testing.T) {
	image := &multipart.FileHeader{
		Filename: "sample-image.png",
		Size:     100,
		Header:   map[string][]string{"Content-Type": {"image/png"}},
	}

	type args struct {
		request domainSend.ImageRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with normal condition",
			args: args{request: domainSend.ImageRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Caption: "Hello this is testing",
				Image:   image,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.ImageRequest{
				Phone: "",
				Image: image,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty image",
			args: args{request: domainSend.ImageRequest{
				Phone: "1728937129312@s.whatsapp.net",
				Image: nil,
			}},
			err: pkgError.ValidationError("image: cannot be blank."),
		},
		{
			name: "should error with invalid image type",
			args: args{request: domainSend.ImageRequest{
				Phone: "1728937129312@s.whatsapp.net",
				Image: &multipart.FileHeader{
					Filename: "sample-image.pdf",
					Size:     100,
					Header:   map[string][]string{"Content-Type": {"application/pdf"}},
				},
			}},
			err: pkgError.ValidationError("your image is not allowed. please use jpg/jpeg/png"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendImage(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendFile(t *testing.T) {
	file := &multipart.FileHeader{
		Filename: "sample-image.png",
		Size:     100,
		Header:   map[string][]string{"Content-Type": {"image/png"}},
	}

	type args struct {
		request domainSend.FileRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with normal condition",
			args: args{request: domainSend.FileRequest{
				Phone: "1728937129312@s.whatsapp.net",
				File:  file,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.FileRequest{
				Phone: "",
				File:  file,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty file",
			args: args{request: domainSend.FileRequest{
				Phone: "1728937129312@s.whatsapp.net",
				File:  nil,
			}},
			err: pkgError.ValidationError("file: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendFile(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendVideo(t *testing.T) {
	file := &multipart.FileHeader{
		Filename: "sample-video.mp4",
		Size:     100,
		Header:   map[string][]string{"Content-Type": {"video/mp4"}},
	}

	type args struct {
		request domainSend.VideoRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with normal condition",
			args: args{request: domainSend.VideoRequest{
				Phone:    "1728937129312@s.whatsapp.net",
				Caption:  "simple caption",
				Video:    file,
				ViewOnce: false,
				Compress: false,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.VideoRequest{
				Phone:    "",
				Caption:  "simple caption",
				Video:    file,
				ViewOnce: false,
				Compress: false,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty video",
			args: args{request: domainSend.VideoRequest{
				Phone:    "1728937129312@s.whatsapp.net",
				Caption:  "simple caption",
				Video:    nil,
				ViewOnce: false,
				Compress: false,
			}},
			err: pkgError.ValidationError("video: cannot be blank."),
		},
		{
			name: "should error with invalid format video",
			args: args{request: domainSend.VideoRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Caption: "simple caption",
				Video: func() *multipart.FileHeader {
					return &multipart.FileHeader{
						Filename: "sample-video.jpg",
						Size:     100,
						Header:   map[string][]string{"Content-Type": {"image/png"}},
					}
				}(),
				ViewOnce: false,
				Compress: false,
			}},
			err: pkgError.ValidationError("your video type is not allowed. please use mp4/mkv/avi"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendVideo(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendLink(t *testing.T) {
	type args struct {
		request domainSend.LinkRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success normal condition",
			args: args{request: domainSend.LinkRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Caption: "description",
				Link:    "https://google.com",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.LinkRequest{
				Phone:   "",
				Caption: "description",
				Link:    "https://google.com",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty caption",
			args: args{request: domainSend.LinkRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Caption: "",
				Link:    "https://google.com",
			}},
			err: pkgError.ValidationError("caption: cannot be blank."),
		},
		{
			name: "should error with empty link",
			args: args{request: domainSend.LinkRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Caption: "description",
				Link:    "",
			}},
			err: pkgError.ValidationError("link: cannot be blank."),
		},
		{
			name: "should error with invalid link",
			args: args{request: domainSend.LinkRequest{
				Phone:   "1728937129312@s.whatsapp.net",
				Caption: "description",
				Link:    "googlecom",
			}},
			err: pkgError.ValidationError("link: must be a valid URL."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendLink(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateRevokeMessage(t *testing.T) {
	type args struct {
		request domainSend.RevokeRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success normal condition",
			args: args{request: domainSend.RevokeRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				MessageID: "1382901271239781",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.RevokeRequest{
				Phone:     "",
				MessageID: "1382901271239781",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message id",
			args: args{request: domainSend.RevokeRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				MessageID: "",
			}},
			err: pkgError.ValidationError("message_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRevokeMessage(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateUpdateMessage(t *testing.T) {
	type args struct {
		request domainSend.UpdateMessageRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success normal condition",
			args: args{request: domainSend.UpdateMessageRequest{
				MessageID: "1382901271239781",
				Message:   "some update message",
				Phone:     "1728937129312@s.whatsapp.net",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.UpdateMessageRequest{
				MessageID: "1382901271239781",
				Message:   "some update message",
				Phone:     "",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message id",
			args: args{request: domainSend.UpdateMessageRequest{
				MessageID: "",
				Message:   "some update message",
				Phone:     "1728937129312@s.whatsapp.net",
			}},
			err: pkgError.ValidationError("message_id: cannot be blank."),
		},
		{
			name: "should error with empty message update",
			args: args{request: domainSend.UpdateMessageRequest{
				MessageID: "1382901271239781",
				Message:   "",
				Phone:     "1728937129312@s.whatsapp.net",
			}},
			err: pkgError.ValidationError("message: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpdateMessage(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendContact(t *testing.T) {
	type args struct {
		request domainSend.ContactRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success normal condition",
			args: args{request: domainSend.ContactRequest{
				Phone:        "1728937129312@s.whatsapp.net",
				ContactName:  "Aldino",
				ContactPhone: "62788712738123",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.ContactRequest{
				Phone:        "",
				ContactName:  "Aldino",
				ContactPhone: "62788712738123",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty contact name",
			args: args{request: domainSend.ContactRequest{
				Phone:        "1728937129312@s.whatsapp.net",
				ContactName:  "",
				ContactPhone: "62788712738123",
			}},
			err: pkgError.ValidationError("contact_name: cannot be blank."),
		},
		{
			name: "should error with empty contact phone",
			args: args{request: domainSend.ContactRequest{
				Phone:        "1728937129312@s.whatsapp.net",
				ContactName:  "Aldino",
				ContactPhone: "",
			}},
			err: pkgError.ValidationError("contact_phone: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendContact(tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}
