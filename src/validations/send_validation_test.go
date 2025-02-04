package validations

import (
	"context"
	"mime/multipart"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
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
			err := ValidateSendMessage(context.Background(), tt.args.request)
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
			err: pkgError.ValidationError("either Image or ImageURL must be provided"),
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
			err := ValidateSendImage(context.Background(), tt.args.request)
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
			err := ValidateSendFile(context.Background(), tt.args.request)
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
			err := ValidateSendVideo(context.Background(), tt.args.request)
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
			err := ValidateSendLink(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateRevokeMessage(t *testing.T) {
	type args struct {
		request domainMessage.RevokeRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success normal condition",
			args: args{request: domainMessage.RevokeRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				MessageID: "1382901271239781",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainMessage.RevokeRequest{
				Phone:     "",
				MessageID: "1382901271239781",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message id",
			args: args{request: domainMessage.RevokeRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				MessageID: "",
			}},
			err: pkgError.ValidationError("message_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRevokeMessage(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateUpdateMessage(t *testing.T) {
	type args struct {
		request domainMessage.UpdateMessageRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success normal condition",
			args: args{request: domainMessage.UpdateMessageRequest{
				MessageID: "1382901271239781",
				Message:   "some update message",
				Phone:     "1728937129312@s.whatsapp.net",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainMessage.UpdateMessageRequest{
				MessageID: "1382901271239781",
				Message:   "some update message",
				Phone:     "",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message id",
			args: args{request: domainMessage.UpdateMessageRequest{
				MessageID: "",
				Message:   "some update message",
				Phone:     "1728937129312@s.whatsapp.net",
			}},
			err: pkgError.ValidationError("message_id: cannot be blank."),
		},
		{
			name: "should error with empty message update",
			args: args{request: domainMessage.UpdateMessageRequest{
				MessageID: "1382901271239781",
				Message:   "",
				Phone:     "1728937129312@s.whatsapp.net",
			}},
			err: pkgError.ValidationError("message: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpdateMessage(context.Background(), tt.args.request)
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
			err := ValidateSendContact(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendLocation(t *testing.T) {
	type args struct {
		request domainSend.LocationRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success normal condition",
			args: args{request: domainSend.LocationRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Latitude:  "-7.797068",
				Longitude: "110.370529",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.LocationRequest{
				Phone:     "",
				Latitude:  "-7.797068",
				Longitude: "110.370529",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty latitude",
			args: args{request: domainSend.LocationRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Latitude:  "",
				Longitude: "110.370529",
			}},
			err: pkgError.ValidationError("latitude: cannot be blank."),
		},
		{
			name: "should error with empty longitude",
			args: args{request: domainSend.LocationRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Latitude:  "-7.797068",
				Longitude: "",
			}},
			err: pkgError.ValidationError("longitude: cannot be blank."),
		},
		{
			name: "should error with invalid latitude",
			args: args{request: domainSend.LocationRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Latitude:  "ABCDEF",
				Longitude: "110.370529",
			}},
			err: pkgError.ValidationError("latitude: must be a valid latitude."),
		},
		{
			name: "should error with invalid latitude",
			args: args{request: domainSend.LocationRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Latitude:  "-7.797068",
				Longitude: "ABCDEF",
			}},
			err: pkgError.ValidationError("longitude: must be a valid longitude."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendLocation(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendAudio(t *testing.T) {
	audio := &multipart.FileHeader{
		Filename: "sample-audio.mp3",
		Size:     100,
		Header:   map[string][]string{"Content-Type": {"audio/mp3"}},
	}

	type args struct {
		request domainSend.AudioRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with normal condition",
			args: args{request: domainSend.AudioRequest{
				Phone: "1728937129312@s.whatsapp.net",
				Audio: audio,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.AudioRequest{
				Phone: "",
				Audio: audio,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty audio",
			args: args{request: domainSend.AudioRequest{
				Phone: "1728937129312@s.whatsapp.net",
				Audio: nil,
			}},
			err: pkgError.ValidationError("audio: cannot be blank."),
		},
		{
			name: "should error with invalid audio type",
			args: args{request: domainSend.AudioRequest{
				Phone: "1728937129312@s.whatsapp.net",
				Audio: &multipart.FileHeader{
					Filename: "sample-audio.txt",
					Size:     100,
					Header:   map[string][]string{"Content-Type": {"text/plain"}},
				},
			}},
			err: pkgError.ValidationError("your audio type is not allowed. please use (audio/aac,audio/amr,audio/flac,audio/m4a,audio/m4r,audio/mp3,audio/mpeg,audio/ogg,audio/vnd.wav,audio/vnd.wave,audio/wav,audio/wave,audio/wma,audio/x-ms-wma,audio/x-pn-wav,audio/x-wav,)"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendAudio(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendPoll(t *testing.T) {
	type args struct {
		request domainSend.PollRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with normal condition",
			args: args{request: domainSend.PollRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Question:  "What is your favorite color?",
				Options:   []string{"Red", "Blue", "Green"},
				MaxAnswer: 1,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.PollRequest{
				Phone:     "",
				Question:  "What is your favorite color?",
				Options:   []string{"Red", "Blue", "Green"},
				MaxAnswer: 1,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty question",
			args: args{request: domainSend.PollRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Question:  "",
				Options:   []string{"Red", "Blue", "Green"},
				MaxAnswer: 1,
			}},
			err: pkgError.ValidationError("question: cannot be blank."),
		},
		{
			name: "should error with empty options",
			args: args{request: domainSend.PollRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Question:  "What is your favorite color?",
				Options:   []string{},
				MaxAnswer: 5,
			}},
			err: pkgError.ValidationError("options: cannot be blank."),
		},
		{
			name: "should error with duplicate options",
			args: args{request: domainSend.PollRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Question:  "What is your favorite color?",
				Options:   []string{"Red", "Red", "Green"},
				MaxAnswer: 1,
			}},
			err: pkgError.ValidationError("options should be unique"),
		},
		{
			name: "should error with max answer greater than options",
			args: args{request: domainSend.PollRequest{
				Phone:     "1728937129312@s.whatsapp.net",
				Question:  "What is your favorite color?",
				Options:   []string{"Red", "Blue", "Green"},
				MaxAnswer: 5,
			}},
			err: pkgError.ValidationError("max_answer: must be no greater than 3."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendPoll(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendPresence(t *testing.T) {
	type args struct {
		request domainSend.PresenceRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with available type",
			args: args{request: domainSend.PresenceRequest{
				Type: "available",
			}},
			err: nil,
		},
		{
			name: "should success with unavailable type",
			args: args{request: domainSend.PresenceRequest{
				Type: "unavailable",
			}},
			err: nil,
		},
		{
			name: "should error with invalid type",
			args: args{request: domainSend.PresenceRequest{
				Type: "invalid",
			}},
			err: pkgError.ValidationError("type: must be a valid value."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendPresence(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}
