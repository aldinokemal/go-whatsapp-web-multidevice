package admin

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/abrander/go-supervisord"
)

// SupervisorClient wraps the supervisord client with configuration
type SupervisorClient struct {
	client *supervisord.Client
	config *Config
}

// Config holds the configuration for Supervisor client
type Config struct {
	URL      string
	Username string
	Password string
}

// NewSupervisorClient creates a new Supervisor client based on configuration
func NewSupervisorClient(config *Config) (*SupervisorClient, error) {
	var client *supervisord.Client
	var err error

	// Check if URL is a Unix socket
	if strings.HasPrefix(config.URL, "unix://") {
		socketPath := strings.TrimPrefix(config.URL, "unix://")
		if config.Username != "" && config.Password != "" {
			client, err = supervisord.NewUnixSocketClient(socketPath,
				supervisord.WithAuthentication(config.Username, config.Password))
		} else {
			client, err = supervisord.NewUnixSocketClient(socketPath)
		}
	} else {
		// HTTP/HTTPS URL
		if config.Username != "" && config.Password != "" {
			client, err = supervisord.NewClient(config.URL,
				supervisord.WithAuthentication(config.Username, config.Password))
		} else {
			client, err = supervisord.NewClient(config.URL)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create supervisord client: %w", err)
	}

	return &SupervisorClient{
		client: client,
		config: config,
	}, nil
}

// NewSupervisorClientFromEnv creates a Supervisor client from environment variables
func NewSupervisorClientFromEnv() (*SupervisorClient, error) {
	config := &Config{
		URL:      getEnvOrDefault("SUPERVISOR_URL", "http://127.0.0.1:9001/RPC2"),
		Username: os.Getenv("SUPERVISOR_USER"),
		Password: os.Getenv("SUPERVISOR_PASS"),
	}

	// Validate URL
	if _, err := url.Parse(config.URL); err != nil {
		return nil, fmt.Errorf("invalid SUPERVISOR_URL: %w", err)
	}

	return NewSupervisorClient(config)
}

// GetClient returns the underlying supervisord client
func (sc *SupervisorClient) GetClient() *supervisord.Client {
	return sc.client
}

// Ping tests the connection to supervisord
func (sc *SupervisorClient) Ping() error {
	_, err := sc.client.GetAPIVersion()
	if err != nil {
		return fmt.Errorf("failed to ping supervisord: %w", err)
	}
	return nil
}

// IsHealthy checks if the supervisord connection is healthy
func (sc *SupervisorClient) IsHealthy() bool {
	return sc.Ping() == nil
}

// getEnvOrDefault returns the environment variable value or a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
