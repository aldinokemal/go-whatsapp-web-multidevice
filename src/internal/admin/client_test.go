package admin

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewSupervisorClient(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name: "valid HTTP URL",
			config: &Config{
				URL:      "http://localhost:9001/RPC2",
				Username: "admin",
				Password: "password",
			},
			expectError: false,
		},
		{
			name: "valid Unix socket URL",
			config: &Config{
				URL:      "unix:///var/run/supervisor.sock",
				Username: "admin",
				Password: "password",
			},
			expectError: false,
		},
		{
			name: "HTTP URL without auth",
			config: &Config{
				URL: "http://localhost:9001/RPC2",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewSupervisorClient(tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				assert.NotNil(t, client.GetClient())
			}
		})
	}
}

func TestNewSupervisorClientFromEnv(t *testing.T) {
	// Save original environment
	originalURL := os.Getenv("SUPERVISOR_URL")
	originalUser := os.Getenv("SUPERVISOR_USER")
	originalPass := os.Getenv("SUPERVISOR_PASS")

	defer func() {
		// Restore original environment
		os.Setenv("SUPERVISOR_URL", originalURL)
		os.Setenv("SUPERVISOR_USER", originalUser)
		os.Setenv("SUPERVISOR_PASS", originalPass)
	}()

	t.Run("default configuration", func(t *testing.T) {
		os.Unsetenv("SUPERVISOR_URL")
		os.Unsetenv("SUPERVISOR_USER")
		os.Unsetenv("SUPERVISOR_PASS")

		client, err := NewSupervisorClientFromEnv()
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("custom configuration", func(t *testing.T) {
		os.Setenv("SUPERVISOR_URL", "http://custom:9001/RPC2")
		os.Setenv("SUPERVISOR_USER", "testuser")
		os.Setenv("SUPERVISOR_PASS", "testpass")

		client, err := NewSupervisorClientFromEnv()
		assert.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("invalid URL", func(t *testing.T) {
		os.Setenv("SUPERVISOR_URL", ":")

		client, err := NewSupervisorClientFromEnv()
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "invalid SUPERVISOR_URL")
	})
}

func TestGetEnvOrDefault(t *testing.T) {
	key := "TEST_ENV_VAR_ADMIN"
	defaultValue := "default-value"

	// Test with unset environment variable
	os.Unsetenv(key)
	result := getEnvOrDefault(key, defaultValue)
	assert.Equal(t, defaultValue, result)

	// Test with set environment variable
	testValue := "test-value"
	os.Setenv(key, testValue)
	defer os.Unsetenv(key)

	result = getEnvOrDefault(key, defaultValue)
	assert.Equal(t, testValue, result)

	// Test with empty environment variable
	os.Setenv(key, "")
	result = getEnvOrDefault(key, defaultValue)
	assert.Equal(t, defaultValue, result)
}
