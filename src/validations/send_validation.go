package validations

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/dustin/go-humanize"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateSendMessage(request domainSend.MessageRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, validation.Length(10, 25)),
		validation.Field(&request.Message, validation.Required, validation.Length(1, 1000)),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}
}

func ValidateSendImage(request domainSend.ImageRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, validation.Length(10, 25)),
		validation.Field(&request.Caption, validation.When(true, validation.Length(1, 1000))),
		validation.Field(&request.Image, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}

	availableMimes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
	}

	if !availableMimes[request.Image.Header.Get("Content-Type")] {
		panic(utils.ValidationError{
			Message: "your image is not allowed. please use jpg/jpeg/png",
		})
	}
}

func ValidateSendFile(request domainSend.FileRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, validation.Length(10, 25)),
		validation.Field(&request.File, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}

	if request.File.Size > config.WhatsappSettingMaxFileSize { // 10MB
		maxSizeString := humanize.Bytes(uint64(config.WhatsappSettingMaxFileSize))
		panic(utils.ValidationError{
			Message: fmt.Sprintf("max file upload is %s, please upload in cloud and send via text if your file is higher than %s", maxSizeString, maxSizeString),
		})
	}
}

func ValidateSendVideo(request domainSend.VideoRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, validation.Length(10, 25)),
		validation.Field(&request.Video, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}

	availableMimes := map[string]bool{
		"video/mp4":        true,
		"video/x-matroska": true,
		"video/avi":        true,
	}

	if !availableMimes[request.Video.Header.Get("Content-Type")] {
		panic(utils.ValidationError{
			Message: "your video type is not allowed. please use mp4/mkv",
		})
	}

	if request.Video.Size > config.WhatsappSettingMaxVideoSize { // 30MB
		maxSizeString := humanize.Bytes(uint64(config.WhatsappSettingMaxVideoSize))
		panic(utils.ValidationError{
			Message: fmt.Sprintf("max video upload is %s, please upload in cloud and send via text if your file is higher than %s", maxSizeString, maxSizeString),
		})
	}
}

func ValidateSendContact(request domainSend.ContactRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, validation.Length(10, 25)),
		validation.Field(&request.ContactName, validation.Required),
		validation.Field(&request.ContactPhone, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}
}
