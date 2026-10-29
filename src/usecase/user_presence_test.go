package usecase

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

func TestSubscribePresenceRejectsBlankPhoneBeforeClient(t *testing.T) {
	original := config.WhatsappAccountValidation
	t.Cleanup(func() { config.WhatsappAccountValidation = original })
	for _, enabled := range []bool{false, true} {
		config.WhatsappAccountValidation = enabled
		for _, phone := range []string{"", " ", "\t\n"} {
			response, err := NewUserService(nil).SubscribePresence(context.Background(), domainUser.SubscribePresenceRequest{Phone: phone})
			var validationErr pkgError.ValidationError
			require.ErrorAs(t, err, &validationErr)
			require.False(t, response.Subscribed)
		}
	}
}
