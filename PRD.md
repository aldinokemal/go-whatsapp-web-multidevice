# Product Requirements Document (PRD)
# WhatsApp Multi-Server SaaS Platform

**Version:** 2.0.0
**Last Updated:** 2025-11-19
**Status:** Planning Phase
**Author:** Development Team

---

## 📋 Table of Contents

1. [Executive Summary](#executive-summary)
2. [Business Objectives](#business-objectives)
3. [Current State Analysis](#current-state-analysis)
4. [Target Architecture](#target-architecture)
5. [System Components](#system-components)
6. [Security Requirements](#security-requirements)
7. [Folder Structure](#folder-structure)
8. [Database Schema](#database-schema)
9. [API Specifications](#api-specifications)
10. [Migration Strategy](#migration-strategy)
11. [Success Metrics](#success-metrics)
12. [Timeline & Phases](#timeline--phases)

---

## 1. Executive Summary

### 1.1 Project Overview
Transform the current monolithic WhatsApp Web API into a **scalable multi-server SaaS platform** capable of serving 1000+ customers with centralized management, enhanced security, and horizontal scalability.

### 1.2 Key Goals
- ✅ Support 1000+ customers across multiple VPS servers
- ✅ Centralized management system with "Add Server" capability
- ✅ Enhanced security (API key management, rate limiting, audit logs)
- ✅ Clean separation of concerns (backend + frontend)
- ✅ Cost-effective scaling ($0.50-0.60 per customer/month)
- ✅ Zero downtime deployments
- ✅ Comprehensive monitoring and alerting

### 1.3 Business Model
**Shared Infrastructure SaaS:**
- 1 VPS serves 50-100 customers
- Dynamic server allocation
- Pay-as-you-grow infrastructure
- Target profit margin: 90-95%

---

## 2. Business Objectives

### 2.1 Primary Objectives
| Objective | Target | Timeline |
|-----------|--------|----------|
| Support 1000+ concurrent customers | 1000+ | Q2 2025 |
| 99.9% uptime SLA | 99.9% | Q1 2025 |
| Average response time | < 200ms | Q1 2025 |
| Infrastructure cost per customer | < $0.60/month | Q1 2025 |
| Customer acquisition cost | < $10 | Q2 2025 |

### 2.2 Revenue Model
```
Pricing Tiers:
- Basic: $10/month (1000 messages/day)
- Pro: $25/month (5000 messages/day)
- Business: $50/month (20000 messages/day)
- Enterprise: $150/month (Unlimited + dedicated support)

Target Revenue (1000 customers, 70% Basic, 20% Pro, 10% Business):
= (700 × $10) + (200 × $25) + (100 × $50)
= $7,000 + $5,000 + $5,000
= $17,000/month

Infrastructure Cost: $600/month
Gross Margin: 96.5%
```

---

## 3. Current State Analysis

### 3.1 Current Architecture Issues

**❌ Problems:**
1. **Monolithic Structure** - All code in single `src/` directory
2. **No Multi-Tenancy** - Cannot serve multiple customers efficiently
3. **No Central Management** - Cannot add/remove servers dynamically
4. **Security Gaps:**
   - No API key per customer
   - No rate limiting per customer
   - Basic auth only (shared credentials)
   - No audit logging
5. **Global State** - WhatsApp client as global variable
6. **No Customer Isolation** - All customers share same instance
7. **Limited Scalability** - Cannot scale horizontally

### 3.2 Current Strengths (Keep These!)
✅ Clean domain-driven design
✅ Well-structured use cases
✅ Comprehensive WhatsApp integration
✅ Good validation framework
✅ Webhook support with retry logic
✅ WebSocket real-time updates

---

## 4. Target Architecture

### 4.1 High-Level Architecture

```
┌────────────────────────────────────────────────────────────┐
│                  CUSTOMER LAYER                            │
│  Customer A, B, C, ... → Dashboard (Next.js)               │
└────────────────┬───────────────────────────────────────────┘
                 │ HTTPS (API Key Authentication)
┌────────────────▼───────────────────────────────────────────┐
│              MANAGEMENT SYSTEM                             │
│  ┌──────────────────────────────────────────────┐          │
│  │  Admin Panel (Next.js)                       │          │
│  │  - Add/Remove Servers                        │          │
│  │  - Monitor Server Health                     │          │
│  │  - Customer Management                       │          │
│  │  - Billing & Usage Analytics                 │          │
│  └──────────────────────────────────────────────┘          │
│  ┌──────────────────────────────────────────────┐          │
│  │  Management API (Go)                         │          │
│  │  - Load Balancer / Request Router            │          │
│  │  - Authentication & Authorization            │          │
│  │  - Rate Limiting (per customer)              │          │
│  │  - Server Registry                           │          │
│  │  - Health Monitoring                         │          │
│  │  - Audit Logging                             │          │
│  └──────────────────────────────────────────────┘          │
└────────────────┬───────────────────────────────────────────┘
                 │
        ┌────────┼────────┬─────────┐
        │        │        │         │
┌───────▼────┐ ┌▼────────▼──┐ ┌────▼────────┐
│  VPS 1     │ │  VPS 2     │ │  VPS N      │
│  Worker    │ │  Worker    │ │  Worker     │
│  Backend   │ │  Backend   │ │  Backend    │
│            │ │            │ │             │
│  100 users │ │  100 users │ │  100 users  │
└────────────┘ └────────────┘ └─────────────┘
        │        │        │
        └────────┼────────┘
                 │
┌────────────────▼───────────────────────────────────────────┐
│         CENTRAL DATABASE (PostgreSQL)                      │
│  - Customers, Servers, Metrics, Billing, Audit Logs        │
└────────────────────────────────────────────────────────────┘
```

### 4.2 Component Breakdown

#### 4.2.1 **Management System**
- **Purpose:** Central brain for managing all VPS servers
- **Components:**
  - Admin Panel (Frontend)
  - Management API (Backend)
  - Load Balancer / Proxy
  - Health Monitor

#### 4.2.2 **Worker Backend**
- **Purpose:** Handle WhatsApp connections for customers
- **Components:**
  - WhatsApp Instance Manager
  - Multi-tenant support
  - Local cache (Redis/Memory)
  - Message queue consumer

#### 4.2.3 **Customer Dashboard**
- **Purpose:** Customer-facing UI for WhatsApp operations
- **Components:**
  - Login/Registration
  - Send Messages UI
  - Chat Management
  - API Key Management
  - Usage Statistics

---

## 5. System Components

### 5.1 Component 1: Management System

#### 5.1.1 Admin Panel (Frontend)

**Technology:** Next.js 14+ with App Router

**Features:**
1. **Server Management**
   - Add new VPS server (manual or auto-provision)
   - Remove/deactivate server
   - View server list with metrics
   - Server health status (CPU, RAM, disk, active connections)
   - Rebalance customers across servers

2. **Customer Management**
   - Create/suspend/delete customers
   - View customer details (plan, usage, server assignment)
   - Manual server assignment
   - API key regeneration
   - Usage analytics per customer

3. **Monitoring Dashboard**
   - Global metrics (total customers, servers, messages/day)
   - Server utilization charts
   - Alert management
   - System logs viewer

4. **Billing & Analytics**
   - Revenue dashboard
   - Usage reports
   - Cost analysis per server
   - Customer tier distribution

**UI Components:**
```typescript
pages/
├── dashboard/              # Overview
├── servers/
│   ├── list/              # Server list table
│   ├── add/               # Add server form
│   └── [id]/              # Server detail & metrics
├── customers/
│   ├── list/              # Customer list
│   ├── create/            # Create customer
│   └── [id]/              # Customer detail
├── monitoring/
│   ├── metrics/           # Global metrics
│   ├── logs/              # System logs
│   └── alerts/            # Alerts management
└── billing/
    ├── revenue/           # Revenue dashboard
    └── usage/             # Usage reports
```

#### 5.1.2 Management API (Backend)

**Technology:** Go (Fiber framework)

**Responsibilities:**
1. **Server Registry**
   - CRUD operations for servers
   - Health check scheduling
   - Capacity tracking
   - Auto-scaling triggers

2. **Customer Routing**
   - Assign customer to optimal server
   - Proxy requests to correct worker
   - Load balancing logic
   - Rebalancing customers

3. **Authentication & Authorization**
   - Admin authentication (JWT)
   - Customer API key validation
   - Role-based access control (RBAC)
   - Session management

4. **Rate Limiting**
   - Per customer rate limits (based on plan)
   - Per server rate limits
   - Global rate limiting
   - Abuse detection

5. **Audit Logging**
   - All admin actions
   - Customer API calls
   - System events
   - Security events

6. **Monitoring & Alerting**
   - Collect metrics from workers
   - Alert on threshold breaches
   - Webhook notifications
   - Email/Slack alerts

**API Endpoints:**
```
# Server Management
POST   /api/admin/servers                 # Add server
GET    /api/admin/servers                 # List servers
GET    /api/admin/servers/:id             # Get server detail
PUT    /api/admin/servers/:id             # Update server
DELETE /api/admin/servers/:id             # Remove server
POST   /api/admin/servers/:id/rebalance   # Rebalance customers

# Customer Management
POST   /api/admin/customers                # Create customer
GET    /api/admin/customers                # List customers
GET    /api/admin/customers/:id            # Get customer detail
PUT    /api/admin/customers/:id            # Update customer
DELETE /api/admin/customers/:id            # Delete customer
POST   /api/admin/customers/:id/reassign   # Reassign to different server
POST   /api/admin/customers/:id/regenerate-key  # Regenerate API key

# Customer Routing (Proxy)
POST   /api/v1/send/message                # Proxied to worker
POST   /api/v1/send/image                  # Proxied to worker
GET    /api/v1/chats                       # Proxied to worker
... (all customer-facing endpoints)

# Monitoring
GET    /api/admin/metrics/global           # Global metrics
GET    /api/admin/metrics/servers/:id      # Server metrics
GET    /api/admin/logs                     # System logs
GET    /api/admin/alerts                   # Active alerts

# Health
GET    /health                             # Management system health
GET    /api/admin/servers/:id/health       # Worker health check
```

### 5.2 Component 2: Worker Backend

**Technology:** Go (refactored from current codebase)

**Key Changes from Current Codebase:**
1. **Multi-instance Manager** - Handle multiple WhatsApp connections per process
2. **Customer Isolation** - Separate context per customer
3. **Registration** - Auto-register with management system
4. **Health Reporting** - Periodic health reports to management
5. **Graceful Shutdown** - Handle customer migration

**Architecture:**
```go
type WorkerNode struct {
    ID              string
    ManagementURL   string
    APIKey          string
    MaxCapacity     int
    Instances       map[string]*WhatsAppInstance
    HealthReporter  *HealthReporter
}

type WhatsAppInstance struct {
    CustomerID      string
    Client          *whatsmeow.Client
    DB              *sqlstore.Container
    EventHandlers   map[string]func(interface{})
    Status          string  // connected, disconnected, scanning
    LastActivity    time.Time
}

type InstanceManager struct {
    instances       sync.Map  // customerID -> *WhatsAppInstance
    maxInstances    int
    shutdownChan    chan struct{}
}

func (im *InstanceManager) CreateInstance(customerID string) error
func (im *InstanceManager) RemoveInstance(customerID string) error
func (im *InstanceManager) GetInstance(customerID string) (*WhatsAppInstance, error)
func (im *InstanceManager) ListInstances() []*WhatsAppInstance
func (im *InstanceManager) Shutdown() error
```

**API Endpoints (Worker Internal):**
```
# Instance Management (called by Management System)
POST   /internal/instances                  # Create WhatsApp instance
DELETE /internal/instances/:customer_id     # Remove instance
GET    /internal/instances                  # List instances
GET    /internal/instances/:customer_id     # Get instance status

# Customer Operations (called via proxy from Management)
POST   /api/send/message                    # Send message
POST   /api/send/image                      # Send image
GET    /api/chats                           # List chats
... (all existing endpoints)

# Health
GET    /health                              # Worker health
GET    /metrics                             # Prometheus metrics
```

### 5.3 Component 3: Customer Dashboard

**Technology:** Next.js 14+ with App Router

**Features:**
1. **Authentication**
   - Login/Register
   - Password reset
   - API key management
   - Session management

2. **WhatsApp Connection**
   - QR code login
   - Phone pairing
   - Connection status
   - Disconnect/reconnect

3. **Messaging**
   - Send text, image, video, file
   - Send contact, location, poll
   - Message history
   - Chat list

4. **Account Management**
   - Profile settings
   - API key viewing/regeneration
   - Usage statistics
   - Billing information

5. **Developer Tools**
   - API documentation
   - Webhook configuration
   - API playground
   - Code examples

**Pages:**
```
app/
├── (auth)/
│   ├── login/page.tsx
│   ├── register/page.tsx
│   └── forgot-password/page.tsx
├── (dashboard)/
│   ├── layout.tsx
│   ├── page.tsx                # Overview
│   ├── connect/page.tsx        # WhatsApp connection
│   ├── send/page.tsx           # Send messages
│   ├── chats/page.tsx          # Chat list
│   ├── messages/[jid]/page.tsx # Message history
│   ├── account/page.tsx        # Account settings
│   ├── api-keys/page.tsx       # API key management
│   ├── usage/page.tsx          # Usage statistics
│   └── docs/page.tsx           # API documentation
└── api/                        # API routes (proxy to management)
```

---

## 6. Security Requirements

### 6.1 Authentication & Authorization

#### 6.1.1 Multi-Level Authentication

**Level 1: Customer Authentication**
```
Method: API Key (X-API-Key header)
Format: ak_live_32_random_characters
Storage: PostgreSQL (hashed with bcrypt)
Rotation: Supported via dashboard/API
Rate Limit: Per API key based on plan
```

**Level 2: Admin Authentication**
```
Method: JWT (OAuth 2.0)
Provider: NextAuth.js
Storage: HTTP-only cookies
Expiration: 24 hours
Refresh: 7 days
MFA: Optional (TOTP)
```

**Level 3: Inter-service Authentication**
```
Method: Service tokens (internal API calls)
Format: Bearer token (JWT)
Validation: Shared secret between management & workers
Rotation: Daily
```

#### 6.1.2 API Key Management

**Requirements:**
- ✅ Generate cryptographically secure API keys (crypto.randomBytes)
- ✅ Store hashed version only (bcrypt, cost factor 12)
- ✅ Support key regeneration (invalidate old key)
- ✅ Track key usage (last used, created date)
- ✅ Support multiple keys per customer (future)
- ✅ Key prefix for easy identification (ak_live_, ak_test_)

**Implementation:**
```go
type APIKey struct {
    ID           string
    CustomerID   string
    KeyHash      string    // bcrypt hash
    Prefix       string    // ak_live_, ak_test_
    Name         string    // Optional: "Production", "Development"
    LastUsedAt   *time.Time
    CreatedAt    time.Time
    RevokedAt    *time.Time
}

func GenerateAPIKey() (plainKey string, hash string, prefix string, err error) {
    // Generate 32 random bytes
    randomBytes := make([]byte, 32)
    _, err = crypto.Read(randomBytes)

    // Encode to base58 (no ambiguous characters)
    encoded := base58.Encode(randomBytes)

    prefix = "ak_live_"
    plainKey = prefix + encoded

    // Hash for storage
    hashBytes, err := bcrypt.GenerateFromPassword([]byte(plainKey), 12)
    hash = string(hashBytes)

    return plainKey, hash, prefix, nil
}
```

### 6.2 Rate Limiting

#### 6.2.1 Per-Customer Rate Limits

**Tier-based Limits:**
```yaml
Basic Plan:
  requests_per_minute: 60
  requests_per_hour: 1000
  requests_per_day: 10000
  messages_per_day: 1000

Pro Plan:
  requests_per_minute: 300
  requests_per_hour: 5000
  requests_per_day: 50000
  messages_per_day: 5000

Business Plan:
  requests_per_minute: 600
  requests_per_hour: 15000
  requests_per_day: 150000
  messages_per_day: 20000

Enterprise Plan:
  requests_per_minute: unlimited
  requests_per_hour: unlimited
  requests_per_day: unlimited
  messages_per_day: unlimited
```

**Implementation:**
```go
// Use Redis for distributed rate limiting
type RateLimiter struct {
    redis *redis.Client
}

func (rl *RateLimiter) CheckLimit(customerID string, limit RateLimit) (allowed bool, err error) {
    key := fmt.Sprintf("ratelimit:%s:%s", customerID, limit.Window)

    // Use Redis INCR with EXPIRE
    count, err := rl.redis.Incr(ctx, key).Result()
    if err != nil {
        return false, err
    }

    if count == 1 {
        // Set expiration on first request
        rl.redis.Expire(ctx, key, limit.Duration)
    }

    return count <= limit.Max, nil
}
```

#### 6.2.2 DDoS Protection

**Requirements:**
- ✅ Global rate limiting per IP (100 req/min)
- ✅ Connection rate limiting (10 new connections/sec per IP)
- ✅ Request size limits (10MB max)
- ✅ Automatic IP blocking on abuse (10 min ban after 5 violations)
- ✅ Cloudflare/CDN integration for L7 DDoS protection

### 6.3 Data Security

#### 6.3.1 Encryption

**At Rest:**
```yaml
Database:
  - PostgreSQL with encryption enabled
  - API keys: bcrypt hashed (never stored plain)
  - Customer data: AES-256 encryption for PII
  - Backups: Encrypted with GPG

File Storage:
  - Media files: Encrypted at rest (S3 with SSE)
  - Database backups: GPG encrypted
```

**In Transit:**
```yaml
External:
  - HTTPS/TLS 1.3 only
  - Certificate: Let's Encrypt / AWS ACM
  - HSTS enabled

Internal:
  - mTLS between management ↔ workers
  - VPN/private network for inter-VPS communication
```

#### 6.3.2 Sensitive Data Handling

**Requirements:**
- ✅ Never log API keys, passwords, tokens
- ✅ Mask sensitive data in logs (phone numbers last 4 digits only)
- ✅ Separate database for audit logs (append-only)
- ✅ Regular security audits
- ✅ GDPR compliance (right to delete)

**Log Sanitization:**
```go
func SanitizeLog(message string) string {
    // Mask API keys
    message = regexp.MustCompile(`ak_live_[a-zA-Z0-9]+`).
        ReplaceAllString(message, "ak_live_***")

    // Mask phone numbers (show last 4 only)
    message = regexp.MustCompile(`\+?\d{10,15}`).
        ReplaceAllStringFunc(message, func(phone string) string {
            if len(phone) > 4 {
                return "****" + phone[len(phone)-4:]
            }
            return "****"
        })

    return message
}
```

### 6.4 Input Validation

**Requirements:**
- ✅ Validate all inputs at API gateway level
- ✅ Use existing ozzo-validation framework
- ✅ SQL injection prevention (use parameterized queries)
- ✅ XSS prevention (sanitize HTML inputs)
- ✅ Path traversal prevention (validate file paths)
- ✅ Command injection prevention (validate exec parameters)

**Enhanced Validation:**
```go
type SendMessageRequest struct {
    Phone   string `json:"phone"`
    Message string `json:"message"`
}

func (r SendMessageRequest) Validate() error {
    return validation.ValidateStruct(&r,
        // Phone: international format, max 15 digits
        validation.Field(&r.Phone,
            validation.Required,
            validation.Match(regexp.MustCompile(`^\+[1-9]\d{1,14}$`)).
                Error("must be valid international phone number"),
        ),

        // Message: max 4096 chars, no null bytes
        validation.Field(&r.Message,
            validation.Required,
            validation.Length(1, 4096),
            validation.By(func(value interface{}) error {
                msg := value.(string)
                if strings.Contains(msg, "\x00") {
                    return errors.New("message contains invalid characters")
                }
                return nil
            }),
        ),
    )
}
```

### 6.5 Audit Logging

**Requirements:**
- ✅ Log all admin actions (create/delete/update)
- ✅ Log customer API calls (endpoint, response code, latency)
- ✅ Log authentication events (login, logout, failed attempts)
- ✅ Log security events (rate limit exceeded, invalid API key)
- ✅ Immutable logs (append-only, separate database)
- ✅ Retention: 90 days minimum

**Audit Log Schema:**
```sql
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    event_type VARCHAR(50) NOT NULL,  -- admin_action, api_call, auth_event, security_event
    actor_type VARCHAR(20) NOT NULL,  -- admin, customer, system
    actor_id UUID,
    action VARCHAR(100) NOT NULL,     -- create_customer, send_message, login_failed
    resource_type VARCHAR(50),        -- customer, server, message
    resource_id VARCHAR(255),
    ip_address INET,
    user_agent TEXT,
    metadata JSONB,                   -- Additional context
    status VARCHAR(20),               -- success, failure
    error_message TEXT
);

-- Index for fast queries
CREATE INDEX idx_audit_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX idx_audit_actor ON audit_logs(actor_type, actor_id);
CREATE INDEX idx_audit_event ON audit_logs(event_type);
```

**Usage:**
```go
func LogAuditEvent(event AuditEvent) {
    query := `
        INSERT INTO audit_logs (
            event_type, actor_type, actor_id, action,
            resource_type, resource_id, ip_address,
            user_agent, metadata, status, error_message
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `

    db.Exec(query,
        event.EventType,
        event.ActorType,
        event.ActorID,
        event.Action,
        event.ResourceType,
        event.ResourceID,
        event.IPAddress,
        event.UserAgent,
        event.Metadata,
        event.Status,
        event.ErrorMessage,
    )
}

// Example usage
LogAuditEvent(AuditEvent{
    EventType: "api_call",
    ActorType: "customer",
    ActorID: customerID,
    Action: "send_message",
    ResourceType: "message",
    IPAddress: c.IP(),
    UserAgent: c.Get("User-Agent"),
    Status: "success",
})
```

### 6.6 Security Headers

**Requirements:**
```go
// Management & Customer Dashboard
app.Use(func(c *fiber.Ctx) error {
    c.Set("X-Content-Type-Options", "nosniff")
    c.Set("X-Frame-Options", "DENY")
    c.Set("X-XSS-Protection", "1; mode=block")
    c.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
    c.Set("Content-Security-Policy",
        "default-src 'self'; "+
        "script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
        "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
        "img-src 'self' data: https:; "+
        "font-src 'self' https://fonts.gstatic.com;")
    c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
    c.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

    return c.Next()
})
```

### 6.7 Dependency Security

**Requirements:**
- ✅ Regular dependency updates (weekly automated PR)
- ✅ Security scanning (GitHub Dependabot, Snyk)
- ✅ No dependencies with known critical vulnerabilities
- ✅ Minimal dependencies principle
- ✅ Vendor dependencies for Go (go mod vendor)

**GitHub Actions Workflow:**
```yaml
name: Security Scan

on:
  push:
  schedule:
    - cron: '0 0 * * 0'  # Weekly

jobs:
  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      # Go security scan
      - name: Run Gosec
        run: |
          go install github.com/securego/gosec/v2/cmd/gosec@latest
          gosec ./...

      # Dependency audit
      - name: Go Vulnerability Check
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./...

      # npm audit
      - name: npm Audit
        working-directory: ./frontend
        run: npm audit --audit-level=high
```

---

## 7. Folder Structure

### 7.1 New Repository Structure

```
whatsapp-saas-platform/
│
├── backend/                              # All backend services
│   │
│   ├── management/                       # Management System
│   │   ├── cmd/
│   │   │   └── server/
│   │   │       └── main.go              # Entry point
│   │   ├── internal/
│   │   │   ├── api/                     # HTTP handlers
│   │   │   │   ├── admin/               # Admin endpoints
│   │   │   │   │   ├── server_handler.go
│   │   │   │   │   ├── customer_handler.go
│   │   │   │   │   └── metrics_handler.go
│   │   │   │   ├── proxy/               # Customer request proxy
│   │   │   │   │   └── proxy_handler.go
│   │   │   │   └── middleware/
│   │   │   │       ├── auth.go
│   │   │   │       ├── ratelimit.go
│   │   │   │       └── audit.go
│   │   │   ├── domain/                  # Business logic
│   │   │   │   ├── server/
│   │   │   │   │   ├── model.go
│   │   │   │   │   ├── repository.go
│   │   │   │   │   └── service.go
│   │   │   │   ├── customer/
│   │   │   │   │   ├── model.go
│   │   │   │   │   ├── repository.go
│   │   │   │   │   └── service.go
│   │   │   │   ├── auth/
│   │   │   │   └── billing/
│   │   │   ├── infrastructure/
│   │   │   │   ├── database/
│   │   │   │   │   └── postgres.go
│   │   │   │   ├── cache/
│   │   │   │   │   └── redis.go
│   │   │   │   └── monitoring/
│   │   │   │       ├── prometheus.go
│   │   │   │       └── healthcheck.go
│   │   │   └── pkg/
│   │   │       ├── config/
│   │   │       ├── logger/
│   │   │       ├── validator/
│   │   │       └── errors/
│   │   ├── migrations/                  # Database migrations
│   │   │   ├── 001_initial_schema.up.sql
│   │   │   ├── 001_initial_schema.down.sql
│   │   │   └── ...
│   │   ├── Dockerfile
│   │   ├── go.mod
│   │   ├── go.sum
│   │   └── README.md
│   │
│   └── worker/                          # Worker Backend (Refactored from current codebase)
│       ├── cmd/
│       │   └── worker/
│       │       └── main.go
│       ├── internal/
│       │   ├── api/                     # HTTP handlers
│       │   │   ├── send/
│       │   │   │   ├── text_handler.go
│       │   │   │   ├── image_handler.go
│       │   │   │   └── ...
│       │   │   ├── chat/
│       │   │   ├── message/
│       │   │   ├── group/
│       │   │   └── middleware/
│       │   ├── domain/                  # Business logic (from current src/domains)
│       │   │   ├── app/
│       │   │   ├── send/
│       │   │   ├── chat/
│       │   │   ├── message/
│       │   │   ├── user/
│       │   │   └── group/
│       │   ├── infrastructure/          # From current src/infrastructure
│       │   │   ├── whatsapp/
│       │   │   │   ├── instance_manager.go  # NEW: Multi-instance support
│       │   │   │   ├── client.go
│       │   │   │   ├── event_handler.go
│       │   │   │   └── ...
│       │   │   ├── chatstorage/
│       │   │   └── webhook/
│       │   ├── usecase/                 # From current src/usecase
│       │   │   ├── app.go
│       │   │   ├── send.go
│       │   │   └── ...
│       │   └── pkg/
│       │       ├── config/
│       │       ├── logger/
│       │       ├── utils/
│       │       └── validation/
│       ├── Dockerfile
│       ├── go.mod
│       ├── go.sum
│       └── README.md
│
├── frontend/                            # All frontend applications
│   │
│   ├── admin-panel/                     # Admin Dashboard
│   │   ├── app/
│   │   │   ├── (auth)/
│   │   │   │   └── login/page.tsx
│   │   │   ├── (dashboard)/
│   │   │   │   ├── layout.tsx
│   │   │   │   ├── page.tsx            # Overview
│   │   │   │   ├── servers/
│   │   │   │   │   ├── page.tsx        # Server list
│   │   │   │   │   ├── add/page.tsx    # Add server form
│   │   │   │   │   └── [id]/page.tsx   # Server detail
│   │   │   │   ├── customers/
│   │   │   │   │   ├── page.tsx
│   │   │   │   │   ├── create/page.tsx
│   │   │   │   │   └── [id]/page.tsx
│   │   │   │   ├── monitoring/
│   │   │   │   │   ├── metrics/page.tsx
│   │   │   │   │   ├── logs/page.tsx
│   │   │   │   │   └── alerts/page.tsx
│   │   │   │   └── billing/
│   │   │   │       ├── revenue/page.tsx
│   │   │   │       └── usage/page.tsx
│   │   │   └── api/                    # API routes
│   │   │       └── auth/
│   │   │           └── [...nextauth]/route.ts
│   │   ├── components/
│   │   │   ├── ui/                     # shadcn/ui components
│   │   │   ├── charts/                 # Chart components
│   │   │   ├── tables/                 # Data tables
│   │   │   └── forms/                  # Form components
│   │   ├── lib/
│   │   │   ├── api.ts                  # API client
│   │   │   ├── auth.ts                 # Auth config
│   │   │   └── utils.ts
│   │   ├── public/
│   │   ├── .env.local
│   │   ├── next.config.js
│   │   ├── package.json
│   │   ├── tsconfig.json
│   │   └── README.md
│   │
│   └── customer-dashboard/              # Customer Dashboard
│       ├── app/
│       │   ├── (auth)/
│       │   │   ├── login/page.tsx
│       │   │   ├── register/page.tsx
│       │   │   └── forgot-password/page.tsx
│       │   ├── (dashboard)/
│       │   │   ├── layout.tsx
│       │   │   ├── page.tsx            # Overview
│       │   │   ├── connect/page.tsx    # WhatsApp connection
│       │   │   ├── send/page.tsx       # Send messages
│       │   │   ├── chats/page.tsx      # Chat list
│       │   │   ├── messages/
│       │   │   │   └── [jid]/page.tsx  # Message history
│       │   │   ├── account/
│       │   │   │   ├── profile/page.tsx
│       │   │   │   ├── api-keys/page.tsx
│       │   │   │   └── billing/page.tsx
│       │   │   ├── usage/page.tsx
│       │   │   └── docs/page.tsx       # API documentation
│       │   └── api/
│       ├── components/
│       │   ├── ui/
│       │   ├── whatsapp/               # WhatsApp-specific components
│       │   └── forms/
│       ├── lib/
│       │   ├── api.ts
│       │   └── utils.ts
│       ├── public/
│       ├── .env.local
│       ├── next.config.js
│       ├── package.json
│       ├── tsconfig.json
│       └── README.md
│
├── infrastructure/                      # DevOps & Infrastructure
│   ├── docker/
│   │   ├── management.Dockerfile
│   │   ├── worker.Dockerfile
│   │   └── docker-compose.yml
│   ├── kubernetes/
│   │   ├── management/
│   │   │   ├── deployment.yaml
│   │   │   ├── service.yaml
│   │   │   └── ingress.yaml
│   │   └── worker/
│   │       ├── deployment.yaml
│   │       ├── service.yaml
│   │       └── hpa.yaml
│   ├── ansible/
│   │   ├── deploy-worker.yml           # Deploy worker to VPS
│   │   ├── provision-vps.yml           # Setup new VPS
│   │   └── inventory/
│   ├── terraform/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── modules/
│   │       ├── vps/
│   │       ├── database/
│   │       └── networking/
│   └── monitoring/
│       ├── prometheus/
│       │   └── prometheus.yml
│       └── grafana/
│           └── dashboards/
│
├── docs/                                # Documentation
│   ├── architecture/
│   │   ├── overview.md
│   │   ├── security.md
│   │   └── scaling.md
│   ├── api/
│   │   ├── management-api.md
│   │   ├── customer-api.md
│   │   └── openapi.yaml
│   ├── deployment/
│   │   ├── production.md
│   │   ├── development.md
│   │   └── troubleshooting.md
│   └── guides/
│       ├── adding-server.md
│       ├── customer-onboarding.md
│       └── monitoring.md
│
├── scripts/                             # Utility scripts
│   ├── migration/
│   │   └── migrate-from-v1.sh          # Migrate from current version
│   ├── deployment/
│   │   ├── deploy-management.sh
│   │   └── deploy-worker.sh
│   └── backup/
│       └── backup-database.sh
│
├── .github/
│   └── workflows/
│       ├── backend-management.yml       # CI/CD for management
│       ├── backend-worker.yml           # CI/CD for worker
│       ├── frontend-admin.yml           # CI/CD for admin panel
│       ├── frontend-customer.yml        # CI/CD for customer dashboard
│       └── security-scan.yml            # Security scanning
│
├── .gitignore
├── README.md                            # Main README
├── PRD.md                               # This document
├── AGENT.md                             # AI development guide
├── CHANGELOG.md
└── LICENSE
```

### 7.2 Migration from Current Structure

**Current → New Mapping:**
```
Current                          →  New Location
─────────────────────────────────────────────────────────────────
src/cmd/                         →  backend/worker/cmd/
src/domains/                     →  backend/worker/internal/domain/
src/infrastructure/              →  backend/worker/internal/infrastructure/
src/usecase/                     →  backend/worker/internal/usecase/
src/ui/rest/                     →  backend/worker/internal/api/
src/ui/mcp/                      →  backend/worker/internal/api/mcp/ (keep or deprecate)
src/validations/                 →  backend/worker/internal/pkg/validation/
src/pkg/                         →  backend/worker/internal/pkg/
src/config/                      →  backend/worker/internal/pkg/config/
src/views/                       →  DEPRECATED (replaced by customer-dashboard)

NEW COMPONENTS:
backend/management/              →  Management System (new)
frontend/admin-panel/            →  Admin Dashboard (new)
frontend/customer-dashboard/     →  Customer Dashboard (new, replaces src/views)
infrastructure/                  →  DevOps files (new)
```

---

## 8. Database Schema

### 8.1 Central PostgreSQL Schema

```sql
-- ============================================================================
-- MANAGEMENT DATABASE SCHEMA
-- ============================================================================

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_stat_statements";

-- ============================================================================
-- TABLE: servers
-- Purpose: Registry of all worker VPS servers
-- ============================================================================
CREATE TABLE servers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL UNIQUE,           -- e.g., "VPS-1", "VPS-Asia-1"
    ip_address INET NOT NULL,                     -- Server IP
    api_url VARCHAR(255) NOT NULL,                -- http://192.168.1.10:3000
    api_key_hash VARCHAR(255) NOT NULL,           -- Hashed API key for worker auth

    -- Capacity
    max_capacity INTEGER NOT NULL DEFAULT 100,    -- Max customers
    current_load INTEGER NOT NULL DEFAULT 0,      -- Current customers count

    -- Specs
    cpu_cores INTEGER NOT NULL,                   -- 8
    ram_gb INTEGER NOT NULL,                      -- 16
    disk_gb INTEGER,                              -- 100

    -- Status
    status VARCHAR(20) NOT NULL DEFAULT 'active', -- active, inactive, maintenance, error
    health_status VARCHAR(20) DEFAULT 'unknown',  -- healthy, degraded, unhealthy
    last_heartbeat TIMESTAMPTZ,                   -- Last health check

    -- Location
    region VARCHAR(50),                           -- asia-southeast, us-east
    datacenter VARCHAR(100),                      -- Singapore DC1
    provider VARCHAR(50),                         -- digitalocean, vultr, aws

    -- Cost
    cost_per_month DECIMAL(10,2),                 -- 40.00

    -- Metadata
    metadata JSONB,                               -- Additional info
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ                        -- Soft delete
);

CREATE INDEX idx_servers_status ON servers(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_servers_health ON servers(health_status);
CREATE INDEX idx_servers_region ON servers(region);

-- ============================================================================
-- TABLE: customers
-- Purpose: Customer accounts
-- ============================================================================
CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) NOT NULL UNIQUE,
    email_verified_at TIMESTAMPTZ,

    -- WhatsApp
    phone_number VARCHAR(20) UNIQUE,              -- WhatsApp number (after connection)
    whatsapp_jid VARCHAR(100) UNIQUE,             -- WhatsApp JID
    whatsapp_status VARCHAR(20) DEFAULT 'disconnected', -- connected, disconnected, scanning

    -- Assignment
    assigned_server_id UUID REFERENCES servers(id) ON DELETE SET NULL,

    -- Plan & Status
    plan VARCHAR(50) NOT NULL DEFAULT 'basic',    -- basic, pro, business, enterprise
    status VARCHAR(20) NOT NULL DEFAULT 'active', -- active, suspended, deleted

    -- Authentication
    password_hash VARCHAR(255),                   -- bcrypt hash (for dashboard login)

    -- Metadata
    company_name VARCHAR(255),
    full_name VARCHAR(255),
    timezone VARCHAR(50) DEFAULT 'UTC',
    language VARCHAR(10) DEFAULT 'en',

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_customers_email ON customers(email) WHERE deleted_at IS NULL;
CREATE INDEX idx_customers_server ON customers(assigned_server_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_customers_status ON customers(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_customers_plan ON customers(plan);

-- ============================================================================
-- TABLE: api_keys
-- Purpose: Customer API keys for authentication
-- ============================================================================
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,

    key_hash VARCHAR(255) NOT NULL UNIQUE,        -- bcrypt hash of API key
    prefix VARCHAR(20) NOT NULL,                  -- ak_live_, ak_test_
    name VARCHAR(100),                            -- Optional: "Production", "Development"

    -- Permissions (future: granular permissions)
    permissions JSONB DEFAULT '["*"]'::jsonb,     -- ["send:message", "read:chats"]

    -- Usage tracking
    last_used_at TIMESTAMPTZ,
    last_used_ip INET,
    request_count BIGINT DEFAULT 0,

    -- Status
    is_active BOOLEAN DEFAULT true,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ                        -- Optional expiration
);

CREATE INDEX idx_api_keys_customer ON api_keys(customer_id);
CREATE INDEX idx_api_keys_prefix ON api_keys(prefix);
CREATE INDEX idx_api_keys_active ON api_keys(is_active) WHERE revoked_at IS NULL;

-- ============================================================================
-- TABLE: server_metrics
-- Purpose: Server performance metrics (time-series data)
-- ============================================================================
CREATE TABLE server_metrics (
    id BIGSERIAL PRIMARY KEY,
    server_id UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,

    -- Resource usage
    cpu_usage DECIMAL(5,2),                       -- 45.50 (percentage)
    ram_usage DECIMAL(5,2),                       -- 67.20 (percentage)
    disk_usage DECIMAL(5,2),                      -- 30.00 (percentage)

    -- Network
    network_in_mbps DECIMAL(10,2),
    network_out_mbps DECIMAL(10,2),

    -- WhatsApp metrics
    active_connections INTEGER,                   -- Connected customers
    total_instances INTEGER,                      -- Total instances

    -- Message throughput
    messages_per_minute INTEGER,
    errors_per_minute INTEGER,

    -- Response time
    avg_response_time_ms INTEGER,
    p95_response_time_ms INTEGER,
    p99_response_time_ms INTEGER,

    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_server_metrics_server_time ON server_metrics(server_id, recorded_at DESC);
CREATE INDEX idx_server_metrics_time ON server_metrics(recorded_at DESC);

-- Partition by month for better performance (optional)
-- CREATE TABLE server_metrics_y2025m01 PARTITION OF server_metrics
-- FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');

-- ============================================================================
-- TABLE: customer_usage
-- Purpose: Customer usage tracking for billing
-- ============================================================================
CREATE TABLE customer_usage (
    id BIGSERIAL PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    date DATE NOT NULL,

    -- API usage
    api_requests INTEGER DEFAULT 0,

    -- Message counts
    messages_sent INTEGER DEFAULT 0,
    messages_received INTEGER DEFAULT 0,

    -- Media counts
    images_sent INTEGER DEFAULT 0,
    videos_sent INTEGER DEFAULT 0,
    files_sent INTEGER DEFAULT 0,

    -- Bandwidth
    bandwidth_in_mb DECIMAL(10,2) DEFAULT 0,
    bandwidth_out_mb DECIMAL(10,2) DEFAULT 0,

    -- Costs (calculated)
    estimated_cost DECIMAL(10,4) DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(customer_id, date)
);

CREATE INDEX idx_customer_usage_customer_date ON customer_usage(customer_id, date DESC);
CREATE INDEX idx_customer_usage_date ON customer_usage(date DESC);

-- ============================================================================
-- TABLE: audit_logs
-- Purpose: Immutable audit trail
-- ============================================================================
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Event classification
    event_type VARCHAR(50) NOT NULL,              -- admin_action, api_call, auth_event, security_event
    severity VARCHAR(20) DEFAULT 'info',          -- debug, info, warning, error, critical

    -- Actor
    actor_type VARCHAR(20) NOT NULL,              -- admin, customer, system
    actor_id UUID,

    -- Action
    action VARCHAR(100) NOT NULL,                 -- create_customer, send_message, login_failed
    resource_type VARCHAR(50),                    -- customer, server, message
    resource_id VARCHAR(255),

    -- Request info
    ip_address INET,
    user_agent TEXT,
    request_id VARCHAR(100),                      -- Trace ID

    -- Additional context
    metadata JSONB,

    -- Result
    status VARCHAR(20),                           -- success, failure, partial
    error_message TEXT,
    duration_ms INTEGER                           -- Request duration
);

CREATE INDEX idx_audit_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX idx_audit_actor ON audit_logs(actor_type, actor_id, timestamp DESC);
CREATE INDEX idx_audit_event ON audit_logs(event_type, timestamp DESC);
CREATE INDEX idx_audit_severity ON audit_logs(severity, timestamp DESC) WHERE severity IN ('error', 'critical');

-- ============================================================================
-- TABLE: rate_limit_buckets
-- Purpose: Rate limiting (alternative to Redis for persistence)
-- ============================================================================
CREATE TABLE rate_limit_buckets (
    id BIGSERIAL PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    bucket_type VARCHAR(50) NOT NULL,             -- requests_per_minute, messages_per_day
    window_start TIMESTAMPTZ NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    limit_value INTEGER NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(customer_id, bucket_type, window_start)
);

CREATE INDEX idx_rate_limit_customer_window ON rate_limit_buckets(customer_id, bucket_type, window_start DESC);

-- ============================================================================
-- TABLE: webhooks
-- Purpose: Customer webhook configurations
-- ============================================================================
CREATE TABLE webhooks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,

    url VARCHAR(500) NOT NULL,
    secret VARCHAR(255),                          -- HMAC secret

    -- Event subscriptions
    events JSONB NOT NULL DEFAULT '["*"]'::jsonb, -- ["message.received", "message.sent"]

    -- Status
    is_active BOOLEAN DEFAULT true,

    -- Retry config
    max_retries INTEGER DEFAULT 5,
    retry_backoff_ms INTEGER DEFAULT 1000,

    -- Stats
    last_triggered_at TIMESTAMPTZ,
    success_count BIGINT DEFAULT 0,
    failure_count BIGINT DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_webhooks_customer ON webhooks(customer_id);
CREATE INDEX idx_webhooks_active ON webhooks(is_active);

-- ============================================================================
-- TABLE: alerts
-- Purpose: System alerts and notifications
-- ============================================================================
CREATE TABLE alerts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    -- Alert details
    type VARCHAR(50) NOT NULL,                    -- server_down, high_cpu, rate_limit_exceeded
    severity VARCHAR(20) NOT NULL,                -- info, warning, critical
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,

    -- Related resources
    server_id UUID REFERENCES servers(id) ON DELETE CASCADE,
    customer_id UUID REFERENCES customers(id) ON DELETE CASCADE,

    -- Status
    status VARCHAR(20) DEFAULT 'active',          -- active, acknowledged, resolved
    acknowledged_by UUID,                         -- Admin user ID
    acknowledged_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,

    -- Metadata
    metadata JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alerts_status ON alerts(status, severity, created_at DESC);
CREATE INDEX idx_alerts_server ON alerts(server_id, created_at DESC);
CREATE INDEX idx_alerts_customer ON alerts(customer_id, created_at DESC);

-- ============================================================================
-- TABLE: admin_users
-- Purpose: Admin panel authentication
-- ============================================================================
CREATE TABLE admin_users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,

    full_name VARCHAR(255),
    role VARCHAR(50) NOT NULL DEFAULT 'admin',    -- super_admin, admin, viewer

    -- MFA
    mfa_enabled BOOLEAN DEFAULT false,
    mfa_secret VARCHAR(255),

    -- Status
    is_active BOOLEAN DEFAULT true,

    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ
);

CREATE INDEX idx_admin_users_email ON admin_users(email) WHERE is_active = true;

-- ============================================================================
-- VIEWS
-- ============================================================================

-- Server summary view
CREATE VIEW server_summary AS
SELECT
    s.id,
    s.name,
    s.status,
    s.health_status,
    s.current_load,
    s.max_capacity,
    ROUND((s.current_load::DECIMAL / s.max_capacity * 100), 2) as utilization_percent,
    s.region,
    s.provider,
    s.cost_per_month,
    COUNT(c.id) as actual_customer_count,
    s.last_heartbeat,
    CASE
        WHEN s.last_heartbeat > NOW() - INTERVAL '5 minutes' THEN 'online'
        WHEN s.last_heartbeat > NOW() - INTERVAL '15 minutes' THEN 'degraded'
        ELSE 'offline'
    END as connectivity_status
FROM servers s
LEFT JOIN customers c ON c.assigned_server_id = s.id AND c.deleted_at IS NULL
WHERE s.deleted_at IS NULL
GROUP BY s.id;

-- Customer overview
CREATE VIEW customer_overview AS
SELECT
    c.id,
    c.email,
    c.full_name,
    c.company_name,
    c.phone_number,
    c.plan,
    c.status,
    c.whatsapp_status,
    s.name as assigned_server_name,
    s.region as server_region,
    c.created_at,
    c.last_login_at,
    COUNT(DISTINCT ak.id) as api_key_count,
    SUM(CASE WHEN cu.date = CURRENT_DATE THEN cu.messages_sent ELSE 0 END) as messages_today
FROM customers c
LEFT JOIN servers s ON s.id = c.assigned_server_id
LEFT JOIN api_keys ak ON ak.customer_id = c.id AND ak.revoked_at IS NULL
LEFT JOIN customer_usage cu ON cu.customer_id = c.id
WHERE c.deleted_at IS NULL
GROUP BY c.id, s.id;

-- ============================================================================
-- FUNCTIONS & TRIGGERS
-- ============================================================================

-- Update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Apply trigger to all tables with updated_at
CREATE TRIGGER update_servers_updated_at BEFORE UPDATE ON servers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_customers_updated_at BEFORE UPDATE ON customers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_webhooks_updated_at BEFORE UPDATE ON webhooks
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Sync server current_load with actual customer count
CREATE OR REPLACE FUNCTION sync_server_load()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE servers
    SET current_load = (
        SELECT COUNT(*)
        FROM customers
        WHERE assigned_server_id = NEW.assigned_server_id
        AND deleted_at IS NULL
    )
    WHERE id = NEW.assigned_server_id;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER sync_server_load_on_customer_change
    AFTER INSERT OR UPDATE OF assigned_server_id OR DELETE ON customers
    FOR EACH ROW
    EXECUTE FUNCTION sync_server_load();

-- ============================================================================
-- INDEXES FOR PERFORMANCE
-- ============================================================================

-- Composite indexes for common queries
CREATE INDEX idx_customers_server_status ON customers(assigned_server_id, status)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_audit_logs_actor_action ON audit_logs(actor_type, actor_id, action, timestamp DESC);

-- Partial indexes for active records
CREATE INDEX idx_api_keys_customer_active ON api_keys(customer_id)
    WHERE is_active = true AND revoked_at IS NULL;

-- ============================================================================
-- SAMPLE DATA (for development)
-- ============================================================================

-- Insert default admin user (password: admin123)
-- Note: In production, set this via secure method
INSERT INTO admin_users (email, password_hash, full_name, role)
VALUES (
    'admin@example.com',
    '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYJq7OEe9Sq', -- bcrypt of 'admin123'
    'System Administrator',
    'super_admin'
);

-- ============================================================================
-- DATABASE MAINTENANCE
-- ============================================================================

-- Auto-vacuum settings for high-write tables
ALTER TABLE audit_logs SET (autovacuum_vacuum_scale_factor = 0.05);
ALTER TABLE server_metrics SET (autovacuum_vacuum_scale_factor = 0.05);
ALTER TABLE customer_usage SET (autovacuum_vacuum_scale_factor = 0.1);

-- Retention policy (delete old data)
-- Run this daily via cron
-- DELETE FROM server_metrics WHERE recorded_at < NOW() - INTERVAL '90 days';
-- DELETE FROM audit_logs WHERE timestamp < NOW() - INTERVAL '90 days' AND severity NOT IN ('error', 'critical');
```

### 8.2 Worker Local Database (SQLite)

Each worker maintains local SQLite databases (existing schema):
- **Main DB:** WhatsApp connection data per customer
- **Chat Storage DB:** Message history per customer

**Isolation Strategy:**
```
worker/storages/
├── customer_uuid1/
│   ├── whatsapp.db        # WhatsApp connection
│   └── chatstorage.db     # Message history
├── customer_uuid2/
│   ├── whatsapp.db
│   └── chatstorage.db
└── ...
```

---

## 9. API Specifications

### 9.1 Management API (Admin)

**Base URL:** `https://management.yourapp.com/api/admin`

**Authentication:** JWT Bearer token

#### Servers

```http
POST /servers
Authorization: Bearer {jwt_token}
Content-Type: application/json

{
  "name": "VPS-Asia-1",
  "ip_address": "192.168.1.10",
  "max_capacity": 100,
  "cpu_cores": 8,
  "ram_gb": 16,
  "region": "asia-southeast",
  "provider": "digitalocean",
  "cost_per_month": 40.00
}

Response 201:
{
  "success": true,
  "data": {
    "id": "uuid",
    "name": "VPS-Asia-1",
    "api_key": "ak_server_xxx...xxx",  // Only shown once!
    "status": "initializing"
  }
}
```

```http
GET /servers
Authorization: Bearer {jwt_token}

Response 200:
{
  "success": true,
  "data": [
    {
      "id": "uuid",
      "name": "VPS-Asia-1",
      "status": "active",
      "health_status": "healthy",
      "current_load": 85,
      "max_capacity": 100,
      "utilization_percent": 85.0,
      "last_heartbeat": "2025-11-19T10:30:00Z"
    }
  ],
  "meta": {
    "total": 10,
    "page": 1,
    "per_page": 20
  }
}
```

#### Customers

```http
POST /customers
Authorization: Bearer {jwt_token}
Content-Type: application/json

{
  "email": "customer@example.com",
  "full_name": "John Doe",
  "company_name": "Acme Inc",
  "plan": "pro",
  "assign_server": "auto"  // or specific server_id
}

Response 201:
{
  "success": true,
  "data": {
    "id": "uuid",
    "email": "customer@example.com",
    "api_key": "ak_live_xxx...xxx",  // Only shown once!
    "assigned_server_id": "uuid",
    "dashboard_url": "https://dashboard.yourapp.com",
    "dashboard_password": "temp_password_123"  // Temporary, must change on first login
  }
}
```

### 9.2 Customer API

**Base URL:** `https://api.yourapp.com/api/v1`

**Authentication:** API Key (X-API-Key header)

#### Send Message

```http
POST /send/message
X-API-Key: ak_live_xxx...xxx
Content-Type: application/json

{
  "phone": "+628123456789",
  "message": "Hello from API"
}

Response 200:
{
  "success": true,
  "data": {
    "message_id": "3EB0xxx",
    "status": "sent",
    "timestamp": "2025-11-19T10:30:00Z"
  }
}

Response 429 (Rate Limited):
{
  "success": false,
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Rate limit exceeded. Limit: 60 requests per minute. Try again in 45 seconds.",
    "retry_after": 45
  }
}

Response 401 (Invalid API Key):
{
  "success": false,
  "error": {
    "code": "INVALID_API_KEY",
    "message": "Invalid or expired API key"
  }
}
```

**Rate Limit Headers:**
```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 45
X-RateLimit-Reset: 1637308800
```

---

## 10. Migration Strategy

### 10.1 Zero-Downtime Migration Plan

**Phase 1: Preparation (Week 1-2)**
1. ✅ Setup new repository structure
2. ✅ Create migration scripts
3. ✅ Deploy management system (parallel to existing)
4. ✅ Deploy test worker instances
5. ✅ Test thoroughly in staging

**Phase 2: Gradual Migration (Week 3-4)**
1. ✅ Deploy management system to production
2. ✅ Register existing server as VPS-1 in management
3. ✅ Migrate 10% of customers to new system
4. ✅ Monitor for 24 hours
5. ✅ Migrate 50% of customers
6. ✅ Monitor for 48 hours
7. ✅ Migrate 100% of customers

**Phase 3: Cleanup (Week 5)**
1. ✅ Deprecate old endpoints (with 30-day notice)
2. ✅ Archive old codebase
3. ✅ Update documentation
4. ✅ Customer communication

### 10.2 Rollback Plan

If critical issues occur:
1. Switch DNS back to old system
2. Restore database from backup
3. Notify customers
4. Post-mortem analysis

---

## 11. Success Metrics

### 11.1 Technical Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| API Response Time | < 200ms (p95) | Prometheus |
| System Uptime | 99.9% | Uptime monitoring |
| Error Rate | < 0.1% | Application logs |
| Database Query Time | < 50ms (p95) | PostgreSQL stats |
| Worker CPU Usage | < 70% average | Server metrics |
| Worker RAM Usage | < 80% average | Server metrics |

### 11.2 Business Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Customer Acquisition Cost | < $10 | Marketing analytics |
| Monthly Recurring Revenue | $17,000 (1000 customers) | Billing system |
| Churn Rate | < 5% monthly | Customer retention |
| Server Utilization | > 80% | Management dashboard |
| Cost per Customer | < $0.60/month | Infrastructure costs |
| Gross Margin | > 90% | Financial reports |

### 11.3 Security Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Failed Login Attempts | < 1% of total | Audit logs |
| API Key Leaks | 0 | Security scanning |
| Security Patches Applied | < 7 days | Dependency monitoring |
| Vulnerability Severity | No critical/high | Security scans |

---

## 12. Timeline & Phases

### 12.1 Development Timeline

**Phase 1: Foundation (Weeks 1-4)**
- ✅ Week 1-2: Repository restructure + Database schema
- ✅ Week 3-4: Management API core + Worker refactoring

**Phase 2: Core Features (Weeks 5-8)**
- ✅ Week 5-6: Admin panel + Customer dashboard
- ✅ Week 7-8: Security implementation + Testing

**Phase 3: Integration (Weeks 9-10)**
- ✅ Week 9: Integration testing + Performance tuning
- ✅ Week 10: Documentation + Training

**Phase 4: Deployment (Weeks 11-12)**
- ✅ Week 11: Staging deployment + QA
- ✅ Week 12: Production migration + Monitoring

**Total Timeline: 12 weeks (3 months)**

### 12.2 Resource Requirements

**Development Team:**
- 1x Backend Developer (Go) - Full-time
- 1x Frontend Developer (Next.js) - Full-time
- 1x DevOps Engineer - Part-time (50%)
- 1x QA Engineer - Part-time (50%)

**Infrastructure:**
- Development: $100/month
- Staging: $200/month
- Production (initial): $600/month

---

## 13. Risk Management

### 13.1 Technical Risks

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| WhatsApp API changes | High | Medium | Monitor whatsmeow updates, maintain compatibility layer |
| Database performance | High | Low | Implement caching, read replicas, regular optimization |
| Worker crashes | Medium | Medium | Health monitoring, auto-restart, alerting |
| Data loss | Critical | Very Low | Regular backups, replication, disaster recovery plan |

### 13.2 Business Risks

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Customer churn | High | Medium | Excellent support, feature parity, migration assistance |
| Competitor pricing | Medium | High | Focus on reliability, features, support |
| Regulatory changes | High | Low | Monitor WhatsApp ToS, legal compliance |

---

## 14. Success Criteria

### 14.1 Must-Have (MVP)

- ✅ Support 1000+ customers across multiple servers
- ✅ Admin panel with "Add Server" functionality
- ✅ Customer API with authentication
- ✅ Basic monitoring and alerts
- ✅ 99.9% uptime
- ✅ Secure by default (API keys, HTTPS, rate limiting)

### 14.2 Should-Have (V1.1)

- ⚠️ Advanced analytics dashboard
- ⚠️ Automated billing integration
- ⚠️ Customer self-service portal
- ⚠️ Multi-language support
- ⚠️ Advanced webhook features

### 14.3 Nice-to-Have (Future)

- 💡 Mobile app for admin panel
- 💡 AI-powered chatbot support
- 💡 Advanced message scheduling
- 💡 Campaign management
- 💡 CRM integration

---

## 15. Appendix

### 15.1 Glossary

- **VPS:** Virtual Private Server
- **Worker:** Backend instance running on VPS
- **Instance:** Single WhatsApp connection
- **Multi-tenant:** Multiple customers sharing resources
- **SaaS:** Software as a Service
- **JWT:** JSON Web Token
- **API Key:** Authentication credential for API access
- **Rate Limiting:** Restricting request frequency

### 15.2 References

- WhatsApp Business API Documentation
- Go Best Practices
- Next.js Documentation
- PostgreSQL Performance Tuning
- OWASP Security Guidelines

---

**Document Status:** ✅ Ready for Review
**Next Steps:** Create AGENT.md, Start Phase 1 implementation
**Last Updated:** 2025-11-19
