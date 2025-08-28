package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// AdminAPI handles HTTP requests for instance management
type AdminAPI struct {
	lifecycle  ILifecycleManager
	logger     *logrus.Logger
	adminToken string
}

// CreateInstanceRequest represents the request body for creating an instance
type CreateInstanceRequest struct {
	Port              int    `json:"port" validate:"required,min=1024,max=65535"`
	BasicAuth         string `json:"basic_auth,omitempty"`
	Debug             *bool  `json:"debug,omitempty"`
	OS                string `json:"os,omitempty"`
	AccountValidation *bool  `json:"account_validation,omitempty"`
	BasePath          string `json:"base_path,omitempty"`
	AutoReply         string `json:"auto_reply,omitempty"`
	AutoMarkRead      *bool  `json:"auto_mark_read,omitempty"`
	Webhook           string `json:"webhook,omitempty"`
	WebhookSecret     string `json:"webhook_secret,omitempty"`
	ChatStorage       *bool  `json:"chat_storage,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Timestamp string `json:"timestamp"`
}

// SuccessResponse represents a success response
type SuccessResponse struct {
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message"`
	RequestID string      `json:"request_id"`
	Timestamp string      `json:"timestamp"`
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status     string    `json:"status"`
	Timestamp  time.Time `json:"timestamp"`
	Supervisor bool      `json:"supervisor_healthy"`
	Version    string    `json:"version"`
}

// NewAdminAPI creates a new AdminAPI instance
func NewAdminAPI(lifecycle ILifecycleManager, logger *logrus.Logger) (*AdminAPI, error) {
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		return nil, fmt.Errorf("ADMIN_TOKEN environment variable is required")
	}

	return &AdminAPI{
		lifecycle:  lifecycle,
		logger:     logger,
		adminToken: adminToken,
	}, nil
}

// SetupRoutes configures the Fiber app with admin routes
func (api *AdminAPI) SetupRoutes(app *fiber.App) {
	// Middleware
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "${time} ${status} - ${method} ${path} ${latency}\n",
	}))
	app.Use(cors.New())
	app.Use(api.requestIDMiddleware)

	// Health endpoints
	app.Get("/healthz", api.healthHandler)
	app.Get("/readyz", api.readinessHandler)

	// Admin routes with authentication
	admin := app.Group("/admin", api.authMiddleware)
	admin.Post("/instances", api.createInstanceHandler)
	admin.Get("/instances", api.listInstancesHandler)
	admin.Get("/instances/:port", api.getInstanceHandler)
	admin.Delete("/instances/:port", api.deleteInstanceHandler)
}

// requestIDMiddleware adds a request ID to each request
func (api *AdminAPI) requestIDMiddleware(c *fiber.Ctx) error {
	requestID := c.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
	}
	c.Locals("request_id", requestID)
	c.Set("X-Request-ID", requestID)
	return c.Next()
}

// authMiddleware validates bearer token authentication
func (api *AdminAPI) authMiddleware(c *fiber.Ctx) error {
	auth := c.Get("Authorization")
	if auth == "" {
		return api.errorResponse(c, http.StatusUnauthorized, "missing_authorization", "Authorization header is required")
	}

	if !strings.HasPrefix(auth, "Bearer ") {
		return api.errorResponse(c, http.StatusUnauthorized, "invalid_authorization", "Authorization must use Bearer token")
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	if token != api.adminToken {
		return api.errorResponse(c, http.StatusUnauthorized, "invalid_token", "Invalid or expired token")
	}

	return c.Next()
}

// createInstanceHandler handles POST /admin/instances
func (api *AdminAPI) createInstanceHandler(c *fiber.Ctx) error {
	var req CreateInstanceRequest
	if err := c.BodyParser(&req); err != nil {
		return api.errorResponse(c, http.StatusBadRequest, "invalid_json", "Invalid JSON in request body")
	}

	// Validate port range
	if req.Port < 1024 || req.Port > 65535 {
		return api.errorResponse(c, http.StatusBadRequest, "invalid_port", "Port must be between 1024 and 65535")
	}

	api.logger.Infof("Creating instance on port %d with custom config", req.Port)

	// Create custom configuration if any fields are provided
	var customConfig *InstanceConfig
	if api.hasCustomConfig(req) {
		customConfig = api.buildInstanceConfig(req)
	}

	var instance *Instance
	var err error

	if customConfig != nil {
		instance, err = api.lifecycle.CreateInstanceWithConfig(req.Port, customConfig)
	} else {
		instance, err = api.lifecycle.CreateInstance(req.Port)
	}
	if err != nil {
		api.logger.Errorf("Failed to create instance on port %d: %v", req.Port, err)

		// Determine appropriate HTTP status code based on error
		status := http.StatusInternalServerError
		errorCode := "creation_failed"

		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
			errorCode = "instance_exists"
		} else if strings.Contains(err.Error(), "port") && strings.Contains(err.Error(), "in use") {
			status = http.StatusConflict
			errorCode = "port_in_use"
		} else if strings.Contains(err.Error(), "locked") {
			status = http.StatusConflict
			errorCode = "port_locked"
		}

		return api.errorResponse(c, status, errorCode, err.Error())
	}

	return api.successResponse(c, http.StatusCreated, instance, "Instance created successfully")
}

// listInstancesHandler handles GET /admin/instances
func (api *AdminAPI) listInstancesHandler(c *fiber.Ctx) error {
	instances, err := api.lifecycle.ListInstances()
	if err != nil {
		api.logger.Errorf("Failed to list instances: %v", err)
		return api.errorResponse(c, http.StatusInternalServerError, "list_failed", "Failed to retrieve instances")
	}

	return api.successResponse(c, http.StatusOK, instances, "Instances retrieved successfully")
}

// getInstanceHandler handles GET /admin/instances/:port
func (api *AdminAPI) getInstanceHandler(c *fiber.Ctx) error {
	portStr := c.Params("port")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return api.errorResponse(c, http.StatusBadRequest, "invalid_port", "Port must be a valid integer")
	}

	instance, err := api.lifecycle.GetInstance(port)
	if err != nil {
		api.logger.Errorf("Failed to get instance on port %d: %v", port, err)

		if strings.Contains(err.Error(), "not found") {
			return api.errorResponse(c, http.StatusNotFound, "instance_not_found", fmt.Sprintf("Instance on port %d not found", port))
		}

		return api.errorResponse(c, http.StatusInternalServerError, "get_failed", "Failed to retrieve instance")
	}

	return api.successResponse(c, http.StatusOK, instance, "Instance retrieved successfully")
}

// deleteInstanceHandler handles DELETE /admin/instances/:port
func (api *AdminAPI) deleteInstanceHandler(c *fiber.Ctx) error {
	portStr := c.Params("port")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return api.errorResponse(c, http.StatusBadRequest, "invalid_port", "Port must be a valid integer")
	}

	api.logger.Infof("Deleting instance on port %d", port)

	err = api.lifecycle.DeleteInstance(port)
	if err != nil {
		api.logger.Errorf("Failed to delete instance on port %d: %v", port, err)

		if strings.Contains(err.Error(), "not found") {
			return api.errorResponse(c, http.StatusNotFound, "instance_not_found", fmt.Sprintf("Instance on port %d not found", port))
		}

		return api.errorResponse(c, http.StatusInternalServerError, "deletion_failed", "Failed to delete instance")
	}

	return api.successResponse(c, http.StatusOK, nil, "Instance deleted successfully")
}

// healthHandler handles GET /healthz
func (api *AdminAPI) healthHandler(c *fiber.Ctx) error {
	response := HealthResponse{
		Status:     "healthy",
		Timestamp:  time.Now(),
		Supervisor: api.lifecycle.IsHealthy(),
		Version:    "1.0.0", // TODO: Get from build info
	}

	status := http.StatusOK
	if !response.Supervisor {
		response.Status = "degraded"
		status = http.StatusServiceUnavailable
	}

	return c.Status(status).JSON(response)
}

// readinessHandler handles GET /readyz
func (api *AdminAPI) readinessHandler(c *fiber.Ctx) error {
	// Check if supervisord is reachable
	if err := api.lifecycle.Ping(); err != nil {
		return api.errorResponse(c, http.StatusServiceUnavailable, "supervisor_unreachable", "Supervisord is not reachable")
	}

	return api.successResponse(c, http.StatusOK, nil, "Service is ready")
}

// errorResponse sends a standardized error response
func (api *AdminAPI) errorResponse(c *fiber.Ctx, status int, errorCode, message string) error {
	requestID := api.getRequestID(c)

	response := ErrorResponse{
		Error:     errorCode,
		Message:   message,
		RequestID: requestID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Log error for debugging
	api.logger.WithFields(logrus.Fields{
		"request_id": requestID,
		"status":     status,
		"error_code": errorCode,
		"message":    message,
	}).Error("API error response")

	return c.Status(status).JSON(response)
}

// successResponse sends a standardized success response
func (api *AdminAPI) successResponse(c *fiber.Ctx, status int, data interface{}, message string) error {
	requestID := api.getRequestID(c)

	response := SuccessResponse{
		Data:      data,
		Message:   message,
		RequestID: requestID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	return c.Status(status).JSON(response)
}

// getRequestID retrieves the request ID from context
func (api *AdminAPI) getRequestID(c *fiber.Ctx) string {
	if requestID, ok := c.Locals("request_id").(string); ok {
		return requestID
	}
	return "unknown"
}

// StartServer starts the admin HTTP server
func (api *AdminAPI) StartServer(port string) error {
	app := fiber.New(fiber.Config{
		ErrorHandler: api.errorHandler,
		JSONEncoder:  json.Marshal,
		JSONDecoder:  json.Unmarshal,
	})

	api.SetupRoutes(app)

	api.logger.Infof("Starting admin server on port %s", port)
	return app.Listen(":" + port)
}

// errorHandler handles uncaught errors
func (api *AdminAPI) errorHandler(c *fiber.Ctx, err error) error {
	// Default to 500 Internal Server Error
	code := http.StatusInternalServerError
	message := "Internal Server Error"

	// Handle Fiber errors
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
		message = e.Message
	}

	api.logger.WithFields(logrus.Fields{
		"path":   c.Path(),
		"method": c.Method(),
		"error":  err.Error(),
	}).Error("Unhandled error")

	return api.errorResponse(c, code, "internal_error", message)
}

// hasCustomConfig checks if the request contains any custom configuration
func (api *AdminAPI) hasCustomConfig(req CreateInstanceRequest) bool {
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

// buildInstanceConfig creates an InstanceConfig from the API request
func (api *AdminAPI) buildInstanceConfig(req CreateInstanceRequest) *InstanceConfig {
	// Start with default configuration
	config := DefaultInstanceConfig()

	// Override with values from request
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
