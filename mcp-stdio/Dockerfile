# =============================================================================
# GoWA MCP Server - Dockerfile
# =============================================================================
# Multi-stage build for minimal production image

FROM node:24-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./
COPY tsconfig.json ./

# Install dependencies
RUN npm ci

# Copy source and build
COPY src/ ./src/
RUN npm run build

# =============================================================================
# Production image
# =============================================================================
FROM node:24-alpine AS production

WORKDIR /app

# Install only production dependencies
COPY package*.json ./
RUN npm ci --only=production && npm cache clean --force

# Copy built files
COPY --from=builder /app/dist ./dist

# Default environment
ENV GOWA_URL=http://host.docker.internal:3000
ENV GOWA_DEVICE_ID=""
ENV MCP_HTTP_PORT=8090
ENV MCP_HTTP_HOST=0.0.0.0

# Expose HTTP port (only used with --http flag)
EXPOSE 8090

# Health check for HTTP mode
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8090/health || exit 0

# Default: stdio mode
ENTRYPOINT ["node", "dist/index.js"]

# Override with --http for HTTP mode
# docker run -p 8090:8090 gowa-mcp --http
