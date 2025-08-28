package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func TestAdminAPI_EndToEnd_ConfigGeneration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "admin_e2e_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set up environment variables - clear any existing ones first
	clearVars := []string{
		"GOWA_BASE_PATH", "GOWA_AUTO_REPLY", "GOWA_WEBHOOK", "GOWA_AUTO_MARK_READ",
	}
	for _, v := range clearVars {
		os.Unsetenv(v)
	}

	envVars := map[string]string{
		"ADMIN_TOKEN":             "test-e2e-token",
		"SUPERVISOR_CONF_DIR":     filepath.Join(tempDir, "conf"),
		"INSTANCES_DIR":           filepath.Join(tempDir, "instances"),
		"SUPERVISOR_LOG_DIR":      filepath.Join(tempDir, "logs"),
		"GOWA_BIN":                "/usr/local/bin/whatsapp",
		"GOWA_BASIC_AUTH":         "default:auth",
		"GOWA_DEBUG":              "false",
		"GOWA_OS":                 "Chrome",
		"GOWA_ACCOUNT_VALIDATION": "true",
		"GOWA_WEBHOOK_SECRET":     "default-secret",
		"GOWA_CHAT_STORAGE":       "true",
		"LOCK_DIR":                filepath.Join(tempDir, "locks"),
	}

	// Store original values and set test values
	originalValues := make(map[string]string)
	allVars := append(clearVars, []string{
		"ADMIN_TOKEN", "SUPERVISOR_CONF_DIR", "INSTANCES_DIR", "SUPERVISOR_LOG_DIR",
		"GOWA_BIN", "GOWA_BASIC_AUTH", "GOWA_DEBUG", "GOWA_OS", "GOWA_ACCOUNT_VALIDATION",
		"GOWA_WEBHOOK_SECRET", "GOWA_CHAT_STORAGE", "LOCK_DIR",
	}...)

	for _, key := range allVars {
		originalValues[key] = os.Getenv(key)
	}

	for key, value := range envVars {
		os.Setenv(key, value)
	}

	// Restore original environment after test
	defer func() {
		for key, originalValue := range originalValues {
			if originalValue == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, originalValue)
			}
		}
	}()

	// Create a custom test handler that actually generates config files
	app := fiber.New()

	app.Post("/admin/instances", func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth != "Bearer test-e2e-token" {
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

		// Create configuration based on request
		var config *InstanceConfig
		if hasCustomConfig(req) {
			config = buildInstanceConfig(req)
		} else {
			config = DefaultInstanceConfig()
		}

		config.Port = req.Port

		// Write the configuration file to verify it works
		configWriter, err := NewConfigWriter(config)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to create config writer"})
		}

		if err := configWriter.WriteConfig(req.Port); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "failed to write config"})
		}

		return c.Status(201).JSON(fiber.Map{
			"message": "Instance config generated successfully",
			"port":    req.Port,
		})
	})

	// Test 1: Create instance with custom configuration
	t.Run("create instance with custom config", func(t *testing.T) {
		reqBody := CreateInstanceRequest{
			Port:              3001,
			BasicAuth:         "custom:password",
			Debug:             boolPtr(true),
			OS:                "CustomOS",
			AccountValidation: boolPtr(false),
			BasePath:          "/api/v1",
			AutoReply:         "Custom auto reply message",
			AutoMarkRead:      boolPtr(true),
			Webhook:           "https://custom.webhook.com",
			WebhookSecret:     "custom-secret",
			ChatStorage:       boolPtr(false),
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/admin/instances", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-e2e-token")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Verify the config file was created and contains expected content
		configPath := filepath.Join(tempDir, "conf", "gowa-3001.conf")
		assert.FileExists(t, configPath)

		content, err := os.ReadFile(configPath)
		assert.NoError(t, err)

		configStr := string(content)

		// Verify CLI flags
		assert.Contains(t, configStr, "--port=3001")
		assert.Contains(t, configStr, "--debug=true")
		assert.Contains(t, configStr, "--os=CustomOS")
		assert.Contains(t, configStr, "--account-validation=false")
		assert.Contains(t, configStr, "--basic-auth=custom:password")
		assert.Contains(t, configStr, "--base-path=/api/v1")
		assert.Contains(t, configStr, "--autoreply=\"Custom auto reply message\"")
		assert.Contains(t, configStr, "--auto-mark-read=true")
		assert.Contains(t, configStr, "--webhook=\"https://custom.webhook.com\"")
		assert.Contains(t, configStr, "--webhook-secret=\"custom-secret\"")

		// Verify environment variables
		assert.Contains(t, configStr, "APP_PORT=\"3001\"")
		assert.Contains(t, configStr, "APP_DEBUG=\"true\"")
		assert.Contains(t, configStr, "APP_OS=\"CustomOS\"")
		assert.Contains(t, configStr, "APP_BASIC_AUTH=\"custom:password\"")
		assert.Contains(t, configStr, "APP_BASE_PATH=\"/api/v1\"")
		assert.Contains(t, configStr, "WHATSAPP_AUTO_REPLY=\"Custom auto reply message\"")
		assert.Contains(t, configStr, "WHATSAPP_AUTO_MARK_READ=\"true\"")
		assert.Contains(t, configStr, "WHATSAPP_WEBHOOK=\"https://custom.webhook.com\"")
		assert.Contains(t, configStr, "WHATSAPP_WEBHOOK_SECRET=\"custom-secret\"")
		assert.Contains(t, configStr, "WHATSAPP_ACCOUNT_VALIDATION=\"false\"")
		assert.Contains(t, configStr, "WHATSAPP_CHAT_STORAGE=\"false\"")

		// Verify supervisor settings
		assert.Contains(t, configStr, "[program:gowa_3001]")
		assert.Contains(t, configStr, "autostart=true")
		assert.Contains(t, configStr, "autorestart=true")

		// Verify directory structure was created
		storageDir := filepath.Join(tempDir, "instances", "3001", "storages")
		assert.DirExists(t, storageDir)
	})

	// Test 2: Create instance with minimal configuration
	t.Run("create instance with minimal config", func(t *testing.T) {
		reqBody := CreateInstanceRequest{
			Port: 3002,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/admin/instances", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-e2e-token")

		resp, err := app.Test(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Verify the config file was created with default values
		configPath := filepath.Join(tempDir, "conf", "gowa-3002.conf")
		assert.FileExists(t, configPath)

		content, err := os.ReadFile(configPath)
		assert.NoError(t, err)

		configStr := string(content)

		// Verify default values are used
		assert.Contains(t, configStr, "--port=3002")
		assert.Contains(t, configStr, "--basic-auth=default:auth")
		assert.Contains(t, configStr, "--debug=false")
		assert.Contains(t, configStr, "--os=Chrome")
		assert.Contains(t, configStr, "--account-validation=true")

		// Should not contain optional flags that weren't set
		assert.NotContains(t, configStr, "--base-path")
		assert.NotContains(t, configStr, "--autoreply")
		assert.NotContains(t, configStr, "--webhook=")
	})
}

// Helper functions for the test
func hasCustomConfig(req CreateInstanceRequest) bool {
	return req.BasicAuth != "" ||
		req.Debug != nil ||
		req.OS != "" ||
		req.AccountValidation != nil ||
		req.BasePath != "" ||
		req.AutoReply != "" ||
		req.AutoMarkRead != nil ||
		req.Webhook != "" ||
		req.WebhookSecret != "" ||
		req.ChatStorage != nil
}

func buildInstanceConfig(req CreateInstanceRequest) *InstanceConfig {
	config := DefaultInstanceConfig()

	if req.BasicAuth != "" {
		config.BasicAuth = req.BasicAuth
	}
	if req.Debug != nil {
		config.Debug = *req.Debug
	}
	if req.OS != "" {
		config.OS = req.OS
	}
	if req.AccountValidation != nil {
		config.AccountValidation = *req.AccountValidation
	}
	if req.BasePath != "" {
		config.BasePath = req.BasePath
	}
	if req.AutoReply != "" {
		config.AutoReply = req.AutoReply
	}
	if req.AutoMarkRead != nil {
		config.AutoMarkRead = *req.AutoMarkRead
	}
	if req.Webhook != "" {
		config.Webhook = req.Webhook
	}
	if req.WebhookSecret != "" {
		config.WebhookSecret = req.WebhookSecret
	}
	if req.ChatStorage != nil {
		config.ChatStorage = *req.ChatStorage
	}

	return config
}

func boolPtr(b bool) *bool {
	return &b
}
