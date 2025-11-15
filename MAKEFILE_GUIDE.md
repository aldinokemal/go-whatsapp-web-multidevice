# Makefile Guide

This guide explains how to use the Makefile to build, run, and manage the WhatsApp Web Multidevice application.

## Quick Start

View all available commands:
```bash
make help
```

Build and run the application:
```bash
make build
make run-rest
```

Or simply:
```bash
make run  # Builds and runs in REST mode
```

## Common Commands

### Building the Application

**Build for current platform:**
```bash
make build
```
Output: `build/whatsapp`

**Build for specific platforms:**
```bash
make build-linux      # Linux (amd64)
make build-windows    # Windows (amd64)
make build-mac        # macOS (amd64 + arm64)
make build-all        # All platforms
```

### Running the Application

**REST API mode (default):**
```bash
make run              # Builds then runs
make run-rest         # Explicitly run REST mode
```

**MCP Server mode:**
```bash
make run-mcp
```

**Development with hot reload (requires air):**
```bash
make dev-rest         # REST mode with auto-reload
make dev-mcp          # MCP mode with auto-reload
```

Install air for hot reload:
```bash
go install github.com/air-verse/air@latest
```

### Managing Dependencies

**Update all dependencies:**
```bash
make update-deps
```
This updates all Go modules to their latest compatible versions.

**Upgrade to latest versions:**
```bash
make deps-upgrade
```
This upgrades dependencies to their absolute latest versions (may include breaking changes).

**Clean up go.mod:**
```bash
make tidy
```

**Install/download dependencies:**
```bash
make install
```

### Testing

**Run all tests:**
```bash
make test
```

**Run with verbose output:**
```bash
make test-verbose
```

**Generate coverage report:**
```bash
make test-cover
```
Opens `build/coverage.html` in your browser.

### Code Quality

**Format code:**
```bash
make fmt
```

**Run go vet:**
```bash
make vet
```

**Run linter (requires golangci-lint):**
```bash
make lint
```

**Run all checks (fmt + vet + test):**
```bash
make check
```

### Docker Operations

**Build Docker image:**
```bash
make docker-build
```

**Start containers:**
```bash
make docker-up
```

**Stop containers:**
```bash
make docker-down
```

**Restart containers:**
```bash
make docker-restart
```

**View logs:**
```bash
make docker-logs
```

### Cleanup

**Remove build artifacts:**
```bash
make clean
```

**Remove everything (including cache):**
```bash
make clean-all
```

### Information

**Show Go version:**
```bash
make version
```

**Show project info:**
```bash
make info
```

## Typical Workflows

### Development Workflow

```bash
# 1. Update dependencies
make update-deps

# 2. Format and check code
make check

# 3. Run with hot reload
make dev-rest
```

### Build and Deploy Workflow

```bash
# 1. Run tests
make test

# 2. Build for all platforms
make build-all

# 3. Docker deployment
make docker-build
make docker-up
```

### Dependency Update Workflow

```bash
# 1. Update dependencies
make update-deps

# 2. Test after update
make test

# 3. Check code quality
make check

# 4. Build to verify
make build
```

### Quick Fix Workflow

```bash
# 1. Format code
make fmt

# 2. Check for issues
make vet

# 3. Run tests
make test

# 4. Build
make build
```

## Environment-Specific Commands

### Linux

```bash
make build-linux
./build/whatsapp-linux-amd64 rest
```

### Windows

```bash
make build-windows
build\whatsapp-windows-amd64.exe rest
```

### macOS (Intel)

```bash
make build-mac
./build/whatsapp-darwin-amd64 rest
```

### macOS (Apple Silicon)

```bash
make build-mac
./build/whatsapp-darwin-arm64 rest
```

## Advanced Usage

### Custom Go Flags

Edit the Makefile to add custom flags:

```makefile
# In Makefile
GOFLAGS=-v -ldflags="-s -w"  # Strip symbols for smaller binary
```

### Parallel Builds

Build multiple platforms in parallel:

```bash
make build-linux & make build-windows & make build-mac & wait
```

### Testing Specific Packages

```bash
cd src && go test ./pkg/storage/...
```

### Custom Run Arguments

```bash
# Build first
make build

# Run with custom flags
./build/whatsapp rest --port=8080 --debug=true
```

## Troubleshooting

### "Command not found: make"

**Linux (Debian/Ubuntu):**
```bash
sudo apt-get install build-essential
```

**macOS:**
```bash
xcode-select --install
```

**Windows:**
Use WSL, Git Bash, or install make via chocolatey:
```bash
choco install make
```

### "air not found" Error

Install air for hot reload:
```bash
go install github.com/air-verse/air@latest
```

Make sure `$GOPATH/bin` is in your PATH.

### "golangci-lint not found" Warning

Install golangci-lint:
```bash
# macOS/Linux
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Or using go install
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### Build Errors

```bash
# Clean everything and rebuild
make clean-all
make install
make build
```

### Docker Issues

```bash
# Rebuild from scratch
make docker-down
docker system prune -a
make docker-build
make docker-up
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Build and Test

on: [push, pull_request]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: 1.24
      - name: Install dependencies
        run: make install
      - name: Run checks
        run: make check
      - name: Build
        run: make build-all
```

### GitLab CI Example

```yaml
stages:
  - test
  - build

test:
  stage: test
  script:
    - make install
    - make check

build:
  stage: build
  script:
    - make build-all
  artifacts:
    paths:
      - build/
```

## Tips and Best Practices

1. **Always run `make check` before committing**
   ```bash
   make check
   ```

2. **Use hot reload during development**
   ```bash
   make dev-rest
   ```

3. **Update dependencies regularly**
   ```bash
   make update-deps
   make test
   ```

4. **Generate coverage reports**
   ```bash
   make test-cover
   ```

5. **Clean before important builds**
   ```bash
   make clean
   make build
   ```

6. **Use Docker for consistent environments**
   ```bash
   make docker-build
   make docker-up
   ```

## Customization

You can customize the Makefile by editing these variables at the top:

```makefile
BINARY_NAME=whatsapp       # Change binary name
SRC_DIR=src               # Source directory
BUILD_DIR=build           # Build output directory
GOFLAGS=-v                # Go build flags
```

## Additional Resources

- [Go Documentation](https://golang.org/doc/)
- [GNU Make Manual](https://www.gnu.org/software/make/manual/)
- [Air (Hot Reload)](https://github.com/air-verse/air)
- [golangci-lint](https://golangci-lint.run/)
- [Project README](README.md)
- [Media Storage Guide](MEDIA_STORAGE.md)
