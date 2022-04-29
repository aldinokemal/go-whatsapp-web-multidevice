package validations

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

func ValidateSendMessage(request structs.SendMessageRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, is.Digit, validation.Length(10, 25)),
		validation.Field(&request.Message, validation.Required, validation.Length(1, 50)),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}
}

func ValidateSendImage(request structs.SendImageRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, is.Digit, validation.Length(10, 25)),
		validation.Field(&request.Caption, validation.When(true, validation.Length(1, 200))),
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

func ValidateSendFile(request structs.SendFileRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required, is.Digit, validation.Length(10, 25)),
		validation.Field(&request.File, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}

	if request.File.Size > 10240000 { // 10MB
		panic(utils.ValidationError{
			Message: "max file upload is 10MB, please upload in cloud and send via text if your file is higher than 10MB",
		})
	}
}
