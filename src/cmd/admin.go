package cmd

import (
	"os"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/internal/admin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Start the admin server for managing GOWA instances",
	Long: `Start the admin HTTP server that allows you to create, list, and delete
GOWA instances dynamically through REST API endpoints. The server uses
Supervisord to manage the lifecycle of multiple GOWA instances.

Environment variables:
  ADMIN_TOKEN              Bearer token for API authentication (required)
  SUPERVISOR_URL           Supervisord XML-RPC URL (default: http://127.0.0.1:9001/RPC2)
  SUPERVISOR_USER          Supervisord username for authentication
  SUPERVISOR_PASS          Supervisord password for authentication
  SUPERVISOR_CONF_DIR      Directory for supervisord config files (default: /etc/supervisor/conf.d)
  INSTANCES_DIR            Directory for instance data (default: /app/instances)
  SUPERVISOR_LOG_DIR       Directory for supervisor logs (default: /var/log/supervisor)
  GOWA_BIN                 Path to GOWA binary (default: /usr/local/bin/whatsapp)
  GOWA_BASIC_AUTH          Basic auth for GOWA instances (default: admin:admin)
  GOWA_DEBUG               Debug mode for GOWA instances (default: false)
  GOWA_OS                  OS string for GOWA instances (default: Chrome)
  GOWA_ACCOUNT_VALIDATION  Account validation for GOWA instances (default: false)
  GOWA_BASE_PATH           Base path for subpath deployment (default: "")
  GOWA_AUTO_REPLY          Auto-reply message for instances (default: "")
  GOWA_AUTO_MARK_READ      Auto-mark incoming messages as read (default: false)
  GOWA_WEBHOOK             Webhook URL for instances (default: "")
  GOWA_WEBHOOK_SECRET      Webhook secret for instances (default: secret)
  GOWA_CHAT_STORAGE        Enable chat storage for instances (default: true)
  LOCK_DIR                 Directory for lock files (default: /tmp)

Example usage:
  # Start admin server with default settings
  whatsapp admin

  # Start admin server on custom port
  whatsapp admin --port 9000

API endpoints:
  POST   /admin/instances      Create a new instance
  GET    /admin/instances      List all instances
  GET    /admin/instances/:port Get specific instance
  DELETE /admin/instances/:port Delete an instance
  GET    /healthz             Health check
  GET    /readyz              Readiness check

All admin endpoints require Bearer token authentication.`,
	RunE: runAdminServer,
}

var (
	adminPort string
)

func init() {
	rootCmd.AddCommand(adminCmd)
	adminCmd.Flags().StringVarP(&adminPort, "port", "p", "8088", "Port for the admin server")
}

func runAdminServer(cmd *cobra.Command, args []string) error {
	// Setup logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})

	// Set log level based on environment
	if os.Getenv("ADMIN_DEBUG") == "true" {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	logger.Info("Starting GOWA Admin Server")

	// Check required environment variables
	if os.Getenv("ADMIN_TOKEN") == "" {
		logger.Fatal("ADMIN_TOKEN environment variable is required")
	}

	// Create supervisor client
	supervisorClient, err := admin.NewSupervisorClientFromEnv()
	if err != nil {
		logger.WithError(err).Fatal("Failed to create supervisor client")
	}

	// Test supervisor connection
	if err := supervisorClient.Ping(); err != nil {
		logger.WithError(err).Fatal("Failed to connect to supervisord")
	}

	logger.Info("Successfully connected to supervisord")

	// Create config writer
	instanceConfig := admin.DefaultInstanceConfig()
	configWriter, err := admin.NewConfigWriter(instanceConfig)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create config writer")
	}

	// Create lifecycle manager
	lifecycleManager := admin.NewLifecycleManager(supervisorClient, configWriter, logger)

	// Create API handler
	api, err := admin.NewAdminAPI(lifecycleManager, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create admin API")
	}

	// Start server
	logger.WithField("port", adminPort).Info("Admin server starting")
	if err := api.StartServer(adminPort); err != nil {
		logger.WithError(err).Fatal("Failed to start admin server")
	}

	return nil
}
