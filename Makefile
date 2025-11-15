# Go WhatsApp Web Multidevice Makefile
# Author: Claude Code
# Description: Build, run, and manage the WhatsApp Web API application

.PHONY: help build run run-rest run-mcp test clean install update-deps fmt vet lint docker-build docker-up docker-down docker-logs docker-login-ghcr docker-build-image docker-push-ghcr docker-release docker-tag tidy check dev-rest dev-mcp

# Variables
BINARY_NAME=whatsapp
SRC_DIR=src
BUILD_DIR=build
DOCKER_COMPOSE=docker-compose.yml
GO=go
GOFLAGS=-v

# Docker image variables
DOCKER_FILE=docker/golang.Dockerfile
# Get GitHub username from remote URL, fallback to git user.name without spaces
GITHUB_USER?=$(shell git remote get-url origin 2>/dev/null | sed -n 's/.*github.com[:/]\([^/]*\)\/.*/\1/p' | tr '[:upper:]' '[:lower:]' || echo "$(shell git config user.name | tr '[:upper:]' '[:lower:]' | tr -d ' ')")
IMAGE_NAME=go-whatsapp-web-multidevice
VERSION?=latest
GHCR_REGISTRY=ghcr.io
GHCR_IMAGE=$(GHCR_REGISTRY)/$(GITHUB_USER)/$(IMAGE_NAME):$(VERSION)

# Default target
.DEFAULT_GOAL := help

## help: Display this help message
help:
	@echo "WhatsApp Web Multidevice - Makefile Commands"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Build Commands:"
	@echo "  build          Build the application binary"
	@echo "  build-linux    Build for Linux (amd64)"
	@echo "  build-windows  Build for Windows (amd64)"
	@echo "  build-mac      Build for macOS (amd64 and arm64)"
	@echo "  build-all      Build for all platforms"
	@echo ""
	@echo "Run Commands:"
	@echo "  run            Run the application in REST mode (default)"
	@echo "  run-rest       Run the application in REST API mode"
	@echo "  run-mcp        Run the application in MCP server mode"
	@echo "  dev-rest       Run with hot reload in REST mode (requires air)"
	@echo "  dev-mcp        Run with hot reload in MCP mode (requires air)"
	@echo ""
	@echo "Development Commands:"
	@echo "  test           Run all tests"
	@echo "  test-verbose   Run tests with verbose output"
	@echo "  test-cover     Run tests with coverage report"
	@echo "  fmt            Format Go code"
	@echo "  vet            Run go vet"
	@echo "  lint           Run golangci-lint (requires golangci-lint)"
	@echo "  check          Run fmt, vet, and test"
	@echo ""
	@echo "Dependency Commands:"
	@echo "  install        Install dependencies"
	@echo "  update-deps    Update all Go dependencies"
	@echo "  tidy           Clean up go.mod and go.sum"
	@echo "  deps-upgrade   Upgrade all dependencies to latest versions"
	@echo ""
	@echo "Docker Commands:"
	@echo "  docker-build        Build Docker image (docker-compose)"
	@echo "  docker-up           Start Docker containers"
	@echo "  docker-down         Stop Docker containers"
	@echo "  docker-restart      Restart Docker containers"
	@echo "  docker-logs         View Docker container logs"
	@echo ""
	@echo "Docker Registry Commands:"
	@echo "  docker-login-ghcr   Login to GitHub Container Registry"
	@echo "  docker-build-image  Build Docker image for GHCR"
	@echo "  docker-push-ghcr    Push Docker image to GHCR"
	@echo "  docker-release      Build and push to GHCR (build + push)"
	@echo "  docker-tag          Tag image with custom version"
	@echo ""
	@echo "Utility Commands:"
	@echo "  clean          Remove build artifacts and cache"
	@echo "  clean-all      Remove build artifacts, cache, and dependencies"
	@echo "  version        Display Go version"
	@echo "  info           Display project information"

## build: Build the application binary
build:
	@mkdir -p $(BUILD_DIR)
	@echo "Building $(BINARY_NAME)..."
	cd $(SRC_DIR) && $(GO) build $(GOFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

## build-linux: Build for Linux (amd64)
build-linux:
	@mkdir -p $(BUILD_DIR)
	@echo "Building for Linux (amd64)..."
	cd $(SRC_DIR) && GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64"

## build-windows: Build for Windows (amd64)
build-windows:
	@mkdir -p $(BUILD_DIR)
	@echo "Building for Windows (amd64)..."
	cd $(SRC_DIR) && GOOS=windows GOARCH=amd64 $(GO) build $(GOFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe"

## build-mac: Build for macOS (amd64 and arm64)
build-mac:
	@mkdir -p $(BUILD_DIR)
	@echo "Building for macOS (amd64)..."
	cd $(SRC_DIR) && GOOS=darwin GOARCH=amd64 $(GO) build $(GOFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 .
	@echo "Building for macOS (arm64)..."
	cd $(SRC_DIR) && GOOS=darwin GOARCH=arm64 $(GO) build $(GOFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 .
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)-darwin-*"

## build-all: Build for all platforms
build-all: build-linux build-windows build-mac
	@echo "All builds complete!"

## run: Run the application in REST mode
run: run-rest

## run-rest: Run the application in REST API mode
run-rest: build
	@echo "Starting WhatsApp Web API in REST mode..."
	cd $(SRC_DIR) && ../$(BUILD_DIR)/$(BINARY_NAME) rest

## run-mcp: Run the application in MCP server mode
run-mcp: build
	@echo "Starting WhatsApp Web API in MCP mode..."
	cd $(SRC_DIR) && ../$(BUILD_DIR)/$(BINARY_NAME) mcp

## dev-rest: Run with hot reload in REST mode (requires air)
dev-rest:
	@echo "Starting development server in REST mode with hot reload..."
	@if command -v air > /dev/null; then \
		cd $(SRC_DIR) && air -- rest; \
	else \
		echo "Error: 'air' is not installed. Install it with: go install github.com/air-verse/air@latest"; \
		exit 1; \
	fi

## dev-mcp: Run with hot reload in MCP mode (requires air)
dev-mcp:
	@echo "Starting development server in MCP mode with hot reload..."
	@if command -v air > /dev/null; then \
		cd $(SRC_DIR) && air -- mcp; \
	else \
		echo "Error: 'air' is not installed. Install it with: go install github.com/air-verse/air@latest"; \
		exit 1; \
	fi

## test: Run all tests
test:
	@echo "Running tests..."
	cd $(SRC_DIR) && $(GO) test ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "Running tests with verbose output..."
	cd $(SRC_DIR) && $(GO) test -v ./...

## test-cover: Run tests with coverage report
test-cover:
	@echo "Running tests with coverage..."
	cd $(SRC_DIR) && $(GO) test -cover ./...
	cd $(SRC_DIR) && $(GO) test -coverprofile=../$(BUILD_DIR)/coverage.out ./...
	cd $(SRC_DIR) && $(GO) tool cover -html=../$(BUILD_DIR)/coverage.out -o ../$(BUILD_DIR)/coverage.html
	@echo "Coverage report generated: $(BUILD_DIR)/coverage.html"

## fmt: Format Go code
fmt:
	@echo "Formatting Go code..."
	cd $(SRC_DIR) && $(GO) fmt ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	cd $(SRC_DIR) && $(GO) vet ./...

## lint: Run golangci-lint (requires golangci-lint)
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint > /dev/null; then \
		cd $(SRC_DIR) && golangci-lint run; \
	else \
		echo "Warning: 'golangci-lint' is not installed. Install it from: https://golangci-lint.run/usage/install/"; \
		echo "Skipping linting..."; \
	fi

## check: Run fmt, vet, and test
check: fmt vet test
	@echo "All checks passed!"

## install: Install dependencies
install:
	@echo "Installing dependencies..."
	cd $(SRC_DIR) && $(GO) mod download

## update-deps: Update all Go dependencies
update-deps:
	@echo "Updating Go dependencies..."
	cd $(SRC_DIR) && $(GO) get -u ./...
	cd $(SRC_DIR) && $(GO) mod tidy
	@echo "Dependencies updated!"

## tidy: Clean up go.mod and go.sum
tidy:
	@echo "Tidying go.mod and go.sum..."
	cd $(SRC_DIR) && $(GO) mod tidy
	@echo "go.mod and go.sum tidied!"

## deps-upgrade: Upgrade all dependencies to latest versions
deps-upgrade:
	@echo "Upgrading all dependencies to latest versions..."
	cd $(SRC_DIR) && $(GO) get -u ./...
	cd $(SRC_DIR) && $(GO) mod tidy
	@echo "All dependencies upgraded!"
	@echo ""
	@echo "Updated dependencies:"
	cd $(SRC_DIR) && $(GO) list -u -m all

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker-compose build
	@echo "Docker image built!"

## docker-up: Start Docker containers
docker-up:
	@echo "Starting Docker containers..."
	docker-compose up -d
	@echo "Docker containers started!"

## docker-down: Stop Docker containers
docker-down:
	@echo "Stopping Docker containers..."
	docker-compose down
	@echo "Docker containers stopped!"

## docker-restart: Restart Docker containers
docker-restart: docker-down docker-up
	@echo "Docker containers restarted!"

## docker-logs: View Docker container logs
docker-logs:
	@echo "Viewing Docker logs (Ctrl+C to exit)..."
	docker-compose logs -f

## docker-login-ghcr: Login to GitHub Container Registry
docker-login-ghcr:
	@echo "Logging in to GitHub Container Registry..."
	@if [ -z "$(GITHUB_TOKEN)" ]; then \
		echo "Error: GITHUB_TOKEN environment variable not set"; \
		echo ""; \
		echo "Please set it with:"; \
		echo "  export GITHUB_TOKEN=\"your-github-token\""; \
		echo ""; \
		echo "Create a token at: https://github.com/settings/tokens/new?scopes=write:packages"; \
		exit 1; \
	fi
	@echo "$(GITHUB_TOKEN)" | docker login ghcr.io -u $(GITHUB_USER) --password-stdin
	@echo "✅ Login successful!"

## docker-build-image: Build Docker image for GitHub Container Registry
docker-build-image:
	@echo "Building Docker image: $(GHCR_IMAGE)"
	@echo "GitHub User: $(GITHUB_USER)"
	@echo "Version: $(VERSION)"
	docker build -f $(DOCKER_FILE) -t $(GHCR_IMAGE) .
	@echo "Image built successfully: $(GHCR_IMAGE)"

## docker-push-ghcr: Push Docker image to GitHub Container Registry
docker-push-ghcr:
	@echo "Pushing image to GHCR: $(GHCR_IMAGE)"
	docker push $(GHCR_IMAGE)
	@echo "Image pushed successfully!"
	@echo ""
	@echo "Image is now available at:"
	@echo "  docker pull $(GHCR_IMAGE)"

## docker-release: Build and push Docker image to GHCR
docker-release: docker-build-image docker-push-ghcr
	@echo ""
	@echo "✅ Docker image released successfully!"
	@echo "  Image: $(GHCR_IMAGE)"

## docker-tag: Tag image with custom version
docker-tag:
	@if [ -z "$(TAG)" ]; then \
		echo "Error: TAG variable is required"; \
		echo "Usage: make docker-tag TAG=v1.0.0"; \
		exit 1; \
	fi
	@echo "Tagging image: $(GHCR_REGISTRY)/$(GITHUB_USER)/$(IMAGE_NAME):$(TAG)"
	docker tag $(GHCR_IMAGE) $(GHCR_REGISTRY)/$(GITHUB_USER)/$(IMAGE_NAME):$(TAG)
	docker push $(GHCR_REGISTRY)/$(GITHUB_USER)/$(IMAGE_NAME):$(TAG)
	@echo "Tagged and pushed: $(GHCR_REGISTRY)/$(GITHUB_USER)/$(IMAGE_NAME):$(TAG)"

## clean: Remove build artifacts and cache
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	cd $(SRC_DIR) && $(GO) clean -cache
	@echo "Clean complete!"

## clean-all: Remove build artifacts, cache, and dependencies
clean-all: clean
	@echo "Removing dependencies..."
	rm -rf $(SRC_DIR)/vendor
	cd $(SRC_DIR) && $(GO) clean -modcache
	@echo "All clean!"

## version: Display Go version
version:
	@echo "Go version:"
	@$(GO) version

## info: Display project information
info:
	@echo "Project Information:"
	@echo "  Name: WhatsApp Web Multidevice API"
	@echo "  Binary: $(BINARY_NAME)"
	@echo "  Source Directory: $(SRC_DIR)"
	@echo "  Build Directory: $(BUILD_DIR)"
	@echo ""
	@echo "Go Environment:"
	@$(GO) version
	@echo "  GOPATH: $(shell $(GO) env GOPATH)"
	@echo "  GOOS: $(shell $(GO) env GOOS)"
	@echo "  GOARCH: $(shell $(GO) env GOARCH)"
	@echo ""
	@echo "Project Dependencies:"
	@cd $(SRC_DIR) && $(GO) list -m all | head -20
