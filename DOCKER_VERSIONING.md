# Docker Versioning Guide

This document explains how to use the improved Docker release system with automatic versioning.

## Quick Start

### 1. Release with Auto-Versioning (Recommended)

The Makefile automatically detects the version from `src/config/settings.go` (currently `v7.9.0`):

```bash
make docker-release
```

This will:
- Build and tag: `ghcr.io/hbinduni/go-whatsapp-web-multidevice:v7.9.0`
- Build and tag: `ghcr.io/hbinduni/go-whatsapp-web-multidevice:latest`
- Push both tags to GitHub Container Registry

### 2. Release with Custom Version

Override the version for a specific release:

```bash
VERSION=v8.0.0 make docker-release
```

This will create:
- `ghcr.io/hbinduni/go-whatsapp-web-multidevice:v8.0.0`
- `ghcr.io/hbinduni/go-whatsapp-web-multidevice:latest`

## Individual Commands

### Login to GHCR

The Makefile automatically uses GitHub CLI (gh) if available:

```bash
make docker-login-ghcr
```

This will:
1. Check if `gh` CLI is installed and authenticated
2. Use `gh auth token` to login to Docker
3. Fallback to `GITHUB_TOKEN` environment variable if `gh` is not available

### Build Only (No Push)

Build Docker images locally without pushing:

```bash
make docker-build-image
```

Output:
```
Building and tagging:
  - ghcr.io/hbinduni/go-whatsapp-web-multidevice:v7.9.0
  - ghcr.io/hbinduni/go-whatsapp-web-multidevice:latest
```

### Push Only

Push previously built images:

```bash
make docker-push-ghcr
```

This pushes both version and latest tags.

## Version Management

### How Versioning Works

1. **Auto-Detection**: Version is extracted from `src/config/settings.go`:
   ```go
   AppVersion = "v7.9.0"
   ```

2. **Override**: Use `VERSION` environment variable:
   ```bash
   VERSION=v1.2.3 make docker-release
   ```

3. **Latest Tag**: Always pushed alongside the version tag

### Updating the Version

To release a new version:

1. Update `src/config/settings.go`:
   ```go
   AppVersion = "v8.0.0"  // Change this
   ```

2. Build and push:
   ```bash
   make docker-release
   ```

## Usage Examples

### Scenario 1: Regular Release

You've finished developing a new feature and want to release it:

```bash
# 1. Update version in src/config/settings.go to v7.10.0
# 2. Commit your changes
git add src/config/settings.go
git commit -m "chore: bump version to v7.10.0"

# 3. Release to GHCR
make docker-release

# Result:
# - ghcr.io/hbinduni/go-whatsapp-web-multidevice:v7.10.0
# - ghcr.io/hbinduni/go-whatsapp-web-multidevice:latest
```

### Scenario 2: Hotfix Release

Quick patch without changing the config file:

```bash
VERSION=v7.9.1-hotfix make docker-release
```

### Scenario 3: Test Build Locally

Build without pushing to test:

```bash
make docker-build-image

# Test the image locally
docker run -p 3000:3000 --env-file src/.env \
  ghcr.io/hbinduni/go-whatsapp-web-multidevice:v7.9.0 rest
```

## Pulling Images

### Pull Specific Version

```bash
docker pull ghcr.io/hbinduni/go-whatsapp-web-multidevice:v7.9.0
```

### Pull Latest

```bash
docker pull ghcr.io/hbinduni/go-whatsapp-web-multidevice:latest
```

### Use in docker-compose.yml

```yaml
services:
  whatsapp_go:
    # Option 1: Use specific version (recommended for production)
    image: ghcr.io/hbinduni/go-whatsapp-web-multidevice:v7.9.0

    # Option 2: Use latest (for development)
    # image: ghcr.io/hbinduni/go-whatsapp-web-multidevice:latest
```

## Troubleshooting

### Authentication Issues

If `make docker-login-ghcr` fails:

```bash
# Option 1: Re-login with gh CLI
gh auth logout
gh auth login
# When prompted, ensure you add 'write:packages' scope

# Option 2: Use token manually
export GITHUB_TOKEN="your-token-here"
make docker-login-ghcr
```

### Version Not Detected

If version shows as "latest" instead of actual version:

```bash
# Check version extraction
grep 'AppVersion.*=' src/config/settings.go

# Should output: AppVersion = "v7.9.0"
```

### Image Not Found After Push

Check package visibility at: https://github.com/hbinduni?tab=packages

Make sure the package is public or you're authenticated when pulling.

## Best Practices

1. **Semantic Versioning**: Use `vMAJOR.MINOR.PATCH` format (e.g., `v7.9.0`)

2. **Update Version Before Release**: Always update `src/config/settings.go` before running `make docker-release`

3. **Tag Git Commits**: Create git tags matching Docker versions:
   ```bash
   git tag v7.9.0
   git push origin v7.9.0
   ```

4. **Production Deployments**: Use specific version tags, not `latest`:
   ```yaml
   image: ghcr.io/hbinduni/go-whatsapp-web-multidevice:v7.9.0
   ```

5. **Development**: Use `latest` for quick iterations:
   ```yaml
   image: ghcr.io/hbinduni/go-whatsapp-web-multidevice:latest
   ```

## GitHub Package Visibility

After pushing, your images are available at:
https://github.com/hbinduni?tab=packages

To make a package public:
1. Go to the package page
2. Click "Package settings"
3. Scroll to "Danger Zone"
4. Click "Change visibility" → "Public"
