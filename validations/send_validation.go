package validations

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/go-ozzo/ozzo-validation/v4"
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
