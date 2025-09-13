package validations

import (
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

func ValidateCreateWebhook(request *webhook.CreateWebhookRequest) error {
	validEvents := strings.Join(webhook.ValidEvents, ", ")
	
	err := validation.ValidateStruct(request,
		validation.Field(&request.URL,
			validation.Required,
			validation.By(validateWebhookURL),
			is.URL,
		),
		validation.Field(&request.Secret,
			validation.Length(0, 255),
		),
		validation.Field(&request.Events,
			validation.Required,
			validation.Length(1, 0).Error("at least one event must be selected"),
			validation.Each(
				validation.Required,
				validation.By(func(value interface{}) error {
					event, ok := value.(string)
					if !ok {
						return fmt.Errorf("must be a string")
					}
					for _, validEvent := range webhook.ValidEvents {
						if event == validEvent {
							return nil
						}
					}
					return fmt.Errorf("must be one of: %s", validEvents)
				}),
			),
		),
		validation.Field(&request.Description,
			validation.Length(0, 500),
		),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateUpdateWebhook(request *webhook.UpdateWebhookRequest) error {
	validEvents := strings.Join(webhook.ValidEvents, ", ")
	
	err := validation.ValidateStruct(request,
		validation.Field(&request.URL,
			validation.Required,
			validation.By(validateWebhookURL),
			is.URL,
		),
		validation.Field(&request.Secret,
			validation.Length(0, 255),
		),
		validation.Field(&request.Events,
			validation.Required,
			validation.Length(1, 0).Error("at least one event must be selected"),
			validation.Each(
				validation.Required,
				validation.By(func(value interface{}) error {
					event, ok := value.(string)
					if !ok {
						return fmt.Errorf("must be a string")
					}
					for _, validEvent := range webhook.ValidEvents {
						if event == validEvent {
							return nil
						}
					}
					return fmt.Errorf("must be one of: %s", validEvents)
				}),
			),
		),
		validation.Field(&request.Description,
			validation.Length(0, 500),
		),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func validateWebhookURL(value interface{}) error {
	url, ok := value.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}
	
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("must start with http:// or https://")
	}
	
	if strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") {
		return fmt.Errorf("localhost and 127.0.0.1 are not allowed for webhooks")
	}
	
	return nil
}