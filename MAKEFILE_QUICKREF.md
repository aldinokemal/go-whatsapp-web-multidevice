# Makefile Quick Reference

Quick reference card for the most commonly used Makefile commands.

## 🚀 Most Common Commands

```bash
# Show all available commands
make help

# Build and run (REST mode)
make build
make run

# Update dependencies
make update-deps

# Run tests
make test

# Format code
make fmt
```

## 📦 Build Commands

| Command | Description |
|---------|-------------|
| `make build` | Build for current platform |
| `make build-linux` | Build for Linux (amd64) |
| `make build-windows` | Build for Windows (amd64) |
| `make build-mac` | Build for macOS (amd64 + arm64) |
| `make build-all` | Build for all platforms |

## ▶️ Run Commands

| Command | Description |
|---------|-------------|
| `make run` | Run in REST mode |
| `make run-rest` | Run in REST API mode |
| `make run-mcp` | Run in MCP server mode |
| `make dev-rest` | Run with hot reload (REST) |
| `make dev-mcp` | Run with hot reload (MCP) |

## 📚 Dependency Commands

| Command | Description |
|---------|-------------|
| `make install` | Install dependencies |
| `make update-deps` | Update all dependencies |
| `make deps-upgrade` | Upgrade to latest versions |
| `make tidy` | Clean up go.mod/go.sum |

## 🧪 Testing Commands

| Command | Description |
|---------|-------------|
| `make test` | Run all tests |
| `make test-verbose` | Run with verbose output |
| `make test-cover` | Generate coverage report |

## 🛠️ Development Commands

| Command | Description |
|---------|-------------|
| `make fmt` | Format Go code |
| `make vet` | Run go vet |
| `make lint` | Run golangci-lint |
| `make check` | Run fmt + vet + test |

## 🐳 Docker Commands

| Command | Description |
|---------|-------------|
| `make docker-build` | Build Docker image |
| `make docker-up` | Start containers |
| `make docker-down` | Stop containers |
| `make docker-logs` | View logs |
| `make docker-restart` | Restart containers |

## 🧹 Cleanup Commands

| Command | Description |
|---------|-------------|
| `make clean` | Remove build artifacts |
| `make clean-all` | Remove everything |

## ℹ️ Info Commands

| Command | Description |
|---------|-------------|
| `make version` | Show Go version |
| `make info` | Show project info |

## 📋 Typical Workflows

### Daily Development
```bash
make dev-rest       # Start with hot reload
# Make changes...
make check          # Before committing
```

### Before Commit
```bash
make fmt            # Format code
make vet            # Check for issues
make test           # Run tests
```

### Update Dependencies
```bash
make update-deps    # Update dependencies
make test           # Verify tests pass
make build          # Verify build works
```

### Production Build
```bash
make clean          # Clean old builds
make test           # Verify tests
make build-all      # Build all platforms
```

### Docker Deployment
```bash
make docker-build   # Build image
make docker-up      # Start containers
make docker-logs    # Monitor logs
```

## 💡 Pro Tips

1. **Chain commands**: `make fmt && make vet && make test && make build`

2. **Run in background**: `make run &`

3. **Check specific package**:
   ```bash
   cd src && go test ./pkg/storage/...
   ```

4. **Hot reload development**:
   ```bash
   make dev-rest  # Automatic restart on file changes
   ```

5. **View coverage in browser**:
   ```bash
   make test-cover
   open build/coverage.html  # macOS
   xdg-open build/coverage.html  # Linux
   ```

6. **Custom build flags**: Edit Makefile `GOFLAGS` variable

7. **Parallel builds**:
   ```bash
   make build-linux & make build-windows & wait
   ```

## 🔗 Related Documentation

- [Full Makefile Guide](MAKEFILE_GUIDE.md) - Comprehensive documentation
- [Media Storage Guide](MEDIA_STORAGE.md) - S3/Local storage setup
- [Project README](README.md) - Main project documentation

## 🆘 Quick Troubleshooting

**Build fails?**
```bash
make clean-all
make install
make build
```

**Tests fail?**
```bash
make fmt
make vet
make test-verbose
```

**Dependencies issues?**
```bash
make tidy
make install
```

**Docker not working?**
```bash
make docker-down
docker system prune -a
make docker-build
make docker-up
```
