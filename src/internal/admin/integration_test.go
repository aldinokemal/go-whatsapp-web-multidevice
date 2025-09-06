package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompleteConfigurationIntegration(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "admin_integration_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set up comprehensive environment variables
	envVars := map[string]string{
		"SUPERVISOR_CONF_DIR":     filepath.Join(tempDir, "conf"),
		"INSTANCES_DIR":           filepath.Join(tempDir, "instances"),
		"SUPERVISOR_LOG_DIR":      filepath.Join(tempDir, "logs"),
		"GOWA_BIN":                "/usr/local/bin/whatsapp",
		"GOWA_BASIC_AUTH":         "admin:password123",
		"GOWA_DEBUG":              "true",
		"GOWA_OS":                 "AdminAPI",
		"GOWA_ACCOUNT_VALIDATION": "true",
		"GOWA_BASE_PATH":          "/api/v1",
		"GOWA_AUTO_REPLY":         "This is an auto-reply from admin API instance",
		"GOWA_AUTO_MARK_READ":     "true",
		"GOWA_WEBHOOK":            "https://webhook.example.com/whatsapp",
		"GOWA_WEBHOOK_SECRET":     "super-secret-webhook-key",
		"GOWA_CHAT_STORAGE":       "false",
		"LOCK_DIR":                filepath.Join(tempDir, "locks"),
	}

	// Store original values and set test values
	originalValues := make(map[string]string)
	for key, value := range envVars {
		originalValues[key] = os.Getenv(key)
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

	// Create configuration and writer
	config := DefaultInstanceConfig()
	writer, err := NewConfigWriter(config)
	assert.NoError(t, err)

	// Write configuration for port 3001
	port := 3001
	err = writer.WriteConfig(port)
	assert.NoError(t, err)

	// Read the generated configuration
	configPath := filepath.Join(config.ConfDir, "gowa-3001.conf")
	content, err := os.ReadFile(configPath)
	assert.NoError(t, err)

	configStr := string(content)

	t.Run("verify_program_section", func(t *testing.T) {
		assert.Contains(t, configStr, "[program:gowa_3001]")
	})

	t.Run("verify_command_line_flags", func(t *testing.T) {
		// Core flags
		assert.Contains(t, configStr, "--port=3001")
		assert.Contains(t, configStr, "--debug=true")
		assert.Contains(t, configStr, "--os=AdminAPI")
		assert.Contains(t, configStr, "--account-validation=true")
		assert.Contains(t, configStr, "--basic-auth=admin:password123")

		// Additional flags
		assert.Contains(t, configStr, "--base-path=/api/v1")
		assert.Contains(t, configStr, "--autoreply=\"This is an auto-reply from admin API instance\"")
		assert.Contains(t, configStr, "--auto-mark-read=true")
		assert.Contains(t, configStr, "--webhook=\"https://webhook.example.com/whatsapp\"")
		assert.Contains(t, configStr, "--webhook-secret=\"super-secret-webhook-key\"")
	})

	t.Run("verify_environment_variables", func(t *testing.T) {
		// Core environment variables that map to APP_* variables
		assert.Contains(t, configStr, "APP_PORT=\"3001\"")
		assert.Contains(t, configStr, "APP_DEBUG=\"true\"")
		assert.Contains(t, configStr, "APP_OS=\"AdminAPI\"")
		assert.Contains(t, configStr, "APP_BASIC_AUTH=\"admin:password123\"")
		assert.Contains(t, configStr, "DB_URI=\"file:storages/whatsapp.db?_foreign_keys=on\"")

		// Extended environment variables
		assert.Contains(t, configStr, "APP_BASE_PATH=\"/api/v1\"")
		assert.Contains(t, configStr, "WHATSAPP_AUTO_REPLY=\"This is an auto-reply from admin API instance\"")
		assert.Contains(t, configStr, "WHATSAPP_AUTO_MARK_READ=\"true\"")
		assert.Contains(t, configStr, "WHATSAPP_WEBHOOK=\"https://webhook.example.com/whatsapp\"")
		assert.Contains(t, configStr, "WHATSAPP_WEBHOOK_SECRET=\"super-secret-webhook-key\"")
		assert.Contains(t, configStr, "WHATSAPP_ACCOUNT_VALIDATION=\"true\"")
		assert.Contains(t, configStr, "WHATSAPP_CHAT_STORAGE=\"false\"")
	})

	t.Run("verify_supervisor_settings", func(t *testing.T) {
		assert.Contains(t, configStr, "autostart=true")
		assert.Contains(t, configStr, "autorestart=true")
		assert.Contains(t, configStr, "startretries=3")
		assert.Contains(t, configStr, "stdout_logfile="+filepath.Join(tempDir, "logs", "gowa_3001.out.log"))
		assert.Contains(t, configStr, "stderr_logfile="+filepath.Join(tempDir, "logs", "gowa_3001.err.log"))
		assert.Contains(t, configStr, "directory="+filepath.Join(tempDir, "instances", "3001"))
	})

	t.Run("verify_all_readme_env_vars_supported", func(t *testing.T) {
		// This test ensures that all environment variables from the README
		// are represented in our generated configuration

		readmeEnvVars := []string{
			"APP_PORT", "APP_DEBUG", "APP_OS", "APP_BASIC_AUTH", "APP_BASE_PATH",
			"DB_URI", "WHATSAPP_AUTO_REPLY", "WHATSAPP_AUTO_MARK_READ",
			"WHATSAPP_WEBHOOK", "WHATSAPP_WEBHOOK_SECRET",
			"WHATSAPP_ACCOUNT_VALIDATION", "WHATSAPP_CHAT_STORAGE",
		}

		for _, envVar := range readmeEnvVars {
			assert.True(t, strings.Contains(configStr, envVar),
				"Environment variable %s should be present in config", envVar)
		}
	})

	t.Run("verify_directory_creation", func(t *testing.T) {
		// Check that all required directories were created
		assert.DirExists(t, config.ConfDir)
		assert.DirExists(t, config.LogDir)
		assert.DirExists(t, filepath.Join(config.InstancesDir, "3001", "storages"))
	})
}
