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
	"github.com/stretchr/testify/mock"
)

func TestAdminAPI_CreateInstance_Simple(t *testing.T) {
	// Set required environment variable
	os.Setenv("ADMIN_TOKEN", "test-token")
	defer os.Unsetenv("ADMIN_TOKEN")

	mockLifecycle := &MockLifecycleManager{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	api, err := NewAdminAPI(mockLifecycle, logger)
	assert.NoError(t, err)

	expectedInstance := &Instance{
		Port:  3001,
		State: StateRunning,
	}

	// Test 1: Minimal config - should call CreateInstance
	t.Run("minimal config calls CreateInstance", func(t *testing.T) {
		mockLifecycle.On("CreateInstance", 3001).Return(expectedInstance, nil).Once()

		app := fiber.New()
		api.SetupRoutes(app)

		reqBody := CreateInstanceRequest{Port: 3001}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/admin/instances", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		mockLifecycle.AssertExpectations(t)
	})

	// Test 2: Custom config - should call CreateInstanceWithConfig
	t.Run("custom config calls CreateInstanceWithConfig", func(t *testing.T) {
		// Use mock.Anything for the config to avoid strict matching issues
		mockLifecycle.On("CreateInstanceWithConfig", 3002, mock.Anything).Return(expectedInstance, nil).Once()

		app := fiber.New()
		api.SetupRoutes(app)

		reqBody := CreateInstanceRequest{
			Port:      3002,
			BasicAuth: "custom:auth",
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/admin/instances", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		mockLifecycle.AssertExpectations(t)
	})
}
