# WhatsApp API MultiDevice Makefile
.PHONY: help build run-rest run-mcp test clean docker-up docker-down docker-build deps fmt vet lint coverage

# Variables
BINARY_NAME=whatsapp
SRC_DIR=src
DOCKER_COMPOSE=docker-compose.yml

# Default target
help:
	@echo "WhatsApp API MultiDevice - Available commands:"
	@echo ""
	@echo "Development:"
	@echo "  make deps          - Download and tidy Go dependencies"
	@echo "  make build         - Build the binary for current platform"
	@echo "  make build-linux   - Build the binary for Linux"
	@echo "  make build-windows - Build the binary for Windows"
	@echo "  make build-mac     - Build the binary for macOS"
	@echo "  make run-rest      - Run the REST API server"
	@echo "  make run-mcp       - Run the MCP server"
	@echo "  make clean         - Remove built binaries and temporary files"
	@echo ""
	@echo "Testing:"
	@echo "  make test          - Run all tests"
	@echo "  make test-verbose  - Run tests with verbose output"
	@echo "  make coverage      - Run tests with coverage report"
	@echo "  make test-package  - Run tests for specific package (PKG=./validations)"
	@echo ""
	@echo "Code Quality:"
	@echo "  make fmt           - Format all Go code"
	@echo "  make vet           - Run go vet for static analysis"
	@echo "  make lint          - Run linter (requires golangci-lint)"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-up     - Start services with docker-compose"
	@echo "  make docker-down   - Stop docker-compose services"
	@echo "  make docker-build  - Build and start services with docker-compose"
	@echo "  make docker-logs   - Show docker-compose logs"
	@echo ""

# Development commands
deps:
	@echo "Downloading dependencies..."
	@cd $(SRC_DIR) && go mod download
	@cd $(SRC_DIR) && go mod tidy

build:
	@echo "Building binary..."
	@cd $(SRC_DIR) && go build -o $(BINARY_NAME) .
	@echo "Binary built: $(SRC_DIR)/$(BINARY_NAME)"

build-linux:
	@echo "Building for Linux..."
	@cd $(SRC_DIR) && GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux .
	@echo "Binary built: $(SRC_DIR)/$(BINARY_NAME)-linux"

build-windows:
	@echo "Building for Windows..."
	@cd $(SRC_DIR) && GOOS=windows GOARCH=amd64 go build -o $(BINARY_NAME).exe .
	@echo "Binary built: $(SRC_DIR)/$(BINARY_NAME).exe"

build-mac:
	@echo "Building for macOS..."
	@cd $(SRC_DIR) && GOOS=darwin GOARCH=amd64 go build -o $(BINARY_NAME)-mac .
	@echo "Binary built: $(SRC_DIR)/$(BINARY_NAME)-mac"

run-rest:
	@echo "Starting REST API server..."
	@cd $(SRC_DIR) && go run . rest

run-mcp:
	@echo "Starting MCP server..."
	@cd $(SRC_DIR) && go run . mcp

clean:
	@echo "Cleaning up..."
	@rm -f $(SRC_DIR)/$(BINARY_NAME)
	@rm -f $(SRC_DIR)/$(BINARY_NAME)-linux
	@rm -f $(SRC_DIR)/$(BINARY_NAME)-mac
	@rm -f $(SRC_DIR)/$(BINARY_NAME).exe
	@rm -rf $(SRC_DIR)/tmp
	@echo "Cleanup complete"

# Testing commands
test:
	@echo "Running tests..."
	@cd $(SRC_DIR) && go test ./...

test-verbose:
	@echo "Running tests with verbose output..."
	@cd $(SRC_DIR) && go test -v ./...

coverage:
	@echo "Running tests with coverage..."
	@cd $(SRC_DIR) && go test -cover ./...

coverage-html:
	@echo "Generating HTML coverage report..."
	@cd $(SRC_DIR) && go test -coverprofile=coverage.out ./...
	@cd $(SRC_DIR) && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: $(SRC_DIR)/coverage.html"

test-package:
	@if [ -z "$(PKG)" ]; then \
		echo "Usage: make test-package PKG=./validations"; \
	else \
		echo "Running tests for package $(PKG)..."; \
		cd $(SRC_DIR) && go test $(PKG); \
	fi

# Code quality commands
fmt:
	@echo "Formatting code..."
	@cd $(SRC_DIR) && go fmt ./...

vet:
	@echo "Running go vet..."
	@cd $(SRC_DIR) && go vet ./...

lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		cd $(SRC_DIR) && golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install it with:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Docker commands
docker-up:
	@echo "Starting docker services..."
	@docker-compose -f $(DOCKER_COMPOSE) up -d

docker-down:
	@echo "Stopping docker services..."
	@docker-compose -f $(DOCKER_COMPOSE) down

docker-build:
	@echo "Building and starting docker services..."
	@docker-compose -f $(DOCKER_COMPOSE) up -d --build

docker-logs:
	@echo "Showing docker logs..."
	@docker-compose -f $(DOCKER_COMPOSE) logs -f

docker-restart:
	@echo "Restarting docker services..."
	@docker-compose -f $(DOCKER_COMPOSE) restart

# Database commands
db-reset:
	@echo "Resetting database (removing whatsapp.db)..."
	@rm -f $(SRC_DIR)/whatsapp.db
	@echo "Database reset complete"

# Development workflow shortcuts
dev: deps fmt vet test build
	@echo "Development build complete"

dev-rest: deps build run-rest

dev-mcp: deps build run-mcp

# Quick commands
quick-test:
	@cd $(SRC_DIR) && go test -short ./...

quick-build:
	@cd $(SRC_DIR) && go build -o $(BINARY_NAME) .

# Installation target
install: build
	@echo "Installing binary to /usr/local/bin..."
	@sudo cp $(SRC_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Installation complete"