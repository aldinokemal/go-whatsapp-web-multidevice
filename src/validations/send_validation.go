package validations

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/dustin/go-humanize"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

func ValidateSendMessage(request domainSend.MessageRequest) error {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Message, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

func ValidateSendImage(request domainSend.ImageRequest) error {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Image, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	availableMimes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
	}

	if !availableMimes[request.Image.Header.Get("Content-Type")] {
		return pkgError.ValidationError("your image is not allowed. please use jpg/jpeg/png")
	}

	return nil
}

func ValidateSendFile(request domainSend.FileRequest) error {
	err := validation.ValidateStruct(&request,
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

	return nil
}

func ValidateSendVideo(request domainSend.VideoRequest) error {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Video, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	availableMimes := map[string]bool{
		"video/mp4":        true,
		"video/x-matroska": true,
		"video/avi":        true,
	}

	if !availableMimes[request.Video.Header.Get("Content-Type")] {
		return pkgError.ValidationError("your video type is not allowed. please use mp4/mkv/avi")
	}

	if request.Video.Size > config.WhatsappSettingMaxVideoSize { // 30MB
		maxSizeString := humanize.Bytes(uint64(config.WhatsappSettingMaxVideoSize))
		return pkgError.ValidationError(fmt.Sprintf("max video upload is %s, please upload in cloud and send via text if your file is higher than %s", maxSizeString, maxSizeString))
	}

	return nil
}

func ValidateSendContact(request domainSend.ContactRequest) error {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.ContactPhone, validation.Required),
		validation.Field(&request.ContactName, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateSendLink(request domainSend.LinkRequest) error {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.Link, validation.Required, is.URL),
		validation.Field(&request.Caption, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateRevokeMessage(request domainSend.RevokeRequest) error {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.MessageID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateUpdateMessage(request domainSend.UpdateMessageRequest) error {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
		validation.Field(&request.MessageID, validation.Required),
		validation.Field(&request.Message, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
