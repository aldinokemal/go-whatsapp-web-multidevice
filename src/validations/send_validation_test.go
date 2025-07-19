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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Message: "Hello this is testing",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.MessageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				Message: "Hello this is testing",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message",
			args: args{request: domainSend.MessageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption: "Hello this is testing",
				Image:   image,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.ImageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				Image: image,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty image",
			args: args{request: domainSend.ImageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Image: nil,
			}},
			err: pkgError.ValidationError("either Image or ImageURL must be provided"),
		},
		{
			name: "should error with invalid image type",
			args: args{request: domainSend.ImageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				File: file,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.FileRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				File: file,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty file",
			args: args{request: domainSend.FileRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				File: nil,
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption:  "simple caption",
				Video:    nil,
				ViewOnce: false,
				Compress: false,
			}},
			err: pkgError.ValidationError("either Video or VideoURL must be provided"),
		},
		{
			name: "should error with invalid format video",
			args: args{request: domainSend.VideoRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
			err: pkgError.ValidationError("your video type is not allowed. please use mp4/mkv/avi/x-msvideo"),
		},
		{
			name: "should error with empty video and video_url",
			args: args{request: domainSend.VideoRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption:  "simple caption",
				Video:    nil,
				VideoURL: func() *string { s := ""; return &s }(),
				ViewOnce: false,
				Compress: false,
			}},
			err: pkgError.ValidationError("either Video or VideoURL must be provided"),
		},
		{
			name: "should success with video_url provided",
			args: args{request: domainSend.VideoRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption:  "simple caption",
				Video:    nil,
				VideoURL: func() *string { s := "https://example.com/sample.mp4"; return &s }(),
				ViewOnce: false,
				Compress: false,
			}},
			err: nil,
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption: "description",
				Link:    "https://google.com",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.LinkRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				Caption: "description",
				Link:    "https://google.com",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty caption",
			args: args{request: domainSend.LinkRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption: "",
				Link:    "https://google.com",
			}},
			err: pkgError.ValidationError("caption: cannot be blank."),
		},
		{
			name: "should error with empty link",
			args: args{request: domainSend.LinkRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption: "description",
				Link:    "",
			}},
			err: pkgError.ValidationError("link: cannot be blank."),
		},
		{
			name: "should error with invalid link",
			args: args{request: domainSend.LinkRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				ContactName:  "Aldino",
				ContactPhone: "62788712738123",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.ContactRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				ContactName:  "Aldino",
				ContactPhone: "62788712738123",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty contact name",
			args: args{request: domainSend.ContactRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				ContactName:  "",
				ContactPhone: "62788712738123",
			}},
			err: pkgError.ValidationError("contact_name: cannot be blank."),
		},
		{
			name: "should error with empty contact phone",
			args: args{request: domainSend.ContactRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Latitude:  "-7.797068",
				Longitude: "110.370529",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.LocationRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				Latitude:  "-7.797068",
				Longitude: "110.370529",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty latitude",
			args: args{request: domainSend.LocationRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Latitude:  "",
				Longitude: "110.370529",
			}},
			err: pkgError.ValidationError("latitude: cannot be blank."),
		},
		{
			name: "should error with empty longitude",
			args: args{request: domainSend.LocationRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Latitude:  "-7.797068",
				Longitude: "",
			}},
			err: pkgError.ValidationError("longitude: cannot be blank."),
		},
		{
			name: "should error with invalid latitude",
			args: args{request: domainSend.LocationRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Latitude:  "ABCDEF",
				Longitude: "110.370529",
			}},
			err: pkgError.ValidationError("latitude: must be a valid latitude."),
		},
		{
			name: "should error with invalid latitude",
			args: args{request: domainSend.LocationRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Audio: audio,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.AudioRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				Audio: audio,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty audio",
			args: args{request: domainSend.AudioRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Audio: nil,
			}},
			err: pkgError.ValidationError("either Audio or AudioURL must be provided"),
		},
		{
			name: "should error with invalid audio type",
			args: args{request: domainSend.AudioRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Question:  "What is your favorite color?",
				Options:   []string{"Red", "Blue", "Green"},
				MaxAnswer: 1,
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.PollRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "",
				},
				Question:  "What is your favorite color?",
				Options:   []string{"Red", "Blue", "Green"},
				MaxAnswer: 1,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty question",
			args: args{request: domainSend.PollRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Question:  "",
				Options:   []string{"Red", "Blue", "Green"},
				MaxAnswer: 1,
			}},
			err: pkgError.ValidationError("question: cannot be blank."),
		},
		{
			name: "should error with empty options",
			args: args{request: domainSend.PollRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Question:  "What is your favorite color?",
				Options:   []string{},
				MaxAnswer: 5,
			}},
			err: pkgError.ValidationError("options: cannot be blank."),
		},
		{
			name: "should error with duplicate options",
			args: args{request: domainSend.PollRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Question:  "What is your favorite color?",
				Options:   []string{"Red", "Red", "Green"},
				MaxAnswer: 1,
			}},
			err: pkgError.ValidationError("options should be unique"),
		},
		{
			name: "should error with max answer greater than options",
			args: args{request: domainSend.PollRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
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

func TestValidateSendChatPresence(t *testing.T) {
	type args struct {
		request domainSend.ChatPresenceRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with start action",
			args: args{request: domainSend.ChatPresenceRequest{
				Phone:  "1728937129312@s.whatsapp.net",
				Action: "start",
			}},
			err: nil,
		},
		{
			name: "should success with stop action",
			args: args{request: domainSend.ChatPresenceRequest{
				Phone:  "1728937129312@s.whatsapp.net",
				Action: "stop",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainSend.ChatPresenceRequest{
				Phone:  "",
				Action: "start",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty action",
			args: args{request: domainSend.ChatPresenceRequest{
				Phone:  "1728937129312@s.whatsapp.net",
				Action: "",
			}},
			err: pkgError.ValidationError("action: cannot be blank."),
		},
		{
			name: "should error with invalid action",
			args: args{request: domainSend.ChatPresenceRequest{
				Phone:  "1728937129312@s.whatsapp.net",
				Action: "invalid",
			}},
			err: pkgError.ValidationError("action: must be a valid value."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendChatPresence(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration *int
		err      any
	}{
		{
			name:     "should success with nil duration",
			duration: nil,
			err:      nil,
		},
		{
			name:     "should success with zero duration",
			duration: func() *int { d := 0; return &d }(),
			err:      nil,
		},
		{
			name:     "should success with valid duration",
			duration: func() *int { d := 3600; return &d }(),
			err:      nil,
		},
		{
			name:     "should success with max duration",
			duration: func() *int { d := int(maxDuration); return &d }(),
			err:      nil,
		},
		{
			name:     "should error with negative duration",
			duration: func() *int { d := -1; return &d }(),
			err:      pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)"),
		},
		{
			name:     "should error with duration too high",
			duration: func() *int { d := int(maxDuration) + 1; return &d }(),
			err:      pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDuration(tt.duration)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendMessage_WithDuration(t *testing.T) {
	type args struct {
		request domainSend.MessageRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid duration",
			args: args{request: domainSend.MessageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone:    "1728937129312@s.whatsapp.net",
					Duration: func() *int { d := 3600; return &d }(),
				},
				Message: "Hello this is testing",
			}},
			err: nil,
		},
		{
			name: "should error with invalid duration",
			args: args{request: domainSend.MessageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone:    "1728937129312@s.whatsapp.net",
					Duration: func() *int { d := -1; return &d }(),
				},
				Message: "Hello this is testing",
			}},
			err: pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendMessage(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendImage_WithImageURL(t *testing.T) {
	type args struct {
		request domainSend.ImageRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with image URL",
			args: args{request: domainSend.ImageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption:  "Hello this is testing",
				ImageURL: func() *string { s := "https://example.com/image.jpg"; return &s }(),
			}},
			err: nil,
		},
		{
			name: "should error with empty image URL",
			args: args{request: domainSend.ImageRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				Caption:  "Hello this is testing",
				ImageURL: func() *string { s := ""; return &s }(),
			}},
			err: pkgError.ValidationError("either Image or ImageURL must be provided"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendImage(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendFile_WithDuration(t *testing.T) {
	file := &multipart.FileHeader{
		Filename: "sample-file.pdf",
		Size:     100,
		Header:   map[string][]string{"Content-Type": {"application/pdf"}},
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
			name: "should success with valid duration",
			args: args{request: domainSend.FileRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone:    "1728937129312@s.whatsapp.net",
					Duration: func() *int { d := 3600; return &d }(),
				},
				File: file,
			}},
			err: nil,
		},
		{
			name: "should error with invalid duration",
			args: args{request: domainSend.FileRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone:    "1728937129312@s.whatsapp.net",
					Duration: func() *int { d := -1; return &d }(),
				},
				File: file,
			}},
			err: pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendFile(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSendAudio_WithDuration(t *testing.T) {
	audio := &multipart.FileHeader{
		Filename: "sample-audio.mp3",
		Size:     100,
		Header:   map[string][]string{"Content-Type": {"audio/mpeg"}},
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
			name: "should success with valid duration and audio file",
			args: args{request: domainSend.AudioRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone:    "1728937129312@s.whatsapp.net",
					Duration: func() *int { d := 3600; return &d }(),
				},
				Audio: audio,
			}},
			err: nil,
		},
		{
			name: "should success with audio URL",
			args: args{request: domainSend.AudioRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone: "1728937129312@s.whatsapp.net",
				},
				AudioURL: func() *string { s := "https://example.com/audio.mp3"; return &s }(),
			}},
			err: nil,
		},
		{
			name: "should error with invalid duration",
			args: args{request: domainSend.AudioRequest{
				BaseRequest: domainSend.BaseRequest{
					Phone:    "1728937129312@s.whatsapp.net",
					Duration: func() *int { d := -1; return &d }(),
				},
				Audio: audio,
			}},
			err: pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSendAudio(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}
