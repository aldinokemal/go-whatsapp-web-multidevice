package validations

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
	"strings"
)

func ValidateSendMessage(request structs.SendMessageRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.PhoneNumber, validation.Required, is.E164, validation.Length(10, 15)),
		validation.Field(&request.Message, validation.Required, validation.Length(4, 50)),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	} else if !strings.HasPrefix(request.PhoneNumber, "62") {
		panic(utils.ValidationError{
			Message: "this is only work for indonesia country (start with 62)",
		})
	}
}

func ValidateSendImage(request structs.SendImageRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.PhoneNumber, validation.Required, is.E164, validation.Length(10, 15)),
		validation.Field(&request.Caption, validation.When(true, validation.Length(4, 200))),
		validation.Field(&request.Image, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	} else if !strings.HasPrefix(request.PhoneNumber, "62") {
		panic(utils.ValidationError{
			Message: "this is only work for indonesia country (start with 62)",
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
