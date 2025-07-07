package validations

import (
	"context"
	"fmt"
	"sort"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/dustin/go-humanize"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

// maxDuration represents the maximum allowed duration in seconds (uint32 max).
const maxDuration int64 = 4294967295

func ValidateSendMessage(ctx context.Context, request domainSend.MessageRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Message, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	// Custom validation for optional Duration
	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}
	return nil
}

func ValidateSendImage(ctx context.Context, request domainSend.ImageRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	if request.Image == nil && (request.ImageURL == nil || *request.ImageURL == "") {
		return pkgError.ValidationError("either Image or ImageURL must be provided")
	}

	if request.Image != nil {
		availableMimes := map[string]bool{
			"image/jpeg": true,
			"image/jpg":  true,
			"image/png":  true,
		}

		if !availableMimes[request.Image.Header.Get("Content-Type")] {
			return pkgError.ValidationError("your image is not allowed. please use jpg/jpeg/png")
		}
	}

	if request.ImageURL != nil {
		if *request.ImageURL == "" {
			return pkgError.ValidationError("ImageURL cannot be empty")
		}

		err := validation.Validate(*request.ImageURL, is.URL)
		if err != nil {
			return pkgError.ValidationError("ImageURL must be a valid URL")
		}
	}

	// Validate duration
	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	return nil
}

func ValidateSendFile(ctx context.Context, request domainSend.FileRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.File, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	if request.File.Size > config.WhatsappSettingMaxFileSize { // 10MB
		maxSizeString := humanize.Bytes(uint64(config.WhatsappSettingMaxFileSize))
		return pkgError.ValidationError(fmt.Sprintf("max file upload is %s, please upload in cloud and send via text if your file is higher than %s", maxSizeString, maxSizeString))
	}

	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	return nil
}

func ValidateSendVideo(ctx context.Context, request domainSend.VideoRequest) error {
	// Validate common required fields
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	// Ensure at least one of Video or VideoURL is provided
	if request.Video == nil && (request.VideoURL == nil || *request.VideoURL == "") {
		return pkgError.ValidationError("either Video or VideoURL must be provided")
	}

	// If Video file provided perform MIME / size validation
	if request.Video != nil {
		availableMimes := map[string]bool{
			"video/mp4":        true,
			"video/x-matroska": true,
			"video/avi":        true,
			"video/x-msvideo":  true,
		}

		if !availableMimes[request.Video.Header.Get("Content-Type")] {
			return pkgError.ValidationError("your video type is not allowed. please use mp4/mkv/avi/x-msvideo")
		}

		if request.Video.Size > config.WhatsappSettingMaxVideoSize { // 30MB
			maxSizeString := humanize.Bytes(uint64(config.WhatsappSettingMaxVideoSize))
			return pkgError.ValidationError(fmt.Sprintf("max video upload is %s, please upload in cloud and send via text if your file is higher than %s", maxSizeString, maxSizeString))
		}
	}

	// If VideoURL provided, validate url
	if request.VideoURL != nil {
		if *request.VideoURL == "" {
			return pkgError.ValidationError("VideoURL cannot be empty")
		}

		if err := validation.Validate(*request.VideoURL, is.URL); err != nil {
			return pkgError.ValidationError("VideoURL must be a valid URL")
		}
	}

	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	return nil
}

func ValidateSendContact(ctx context.Context, request domainSend.ContactRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.ContactPhone, validation.Required),
		validation.Field(&request.ContactName, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	return nil
}

func ValidateSendLink(ctx context.Context, request domainSend.LinkRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Link, validation.Required, is.URL),
		validation.Field(&request.Caption, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	return nil
}

func ValidateSendLocation(ctx context.Context, request domainSend.LocationRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Latitude, validation.Required, is.Latitude),
		validation.Field(&request.Longitude, validation.Required, is.Longitude),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	return nil
}

func ValidateSendAudio(ctx context.Context, request domainSend.AudioRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	// Ensure at least one of Audio or AudioURL is provided
	if request.Audio == nil && (request.AudioURL == nil || *request.AudioURL == "") {
		return pkgError.ValidationError("either Audio or AudioURL must be provided")
	}

	// If Audio file is provided, validate file MIME
	if request.Audio != nil {
		availableMimes := map[string]bool{
			"audio/aac":      true,
			"audio/amr":      true,
			"audio/flac":     true,
			"audio/m4a":      true,
			"audio/m4r":      true,
			"audio/mp3":      true,
			"audio/mpeg":     true,
			"audio/ogg":      true,
			"audio/wma":      true,
			"audio/x-ms-wma": true,
			"audio/wav":      true,
			"audio/vnd.wav":  true,
			"audio/vnd.wave": true,
			"audio/wave":     true,
			"audio/x-pn-wav": true,
			"audio/x-wav":    true,
		}
		availableMimesStr := ""

		// Sort MIME types for consistent error message order
		mimeKeys := make([]string, 0, len(availableMimes))
		for k := range availableMimes {
			mimeKeys = append(mimeKeys, k)
		}
		sort.Strings(mimeKeys)

		for _, k := range mimeKeys {
			availableMimesStr += k + ","
		}

		if !availableMimes[request.Audio.Header.Get("Content-Type")] {
			return pkgError.ValidationError(fmt.Sprintf("your audio type is not allowed. please use (%s)", availableMimesStr))
		}
	}

	// If AudioURL provided, basic URL validation
	if request.AudioURL != nil {
		if *request.AudioURL == "" {
			return pkgError.ValidationError("AudioURL cannot be empty")
		}

		if err := validation.Validate(*request.AudioURL, is.URL); err != nil {
			return pkgError.ValidationError("AudioURL must be a valid URL")
		}
	}

	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	return nil
}

func ValidateSendPoll(ctx context.Context, request domainSend.PollRequest) error {
	// Validate options first to ensure it is not blank before validating MaxAnswer
	if len(request.Options) == 0 {
		return pkgError.ValidationError("options: cannot be blank.")
	}

	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Question, validation.Required),

		validation.Field(&request.Options, validation.Each(validation.Required)),

		validation.Field(&request.MaxAnswer, validation.Required),
		validation.Field(&request.MaxAnswer, validation.Min(1)),
		validation.Field(&request.MaxAnswer, validation.Max(len(request.Options))),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	if request.Duration != nil && (*request.Duration < 0 || int64(*request.Duration) > maxDuration) {
		return pkgError.ValidationError("duration must be between 0 and 4294967295 seconds (0 means no expiry)")
	}

	// validate options should be unique each other
	uniqueOptions := make(map[string]bool)
	for _, option := range request.Options {
		if _, ok := uniqueOptions[option]; ok {
			return pkgError.ValidationError("options should be unique")
		}
		uniqueOptions[option] = true
	}

	return nil
}

func ValidateSendPresence(ctx context.Context, request domainSend.PresenceRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Type, validation.In("available", "unavailable")),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
