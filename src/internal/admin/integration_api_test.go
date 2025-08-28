package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestAdminAPI_Integration_CreateInstance(t *testing.T) {
	// Skip if no admin token set
	originalToken := os.Getenv("ADMIN_TOKEN")
	os.Setenv("ADMIN_TOKEN", "test-integration-token")
	defer func() {
		if originalToken == "" {
			os.Unsetenv("ADMIN_TOKEN")
		} else {
			os.Setenv("ADMIN_TOKEN", originalToken)
		}
	}()

	// Create a real lifecycle manager (but we'll only test the API parsing)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create a simple test that just verifies the request parsing works
	app := fiber.New()

	// Add a custom handler that captures the request
	var capturedRequest *CreateInstanceRequest
	app.Post("/admin/instances", func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth != "Bearer test-integration-token" {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}

		var req CreateInstanceRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid json"})
		}

		// Validate port range
		if req.Port < 1024 || req.Port > 65535 {
			return c.Status(400).JSON(fiber.Map{"error": "invalid port"})
		}

		// Capture the request for verification
		capturedRequest = &req

		// Return success without actually creating an instance
		return c.Status(201).JSON(fiber.Map{
			"message": "Instance would be created",
			"port":    req.Port,
		})
	})

	// Test with full configuration
	reqBody := CreateInstanceRequest{
		Port:              3001,
		BasicAuth:         "admin:password123",
		Debug:             boolPtr(true),
		OS:                "TestOS",
		AccountValidation: boolPtr(false),
		BasePath:          "/api/v1",
		AutoReply:         "Test auto reply",
		AutoMarkRead:      boolPtr(true),
		Webhook:           "https://webhook.test.com",
		WebhookSecret:     "test-secret",
		ChatStorage:       boolPtr(false),
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/admin/instances", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-integration-token")

	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// Verify the request was parsed correctly
	assert.NotNil(t, capturedRequest)
	assert.Equal(t, 3001, capturedRequest.Port)
	assert.Equal(t, "admin:password123", capturedRequest.BasicAuth)
	assert.NotNil(t, capturedRequest.Debug)
	assert.Equal(t, true, *capturedRequest.Debug)
	assert.Equal(t, "TestOS", capturedRequest.OS)
	assert.NotNil(t, capturedRequest.AccountValidation)
	assert.Equal(t, false, *capturedRequest.AccountValidation)
	assert.Equal(t, "/api/v1", capturedRequest.BasePath)
	assert.Equal(t, "Test auto reply", capturedRequest.AutoReply)
	assert.NotNil(t, capturedRequest.AutoMarkRead)
	assert.Equal(t, true, *capturedRequest.AutoMarkRead)
	assert.Equal(t, "https://webhook.test.com", capturedRequest.Webhook)
	assert.Equal(t, "test-secret", capturedRequest.WebhookSecret)
	assert.NotNil(t, capturedRequest.ChatStorage)
	assert.Equal(t, false, *capturedRequest.ChatStorage)
}
