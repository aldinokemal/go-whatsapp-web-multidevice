package validations

import (
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateUserInfo(request domainUser.InfoRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}
}
func ValidateUserAvatar(request domainUser.AvatarRequest) {
	err := validation.ValidateStruct(&request,
		validation.Field(&request.Phone, validation.Required),
	)

	if err != nil {
		panic(utils.ValidationError{
			Message: err.Error(),
		})
	}
}
