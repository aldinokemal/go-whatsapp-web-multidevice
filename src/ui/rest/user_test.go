package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type detailedContactsServiceStub struct {
	domainUser.IUserUsecase
	calls  int
	device *whatsapp.DeviceInstance
}

func (s *detailedContactsServiceStub) MyListContactsDetailed(ctx context.Context) (domainUser.MyListContactsDetailedResponse, error) {
	s.calls++
	s.device, _ = whatsapp.DeviceFromContext(ctx)
	return domainUser.MyListContactsDetailedResponse{Data: []domainUser.MyListContactsDetailedData{
		{JID: "628123456789@s.whatsapp.net", FullName: "Alice Example", PushName: "Ali"},
	}}, nil
}

func TestUserMyListContactsDetailedUsesTheRequestDevice(t *testing.T) {
	device := whatsapp.NewDeviceInstance("device-a", nil, nil)
	service := &detailedContactsServiceStub{}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("device", device)
		return c.Next()
	})
	InitRestUser(app, service)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/user/my/contacts/detailed", nil))
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Equal(t, 1, service.calls)
	assert.Same(t, device, service.device)

	var envelope struct {
		Code    string                                    `json:"code"`
		Message string                                    `json:"message"`
		Results domainUser.MyListContactsDetailedResponse `json:"results"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	assert.Equal(t, "SUCCESS", envelope.Code)
	assert.Equal(t, "Success get detailed contacts", envelope.Message)
	require.Len(t, envelope.Results.Data, 1)
	assert.Equal(t, "Alice Example", envelope.Results.Data[0].FullName)
	assert.Equal(t, "Ali", envelope.Results.Data[0].PushName)
}
