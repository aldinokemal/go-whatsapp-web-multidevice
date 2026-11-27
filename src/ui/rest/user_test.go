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

type userServiceWithoutDetailedContacts struct {
	domainUser.IUserUsecase
}

func TestUserMyListContactsDetailedIsNotRegisteredWithoutTheCapability(t *testing.T) {
	app := fiber.New()
	InitRestUser(app, userServiceWithoutDetailedContacts{})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/user/my/contacts/detailed", nil))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
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

type labelServiceStub struct {
	domainUser.IUserUsecase
	calls   []string
	labelID string
	device  *whatsapp.DeviceInstance
}

func (s *labelServiceStub) MyListLabels(ctx context.Context) (domainUser.MyListLabelsResponse, error) {
	s.calls = append(s.calls, "list")
	s.device, _ = whatsapp.DeviceFromContext(ctx)
	return domainUser.MyListLabelsResponse{Data: []domainUser.MyListLabelsResponseData{{LabelID: "5", Name: "Paid", ChatCount: 2}}}, nil
}

func (s *labelServiceStub) MySyncLabels(ctx context.Context) (domainUser.MyListLabelsResponse, error) {
	s.calls = append(s.calls, "sync")
	s.device, _ = whatsapp.DeviceFromContext(ctx)
	return domainUser.MyListLabelsResponse{Data: []domainUser.MyListLabelsResponseData{}}, nil
}

func (s *labelServiceStub) MyListLabelChats(ctx context.Context, labelID string) (domainUser.MyListLabelChatsResponse, error) {
	s.calls = append(s.calls, "chats")
	s.labelID = labelID
	s.device, _ = whatsapp.DeviceFromContext(ctx)
	return domainUser.MyListLabelChatsResponse{LabelID: labelID, Data: []domainUser.MyListLabelChatsResponseData{
		{JID: "628123456789@s.whatsapp.net", LID: "123456789012345@lid"},
	}}, nil
}

func TestUserLabelRoutesUseTheRequestDevice(t *testing.T) {
	device := whatsapp.NewDeviceInstance("device-a", nil, nil)
	service := &labelServiceStub{}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("device", device)
		return c.Next()
	})
	InitRestUser(app, service)

	for _, tc := range []struct {
		method, path, call, message string
	}{
		{http.MethodGet, "/user/my/labels", "list", "Success get list labels"},
		{http.MethodPost, "/user/my/labels/sync", "sync", "Success sync labels"},
		{http.MethodGet, "/user/my/labels/5/chats", "chats", "Success get label chats"},
	} {
		service.calls, service.device = nil, nil
		resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		assert.Equal(t, []string{tc.call}, service.calls, tc.path)
		assert.Same(t, device, service.device, tc.path)

		var envelope struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &envelope))
		assert.Equal(t, "SUCCESS", envelope.Code)
		assert.Equal(t, tc.message, envelope.Message)
	}
	assert.Equal(t, "5", service.labelID)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/user/my/labels/5/chats", nil))
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"label_id":"5","data":[{"jid":"628123456789@s.whatsapp.net","lid":"123456789012345@lid"}]}`,
		string(mustResults(t, body)))
}

func mustResults(t *testing.T, body []byte) json.RawMessage {
	t.Helper()
	var envelope struct {
		Results json.RawMessage `json:"results"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Results
}
