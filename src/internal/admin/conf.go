package admin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"text/template"
)

// InstanceConfig holds the configuration for a GOWA instance
type InstanceConfig struct {
	Port              int
	ConfDir           string
	InstancesDir      string
	LogDir            string
	GowaBin           string
	BasicAuth         string
	Debug             bool
	OS                string
	AccountValidation bool
	BasePath          string
	AutoReply         string
	AutoMarkRead      bool
	Webhook           string
	WebhookSecret     string
	ChatStorage       bool
}

// DefaultInstanceConfig returns a default instance configuration from environment variables
func DefaultInstanceConfig() *InstanceConfig {
	debug, _ := strconv.ParseBool(getEnvOrDefault("GOWA_DEBUG", "false"))
	accountValidation, _ := strconv.ParseBool(getEnvOrDefault("GOWA_ACCOUNT_VALIDATION", "false"))
	autoMarkRead, _ := strconv.ParseBool(getEnvOrDefault("GOWA_AUTO_MARK_READ", "false"))
	chatStorage, _ := strconv.ParseBool(getEnvOrDefault("GOWA_CHAT_STORAGE", "true"))

	return &InstanceConfig{
		ConfDir:           getEnvOrDefault("SUPERVISOR_CONF_DIR", "/etc/supervisor/conf.d"),
		InstancesDir:      getEnvOrDefault("INSTANCES_DIR", "/app/instances"),
		LogDir:            getEnvOrDefault("SUPERVISOR_LOG_DIR", "/var/log/supervisor"),
		GowaBin:           getEnvOrDefault("GOWA_BIN", "/usr/local/bin/whatsapp"),
		BasicAuth:         getEnvOrDefault("GOWA_BASIC_AUTH", "admin:admin"),
		Debug:             debug,
		OS:                getEnvOrDefault("GOWA_OS", "Chrome"),
		AccountValidation: accountValidation,
		BasePath:          getEnvOrDefault("GOWA_BASE_PATH", ""),
		AutoReply:         getEnvOrDefault("GOWA_AUTO_REPLY", ""),
		AutoMarkRead:      autoMarkRead,
		Webhook:           getEnvOrDefault("GOWA_WEBHOOK", ""),
		WebhookSecret:     getEnvOrDefault("GOWA_WEBHOOK_SECRET", "secret"),
		ChatStorage:       chatStorage,
	}
}

// ConfigTemplate is the template for supervisord program configuration
const ConfigTemplate = `[program:gowa_{{ .Port }}]
command={{ .GowaBin }} rest --port={{ .Port }} --debug={{ .Debug }} --os={{ .OS }} --account-validation={{ .AccountValidation }} --basic-auth={{ .BasicAuth }}{{ if .BasePath }} --base-path={{ .BasePath }}{{ end }}{{ if .AutoReply }} --autoreply="{{ .AutoReply }}"{{ end }}{{ if .AutoMarkRead }} --auto-mark-read={{ .AutoMarkRead }}{{ end }}{{ if .Webhook }} --webhook="{{ .Webhook }}"{{ end }}{{ if .WebhookSecret }} --webhook-secret="{{ .WebhookSecret }}"{{ end }}
directory={{ .InstancesDir }}/{{ .Port }}
autostart=true
autorestart=true
startretries=3
stdout_logfile={{ .LogDir }}/gowa_{{ .Port }}.out.log
stderr_logfile={{ .LogDir }}/gowa_{{ .Port }}.err.log
environment=APP_PORT="{{ .Port }}",APP_DEBUG="{{ .Debug }}",APP_OS="{{ .OS }}",APP_BASIC_AUTH="{{ .BasicAuth }}",DB_URI="file:storages/whatsapp.db?_foreign_keys=on"{{ if .BasePath }},APP_BASE_PATH="{{ .BasePath }}"{{ end }}{{ if .AutoReply }},WHATSAPP_AUTO_REPLY="{{ .AutoReply }}"{{ end }}{{ if .AutoMarkRead }},WHATSAPP_AUTO_MARK_READ="{{ .AutoMarkRead }}"{{ end }}{{ if .Webhook }},WHATSAPP_WEBHOOK="{{ .Webhook }}"{{ end }}{{ if .WebhookSecret }},WHATSAPP_WEBHOOK_SECRET="{{ .WebhookSecret }}"{{ end }},WHATSAPP_ACCOUNT_VALIDATION="{{ .AccountValidation }}",WHATSAPP_CHAT_STORAGE="{{ .ChatStorage }}"
`

// ConfigWriter handles writing and removing supervisord configuration files
type ConfigWriter struct {
	config *InstanceConfig
	tmpl   *template.Template
}

// NewConfigWriter creates a new configuration writer
func NewConfigWriter(config *InstanceConfig) (*ConfigWriter, error) {
	tmpl, err := template.New("config").Parse(ConfigTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config template: %w", err)
	}

	return &ConfigWriter{
		config: config,
		tmpl:   tmpl,
	}, nil
}

// WriteConfig atomically writes a supervisord configuration file for a given port
func (cw *ConfigWriter) WriteConfig(port int) error {
	config := *cw.config
	config.Port = port

	// Ensure the config directory exists
	if err := os.MkdirAll(config.ConfDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", config.ConfDir, err)
	}

	// Ensure the instance storage directory exists
	storageDir := filepath.Join(config.InstancesDir, strconv.Itoa(port), "storages")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory %s: %w", storageDir, err)
	}

	// Ensure the log directory exists
	if err := os.MkdirAll(config.LogDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory %s: %w", config.LogDir, err)
	}

	// Generate config content
	configPath := filepath.Join(config.ConfDir, fmt.Sprintf("gowa-%d.conf", port))
	tempPath := configPath + ".tmp"

	// Write to temporary file first
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temp config file %s: %w", tempPath, err)
	}
	defer tempFile.Close()

	if err := cw.tmpl.Execute(tempFile, config); err != nil {
		return fmt.Errorf("failed to execute config template: %w", err)
	}

	// Ensure data is written to disk
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp config file: %w", err)
	}

	tempFile.Close()

	// Atomically move the temporary file to the final location
	if err := os.Rename(tempPath, configPath); err != nil {
		// Clean up temp file on failure
		os.Remove(tempPath)
		return fmt.Errorf("failed to move temp config file to final location: %w", err)
	}

	return nil
}

// RemoveConfig removes the supervisord configuration file for a given port
func (cw *ConfigWriter) RemoveConfig(port int) error {
	configPath := filepath.Join(cw.config.ConfDir, fmt.Sprintf("gowa-%d.conf", port))

	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove config file %s: %w", configPath, err)
	}

	return nil
}

// ConfigExists checks if a configuration file exists for a given port
func (cw *ConfigWriter) ConfigExists(port int) bool {
	configPath := filepath.Join(cw.config.ConfDir, fmt.Sprintf("gowa-%d.conf", port))
	_, err := os.Stat(configPath)
	return err == nil
}

// LockManager handles per-port locking to prevent concurrent operations
type LockManager struct {
	lockDir string
}

// NewLockManager creates a new lock manager
func NewLockManager() *LockManager {
	return &LockManager{
		lockDir: getEnvOrDefault("LOCK_DIR", "/tmp"),
	}
}

// AcquireLock acquires a file lock for a specific port
func (lm *LockManager) AcquireLock(port int) (*os.File, error) {
	lockPath := filepath.Join(lm.lockDir, fmt.Sprintf("gowa.%d.lock", port))

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}

	// Try to acquire exclusive lock
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lockFile.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("port %d is currently locked by another operation", port)
		}
		return nil, fmt.Errorf("failed to acquire lock for port %d: %w", port, err)
	}

	return lockFile, nil
}

// ReleaseLock releases a file lock
func (lm *LockManager) ReleaseLock(lockFile *os.File) error {
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	return nil
}
