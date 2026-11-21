package rest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestPathDeviceID_DecodesPercentEncodedSpace(t *testing.T) {
	app := fiber.New()
	app.Get("/devices/:device_id", func(c fiber.Ctx) error {
		return c.SendString(pathDeviceID(c))
	})

	req := httptest.NewRequest(http.MethodGet, "/devices/Estar%20Siempre", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Estar Siempre" {
		t.Fatalf("expected decoded device id, got %q", body)
	}
}
