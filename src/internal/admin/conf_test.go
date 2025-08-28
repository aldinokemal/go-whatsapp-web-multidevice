package admin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultInstanceConfig(t *testing.T) {
	// Save original environment
	originalConfDir := os.Getenv("SUPERVISOR_CONF_DIR")
	originalInstancesDir := os.Getenv("INSTANCES_DIR")
	originalLogDir := os.Getenv("SUPERVISOR_LOG_DIR")
	originalGowaBin := os.Getenv("GOWA_BIN")

	defer func() {
		// Restore original environment
		os.Setenv("SUPERVISOR_CONF_DIR", originalConfDir)
		os.Setenv("INSTANCES_DIR", originalInstancesDir)
		os.Setenv("SUPERVISOR_LOG_DIR", originalLogDir)
		os.Setenv("GOWA_BIN", originalGowaBin)
	}()

	t.Run("default values", func(t *testing.T) {
		// Clear environment variables
		os.Unsetenv("SUPERVISOR_CONF_DIR")
		os.Unsetenv("INSTANCES_DIR")
		os.Unsetenv("SUPERVISOR_LOG_DIR")
		os.Unsetenv("GOWA_BIN")
		os.Unsetenv("GOWA_DEBUG")
		os.Unsetenv("GOWA_ACCOUNT_VALIDATION")
		os.Unsetenv("GOWA_BASE_PATH")
		os.Unsetenv("GOWA_AUTO_REPLY")
		os.Unsetenv("GOWA_AUTO_MARK_READ")
		os.Unsetenv("GOWA_WEBHOOK")
		os.Unsetenv("GOWA_WEBHOOK_SECRET")
		os.Unsetenv("GOWA_CHAT_STORAGE")

		config := DefaultInstanceConfig()

		assert.Equal(t, "/etc/supervisor/conf.d", config.ConfDir)
		assert.Equal(t, "/app/instances", config.InstancesDir)
		assert.Equal(t, "/var/log/supervisor", config.LogDir)
		assert.Equal(t, "/usr/local/bin/whatsapp", config.GowaBin)
		assert.Equal(t, "admin:admin", config.BasicAuth)
		assert.Equal(t, false, config.Debug)
		assert.Equal(t, "Chrome", config.OS)
		assert.Equal(t, false, config.AccountValidation)
		assert.Equal(t, "", config.BasePath)
		assert.Equal(t, "", config.AutoReply)
		assert.Equal(t, false, config.AutoMarkRead)
		assert.Equal(t, "", config.Webhook)
		assert.Equal(t, "secret", config.WebhookSecret)
		assert.Equal(t, true, config.ChatStorage)
	})

	t.Run("custom values from environment", func(t *testing.T) {
		os.Setenv("SUPERVISOR_CONF_DIR", "/custom/conf")
		os.Setenv("INSTANCES_DIR", "/custom/instances")
		os.Setenv("SUPERVISOR_LOG_DIR", "/custom/logs")
		os.Setenv("GOWA_BIN", "/custom/bin/whatsapp")
		os.Setenv("GOWA_DEBUG", "true")
		os.Setenv("GOWA_ACCOUNT_VALIDATION", "true")
		os.Setenv("GOWA_BASE_PATH", "/api")
		os.Setenv("GOWA_AUTO_REPLY", "Test auto reply")
		os.Setenv("GOWA_AUTO_MARK_READ", "true")
		os.Setenv("GOWA_WEBHOOK", "https://test.webhook.com")
		os.Setenv("GOWA_WEBHOOK_SECRET", "test-secret")
		os.Setenv("GOWA_CHAT_STORAGE", "false")

		config := DefaultInstanceConfig()

		assert.Equal(t, "/custom/conf", config.ConfDir)
		assert.Equal(t, "/custom/instances", config.InstancesDir)
		assert.Equal(t, "/custom/logs", config.LogDir)
		assert.Equal(t, "/custom/bin/whatsapp", config.GowaBin)
		assert.Equal(t, true, config.Debug)
		assert.Equal(t, true, config.AccountValidation)
		assert.Equal(t, "/api", config.BasePath)
		assert.Equal(t, "Test auto reply", config.AutoReply)
		assert.Equal(t, true, config.AutoMarkRead)
		assert.Equal(t, "https://test.webhook.com", config.Webhook)
		assert.Equal(t, "test-secret", config.WebhookSecret)
		assert.Equal(t, false, config.ChatStorage)
	})
}

func TestNewConfigWriter(t *testing.T) {
	config := DefaultInstanceConfig()

	writer, err := NewConfigWriter(config)
	assert.NoError(t, err)
	assert.NotNil(t, writer)
	assert.NotNil(t, writer.tmpl)
	assert.Equal(t, config, writer.config)
}

func TestConfigWriter_WriteConfig(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "admin_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := &InstanceConfig{
		ConfDir:           filepath.Join(tempDir, "conf"),
		InstancesDir:      filepath.Join(tempDir, "instances"),
		LogDir:            filepath.Join(tempDir, "logs"),
		GowaBin:           "/test/bin/whatsapp",
		BasicAuth:         "test:test",
		Debug:             true,
		OS:                "TestOS",
		AccountValidation: true,
		BasePath:          "/api",
		AutoReply:         "Auto reply message",
		AutoMarkRead:      true,
		Webhook:           "https://webhook.site/test",
		WebhookSecret:     "test-secret",
		ChatStorage:       false,
	}

	writer, err := NewConfigWriter(config)
	assert.NoError(t, err)

	port := 3001
	err = writer.WriteConfig(port)
	assert.NoError(t, err)

	// Check that config file was created
	configPath := filepath.Join(config.ConfDir, "gowa-3001.conf")
	assert.FileExists(t, configPath)

	// Check that storage directory was created
	storageDir := filepath.Join(config.InstancesDir, "3001", "storages")
	assert.DirExists(t, storageDir)

	// Check that log directory was created
	assert.DirExists(t, config.LogDir)

	// Read config file and verify content
	content, err := os.ReadFile(configPath)
	assert.NoError(t, err)

	configStr := string(content)
	assert.Contains(t, configStr, "[program:gowa_3001]")
	assert.Contains(t, configStr, "--port=3001")
	assert.Contains(t, configStr, "--debug=true")
	assert.Contains(t, configStr, "--os=TestOS")
	assert.Contains(t, configStr, "--basic-auth=test:test")
	assert.Contains(t, configStr, "--base-path=/api")
	assert.Contains(t, configStr, "--autoreply=\"Auto reply message\"")
	assert.Contains(t, configStr, "--auto-mark-read=true")
	assert.Contains(t, configStr, "--webhook=\"https://webhook.site/test\"")
	assert.Contains(t, configStr, "--webhook-secret=\"test-secret\"")
	assert.Contains(t, configStr, "APP_PORT=\"3001\"")
	assert.Contains(t, configStr, "APP_BASE_PATH=\"/api\"")
	assert.Contains(t, configStr, "WHATSAPP_AUTO_REPLY=\"Auto reply message\"")
	assert.Contains(t, configStr, "WHATSAPP_AUTO_MARK_READ=\"true\"")
	assert.Contains(t, configStr, "WHATSAPP_WEBHOOK=\"https://webhook.site/test\"")
	assert.Contains(t, configStr, "WHATSAPP_WEBHOOK_SECRET=\"test-secret\"")
	assert.Contains(t, configStr, "WHATSAPP_ACCOUNT_VALIDATION=\"true\"")
	assert.Contains(t, configStr, "WHATSAPP_CHAT_STORAGE=\"false\"")
}

func TestConfigWriter_RemoveConfig(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "admin_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := &InstanceConfig{
		ConfDir: filepath.Join(tempDir, "conf"),
	}

	writer, err := NewConfigWriter(config)
	assert.NoError(t, err)

	// Create a config file first
	err = os.MkdirAll(config.ConfDir, 0755)
	assert.NoError(t, err)

	configPath := filepath.Join(config.ConfDir, "gowa-3001.conf")
	err = os.WriteFile(configPath, []byte("test config"), 0644)
	assert.NoError(t, err)

	// Remove the config
	err = writer.RemoveConfig(3001)
	assert.NoError(t, err)

	// Check that file was removed
	assert.NoFileExists(t, configPath)

	// Test removing non-existent file (should not error)
	err = writer.RemoveConfig(3002)
	assert.NoError(t, err)
}

func TestConfigWriter_ConfigExists(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "admin_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := &InstanceConfig{
		ConfDir: filepath.Join(tempDir, "conf"),
	}

	writer, err := NewConfigWriter(config)
	assert.NoError(t, err)

	// Test non-existent config
	assert.False(t, writer.ConfigExists(3001))

	// Create config file
	err = os.MkdirAll(config.ConfDir, 0755)
	assert.NoError(t, err)

	configPath := filepath.Join(config.ConfDir, "gowa-3001.conf")
	err = os.WriteFile(configPath, []byte("test config"), 0644)
	assert.NoError(t, err)

	// Test existing config
	assert.True(t, writer.ConfigExists(3001))
}

func TestLockManager(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "admin_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	lm := &LockManager{
		lockDir: tempDir,
	}

	port := 3001

	// Acquire lock
	lockFile, err := lm.AcquireLock(port)
	assert.NoError(t, err)
	assert.NotNil(t, lockFile)

	// Try to acquire the same lock (should fail)
	lockFile2, err := lm.AcquireLock(port)
	assert.Error(t, err)
	assert.Nil(t, lockFile2)
	assert.Contains(t, err.Error(), "locked by another operation")

	// Release lock
	err = lm.ReleaseLock(lockFile)
	assert.NoError(t, err)

	// Now should be able to acquire the lock again
	lockFile3, err := lm.AcquireLock(port)
	assert.NoError(t, err)
	assert.NotNil(t, lockFile3)

	// Release second lock
	err = lm.ReleaseLock(lockFile3)
	assert.NoError(t, err)
}
