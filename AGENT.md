# AGENT.md
# AI Development Guide for WhatsApp Multi-Server SaaS Platform

**Purpose:** This document provides comprehensive guidance for AI coding assistants (like Claude, GitHub Copilot, etc.) working on this project.

**Last Updated:** 2025-11-19
**Version:** 2.0.0

---

## 📚 Table of Contents

1. [Project Overview](#project-overview)
2. [Development Principles](#development-principles)
3. [Code Standards](#code-standards)
4. [Security Guidelines](#security-guidelines)
5. [Common Patterns](#common-patterns)
6. [Testing Requirements](#testing-requirements)
7. [File Organization](#file-organization)
8. [API Design](#api-design)
9. [Database Operations](#database-operations)
10. [Error Handling](#error-handling)
11. [Deployment](#deployment)
12. [Troubleshooting](#troubleshooting)

---

## 1. Project Overview

### 1.1 Architecture Summary

This is a **multi-server SaaS platform** for WhatsApp Web API with three main components:

```
┌─────────────────────┐
│  Management System  │ (Go + Next.js)
│  - Admin Panel      │
│  - Load Balancer    │
│  - Server Registry  │
└──────────┬──────────┘
           │
    ┌──────┴──────┬──────────┐
    │             │          │
┌───▼──┐      ┌───▼──┐   ┌──▼───┐
│ VPS 1│      │ VPS 2│   │ VPS N│
│Worker│      │Worker│   │Worker│
└──────┘      └──────┘   └──────┘
```

### 1.2 Tech Stack

**Backend:**
- Go 1.24+
- Fiber v2 (web framework)
- PostgreSQL (central database)
- Redis (caching & rate limiting)
- whatsmeow (WhatsApp library)

**Frontend:**
- Next.js 14+ (App Router)
- TypeScript
- Tailwind CSS + shadcn/ui
- TanStack Query (data fetching)
- Zustand (state management)

**Infrastructure:**
- Docker
- Kubernetes (optional)
- Ansible (deployment)
- Prometheus + Grafana (monitoring)

### 1.3 Key Concepts

**Multi-Tenancy:**
- Each customer gets isolated WhatsApp instance
- Instances distributed across multiple VPS workers
- Central management for coordination

**Server Registry:**
- Management system tracks all VPS workers
- Health monitoring & load balancing
- Dynamic server addition/removal

**Security Layers:**
- Customer: API key authentication
- Admin: JWT authentication
- Inter-service: Service tokens

---

## 2. Development Principles

### 2.1 Core Principles

1. **Security First**
   - Never log sensitive data (API keys, passwords, tokens)
   - Always validate and sanitize inputs
   - Use parameterized queries (prevent SQL injection)
   - Implement rate limiting on all public endpoints
   - Hash passwords with bcrypt (cost factor 12)

2. **Clean Architecture**
   - Maintain clear separation of concerns
   - Domain logic independent of frameworks
   - Use interfaces for dependencies
   - Keep business logic in domain/usecase layers

3. **Don't Repeat Yourself (DRY)**
   - Extract common patterns into utilities
   - Reuse existing code before writing new
   - Create shared types/interfaces

4. **Performance Matters**
   - Use database indexes appropriately
   - Implement caching where beneficial
   - Optimize N+1 queries
   - Monitor response times

5. **Fail Gracefully**
   - Never panic in production code
   - Return errors explicitly
   - Log errors with context
   - Provide meaningful error messages

### 2.2 When Adding Features

**ALWAYS:**
- ✅ Check PRD.md for requirements
- ✅ Read existing code in the same domain
- ✅ Follow existing patterns
- ✅ Add validation
- ✅ Add error handling
- ✅ Add logging
- ✅ Write tests
- ✅ Update documentation

**NEVER:**
- ❌ Hardcode credentials
- ❌ Skip input validation
- ❌ Ignore errors
- ❌ Copy-paste without understanding
- ❌ Mix concerns (e.g., business logic in handlers)
- ❌ Use deprecated dependencies

---

## 3. Code Standards

### 3.1 Go Code Standards

#### 3.1.1 Naming Conventions

```go
// Good Examples

// Package names: short, lowercase, no underscores
package server

// Interfaces: start with 'I' for clarity in this project
type ICustomerRepository interface {
    Create(ctx context.Context, customer Customer) error
    GetByID(ctx context.Context, id string) (*Customer, error)
}

// Structs: PascalCase
type CustomerService struct {
    repo ICustomerRepository
    log  *logrus.Logger
}

// Methods: PascalCase
func (s *CustomerService) CreateCustomer(ctx context.Context, req CreateCustomerRequest) error

// Private fields/methods: camelCase
func (s *CustomerService) validateEmail(email string) error

// Constants: PascalCase or ALL_CAPS for exported
const MaxCustomersPerServer = 100
const defaultTimeout = 30 * time.Second

// Errors: start with Err
var ErrCustomerNotFound = errors.New("customer not found")
var ErrServerAtCapacity = errors.New("server at maximum capacity")
```

#### 3.1.2 File Structure

```go
package customer

import (
    "context"
    "time"

    "github.com/google/uuid"
    // Blank line between stdlib and external
    "github.com/your-org/project/internal/domain/server"
    "github.com/your-org/project/internal/pkg/errors"
)

// Constants first
const (
    MaxEmailLength = 255
    MinPasswordLength = 8
)

// Errors next
var (
    ErrInvalidEmail = errors.New("invalid email format")
    ErrWeakPassword = errors.New("password too weak")
)

// Types
type Customer struct {
    ID        uuid.UUID `json:"id" db:"id"`
    Email     string    `json:"email" db:"email"`
    CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Interfaces
type ICustomerRepository interface {
    Create(ctx context.Context, customer Customer) error
}

// Constructor
func NewCustomerService(repo ICustomerRepository) *CustomerService {
    return &CustomerService{
        repo: repo,
    }
}

// Methods
func (s *CustomerService) CreateCustomer(ctx context.Context, req CreateCustomerRequest) error {
    // Implementation
}
```

#### 3.1.3 Error Handling

```go
// Good: Explicit error handling
func (s *ServerService) AddServer(ctx context.Context, req AddServerRequest) (*Server, error) {
    // Validate input
    if err := req.Validate(); err != nil {
        return nil, errors.Wrap(err, "invalid request")
    }

    // Create server
    server, err := s.repo.Create(ctx, req.ToServer())
    if err != nil {
        return nil, errors.Wrap(err, "failed to create server")
    }

    // Deploy worker
    if err := s.deployWorker(ctx, server); err != nil {
        // Rollback on deployment failure
        s.repo.Delete(ctx, server.ID)
        return nil, errors.Wrap(err, "failed to deploy worker")
    }

    return server, nil
}

// Bad: Ignoring errors
func (s *ServerService) AddServer(ctx context.Context, req AddServerRequest) (*Server, error) {
    req.Validate() // ❌ Ignoring error
    server, _ := s.repo.Create(ctx, req.ToServer()) // ❌ Ignoring error
    return server, nil
}
```

#### 3.1.4 Context Usage

```go
// Always accept context as first parameter
func (s *CustomerService) GetCustomer(ctx context.Context, id string) (*Customer, error) {
    // Use context for cancellation
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    // Pass context to repository
    return s.repo.GetByID(ctx, id)
}

// Set timeouts for external operations
func (s *WorkerClient) HealthCheck(serverURL string) error {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    return s.httpClient.Get(ctx, serverURL+"/health")
}
```

#### 3.1.5 Logging

```go
import "github.com/sirupsen/logrus"

// Use structured logging
func (s *CustomerService) CreateCustomer(ctx context.Context, req CreateCustomerRequest) error {
    log := s.log.WithFields(logrus.Fields{
        "action": "create_customer",
        "email":  req.Email,
    })

    log.Info("Creating customer")

    customer, err := s.repo.Create(ctx, req.ToCustomer())
    if err != nil {
        log.WithError(err).Error("Failed to create customer")
        return err
    }

    log.WithField("customer_id", customer.ID).Info("Customer created successfully")
    return nil
}

// NEVER log sensitive data
// ❌ BAD
log.Info("API Key:", apiKey)
log.Info("Password:", password)

// ✅ GOOD
log.Info("API Key generated") // No actual key
log.Info("Password updated")  // No actual password
```

### 3.2 TypeScript/Next.js Code Standards

#### 3.2.1 File Naming

```
// Pages: kebab-case
app/servers/add-server/page.tsx

// Components: PascalCase
components/ServerCard.tsx
components/forms/AddServerForm.tsx

// Utilities: camelCase
lib/apiClient.ts
lib/formatters.ts

// Types: PascalCase
types/Server.ts
types/Customer.ts
```

#### 3.2.2 Component Structure

```typescript
// Good: Functional component with TypeScript
'use client'

import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { ServerService } from '@/lib/api/server'
import type { Server, AddServerRequest } from '@/types/server'

interface AddServerFormProps {
  onSuccess?: (server: Server) => void
  onCancel?: () => void
}

export function AddServerForm({ onSuccess, onCancel }: AddServerFormProps) {
  const [formData, setFormData] = useState<AddServerRequest>({
    name: '',
    ipAddress: '',
    maxCapacity: 100,
  })

  const mutation = useMutation({
    mutationFn: (data: AddServerRequest) => ServerService.create(data),
    onSuccess: (server) => {
      onSuccess?.(server)
    },
    onError: (error) => {
      console.error('Failed to add server:', error)
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    mutation.mutate(formData)
  }

  return (
    <form onSubmit={handleSubmit}>
      {/* Form fields */}
    </form>
  )
}
```

#### 3.2.3 API Client

```typescript
// lib/api/client.ts
import axios, { AxiosInstance } from 'axios'

const apiClient: AxiosInstance = axios.create({
  baseURL: process.env.NEXT_PUBLIC_API_URL,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Request interceptor (add auth token)
apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('auth_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor (handle errors)
apiClient.interceptors.response.use(
  (response) => response.data,
  (error) => {
    if (error.response?.status === 401) {
      // Redirect to login
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

export { apiClient }
```

---

## 4. Security Guidelines

### 4.1 Authentication Implementation

#### 4.1.1 API Key Generation (Go)

```go
package auth

import (
    "crypto/rand"
    "encoding/base64"

    "golang.org/x/crypto/bcrypt"
)

// GenerateAPIKey creates a secure random API key
func GenerateAPIKey(prefix string) (plainKey string, hash string, err error) {
    // Generate 32 random bytes
    randomBytes := make([]byte, 32)
    if _, err = rand.Read(randomBytes); err != nil {
        return "", "", err
    }

    // Encode to base64 (URL-safe)
    encoded := base64.URLEncoding.EncodeToString(randomBytes)

    // Add prefix
    plainKey = prefix + encoded

    // Hash for storage
    hashBytes, err := bcrypt.GenerateFromPassword([]byte(plainKey), 12)
    if err != nil {
        return "", "", err
    }

    return plainKey, string(hashBytes), nil
}

// ValidateAPIKey checks if provided key matches hash
func ValidateAPIKey(plainKey, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plainKey))
    return err == nil
}
```

#### 4.1.2 API Key Middleware (Go)

```go
package middleware

import (
    "github.com/gofiber/fiber/v2"
    "your-project/internal/domain/customer"
)

func APIKeyAuth(customerRepo customer.ICustomerRepository) fiber.Handler {
    return func(c *fiber.Ctx) error {
        apiKey := c.Get("X-API-Key")
        if apiKey == "" {
            return c.Status(401).JSON(fiber.Map{
                "error": "API key required",
            })
        }

        // Validate API key
        cust, err := customerRepo.GetByAPIKey(c.Context(), apiKey)
        if err != nil {
            return c.Status(401).JSON(fiber.Map{
                "error": "Invalid API key",
            })
        }

        // Check if customer is active
        if cust.Status != "active" {
            return c.Status(403).JSON(fiber.Map{
                "error": "Account suspended",
            })
        }

        // Store customer in context
        c.Locals("customer", cust)

        return c.Next()
    }
}
```

### 4.2 Input Validation

#### 4.2.1 Request Validation (Go)

```go
package validation

import (
    "regexp"

    validation "github.com/go-ozzo/ozzo-validation/v4"
    "github.com/go-ozzo/ozzo-validation/v4/is"
)

type AddServerRequest struct {
    Name        string  `json:"name"`
    IPAddress   string  `json:"ip_address"`
    MaxCapacity int     `json:"max_capacity"`
    CPUCores    int     `json:"cpu_cores"`
    RAMGB       int     `json:"ram_gb"`
}

func (r AddServerRequest) Validate() error {
    return validation.ValidateStruct(&r,
        validation.Field(&r.Name,
            validation.Required,
            validation.Length(3, 100),
            validation.Match(regexp.MustCompile(`^[a-zA-Z0-9-]+$`)).
                Error("must contain only letters, numbers, and hyphens"),
        ),
        validation.Field(&r.IPAddress,
            validation.Required,
            is.IPv4,
        ),
        validation.Field(&r.MaxCapacity,
            validation.Required,
            validation.Min(1),
            validation.Max(500),
        ),
        validation.Field(&r.CPUCores,
            validation.Required,
            validation.Min(1),
            validation.Max(128),
        ),
        validation.Field(&r.RAMGB,
            validation.Required,
            validation.Min(1),
            validation.Max(1024),
        ),
    )
}
```

### 4.3 SQL Injection Prevention

```go
// ✅ GOOD: Parameterized queries
func (r *CustomerRepository) GetByEmail(ctx context.Context, email string) (*Customer, error) {
    query := `
        SELECT id, email, full_name, created_at
        FROM customers
        WHERE email = $1 AND deleted_at IS NULL
    `

    var customer Customer
    err := r.db.GetContext(ctx, &customer, query, email)
    return &customer, err
}

// ❌ BAD: String concatenation (vulnerable to SQL injection)
func (r *CustomerRepository) GetByEmail(ctx context.Context, email string) (*Customer, error) {
    query := fmt.Sprintf("SELECT * FROM customers WHERE email = '%s'", email)
    // DON'T DO THIS!
}
```

### 4.4 Rate Limiting

```go
package middleware

import (
    "fmt"
    "time"

    "github.com/gofiber/fiber/v2"
    "github.com/redis/go-redis/v9"
)

func RateLimiter(redis *redis.Client, limit int, window time.Duration) fiber.Handler {
    return func(c *fiber.Ctx) error {
        // Get customer from context
        customer := c.Locals("customer").(*Customer)

        key := fmt.Sprintf("ratelimit:%s:%d", customer.ID, time.Now().Unix()/int64(window.Seconds()))

        // Increment counter
        count, err := redis.Incr(c.Context(), key).Result()
        if err != nil {
            return err
        }

        // Set expiration on first request
        if count == 1 {
            redis.Expire(c.Context(), key, window)
        }

        // Check limit
        if count > int64(limit) {
            return c.Status(429).JSON(fiber.Map{
                "error":       "Rate limit exceeded",
                "retry_after": window.Seconds(),
            })
        }

        // Set rate limit headers
        c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
        c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", limit-int(count)))

        return c.Next()
    }
}
```

---

## 5. Common Patterns

### 5.1 Repository Pattern

```go
// domain/customer/repository.go
package customer

import "context"

type ICustomerRepository interface {
    Create(ctx context.Context, customer Customer) error
    GetByID(ctx context.Context, id string) (*Customer, error)
    GetByEmail(ctx context.Context, email string) (*Customer, error)
    Update(ctx context.Context, customer Customer) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, filter ListFilter) ([]Customer, int, error)
}

// infrastructure/database/customer_repository.go
package database

import (
    "context"

    "github.com/jmoiron/sqlx"
    "your-project/internal/domain/customer"
)

type CustomerRepository struct {
    db *sqlx.DB
}

func NewCustomerRepository(db *sqlx.DB) customer.ICustomerRepository {
    return &CustomerRepository{db: db}
}

func (r *CustomerRepository) Create(ctx context.Context, cust customer.Customer) error {
    query := `
        INSERT INTO customers (id, email, full_name, plan, created_at)
        VALUES ($1, $2, $3, $4, $5)
    `
    _, err := r.db.ExecContext(ctx, query,
        cust.ID,
        cust.Email,
        cust.FullName,
        cust.Plan,
        cust.CreatedAt,
    )
    return err
}
```

### 5.2 Service Pattern

```go
// domain/customer/service.go
package customer

import (
    "context"

    "github.com/google/uuid"
)

type CustomerService struct {
    repo   ICustomerRepository
    logger *logrus.Logger
}

func NewCustomerService(repo ICustomerRepository, logger *logrus.Logger) *CustomerService {
    return &CustomerService{
        repo:   repo,
        logger: logger,
    }
}

func (s *CustomerService) CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*Customer, error) {
    // Validate
    if err := req.Validate(); err != nil {
        return nil, err
    }

    // Check if email exists
    existing, _ := s.repo.GetByEmail(ctx, req.Email)
    if existing != nil {
        return nil, ErrEmailAlreadyExists
    }

    // Create customer
    customer := Customer{
        ID:        uuid.New(),
        Email:     req.Email,
        FullName:  req.FullName,
        Plan:      req.Plan,
        Status:    "active",
        CreatedAt: time.Now(),
    }

    if err := s.repo.Create(ctx, customer); err != nil {
        s.logger.WithError(err).Error("Failed to create customer")
        return nil, err
    }

    s.logger.WithField("customer_id", customer.ID).Info("Customer created")
    return &customer, nil
}
```

### 5.3 Handler Pattern (API Layer)

```go
// api/admin/customer_handler.go
package admin

import (
    "github.com/gofiber/fiber/v2"
    "your-project/internal/domain/customer"
)

type CustomerHandler struct {
    service *customer.CustomerService
}

func NewCustomerHandler(service *customer.CustomerService) *CustomerHandler {
    return &CustomerHandler{service: service}
}

func (h *CustomerHandler) Create(c *fiber.Ctx) error {
    var req customer.CreateCustomerRequest

    // Parse request body
    if err := c.BodyParser(&req); err != nil {
        return c.Status(400).JSON(fiber.Map{
            "error": "Invalid request body",
        })
    }

    // Call service
    cust, err := h.service.CreateCustomer(c.Context(), req)
    if err != nil {
        return c.Status(500).JSON(fiber.Map{
            "error": err.Error(),
        })
    }

    return c.Status(201).JSON(fiber.Map{
        "success": true,
        "data":    cust,
    })
}

func (h *CustomerHandler) RegisterRoutes(router fiber.Router) {
    router.Post("/customers", h.Create)
    router.Get("/customers/:id", h.GetByID)
    router.Put("/customers/:id", h.Update)
    router.Delete("/customers/:id", h.Delete)
    router.Get("/customers", h.List)
}
```

---

## 6. Testing Requirements

### 6.1 Unit Tests (Go)

```go
// domain/customer/service_test.go
package customer_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "your-project/internal/domain/customer"
)

// Mock repository
type MockCustomerRepository struct {
    mock.Mock
}

func (m *MockCustomerRepository) Create(ctx context.Context, cust customer.Customer) error {
    args := m.Called(ctx, cust)
    return args.Error(0)
}

func (m *MockCustomerRepository) GetByEmail(ctx context.Context, email string) (*customer.Customer, error) {
    args := m.Called(ctx, email)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*customer.Customer), args.Error(1)
}

func TestCustomerService_CreateCustomer(t *testing.T) {
    tests := []struct {
        name    string
        request customer.CreateCustomerRequest
        setup   func(*MockCustomerRepository)
        wantErr bool
    }{
        {
            name: "success",
            request: customer.CreateCustomerRequest{
                Email:    "test@example.com",
                FullName: "Test User",
                Plan:     "basic",
            },
            setup: func(repo *MockCustomerRepository) {
                // Email doesn't exist
                repo.On("GetByEmail", mock.Anything, "test@example.com").
                    Return(nil, customer.ErrNotFound)

                // Create succeeds
                repo.On("Create", mock.Anything, mock.Anything).
                    Return(nil)
            },
            wantErr: false,
        },
        {
            name: "email already exists",
            request: customer.CreateCustomerRequest{
                Email:    "existing@example.com",
                FullName: "Test User",
                Plan:     "basic",
            },
            setup: func(repo *MockCustomerRepository) {
                // Email exists
                repo.On("GetByEmail", mock.Anything, "existing@example.com").
                    Return(&customer.Customer{Email: "existing@example.com"}, nil)
            },
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            repo := new(MockCustomerRepository)
            tt.setup(repo)

            service := customer.NewCustomerService(repo, logrus.New())

            _, err := service.CreateCustomer(context.Background(), tt.request)

            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }

            repo.AssertExpectations(t)
        })
    }
}
```

### 6.2 Integration Tests (Go)

```go
// api/admin/customer_handler_integration_test.go
//go:build integration
// +build integration

package admin_test

import (
    "bytes"
    "encoding/json"
    "net/http/httptest"
    "testing"

    "github.com/gofiber/fiber/v2"
    "github.com/stretchr/testify/assert"
)

func TestCustomerHandler_Create_Integration(t *testing.T) {
    // Setup test database
    db := setupTestDB(t)
    defer cleanupTestDB(t, db)

    // Setup app
    app := fiber.New()
    handler := setupCustomerHandler(db)
    handler.RegisterRoutes(app.Group("/api/admin"))

    // Test data
    reqBody := map[string]interface{}{
        "email":     "test@example.com",
        "full_name": "Test User",
        "plan":      "basic",
    }
    body, _ := json.Marshal(reqBody)

    // Make request
    req := httptest.NewRequest("POST", "/api/admin/customers", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    resp, err := app.Test(req)
    assert.NoError(t, err)
    assert.Equal(t, 201, resp.StatusCode)

    // Verify response
    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)
    assert.True(t, result["success"].(bool))
    assert.NotNil(t, result["data"])
}
```

### 6.3 Frontend Tests (TypeScript)

```typescript
// components/forms/AddServerForm.test.tsx
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AddServerForm } from './AddServerForm'
import { ServerService } from '@/lib/api/server'

jest.mock('@/lib/api/server')

describe('AddServerForm', () => {
  const queryClient = new QueryClient()

  it('should submit form successfully', async () => {
    const mockCreate = jest.fn().mockResolvedValue({ id: '123', name: 'Test Server' })
    ;(ServerService.create as jest.Mock) = mockCreate

    const onSuccess = jest.fn()

    render(
      <QueryClientProvider client={queryClient}>
        <AddServerForm onSuccess={onSuccess} />
      </QueryClientProvider>
    )

    // Fill form
    fireEvent.change(screen.getByLabelText(/name/i), {
      target: { value: 'Test Server' },
    })
    fireEvent.change(screen.getByLabelText(/ip address/i), {
      target: { value: '192.168.1.10' },
    })

    // Submit
    fireEvent.click(screen.getByRole('button', { name: /add server/i }))

    await waitFor(() => {
      expect(mockCreate).toHaveBeenCalled()
      expect(onSuccess).toHaveBeenCalled()
    })
  })
})
```

---

## 7. File Organization

### 7.1 Backend (Go) Organization

**Follow this structure strictly:**

```
backend/management/
├── cmd/
│   └── server/
│       └── main.go              # Entry point
├── internal/                    # Private application code
│   ├── api/                     # HTTP handlers (controllers)
│   │   ├── admin/
│   │   │   ├── customer_handler.go
│   │   │   └── server_handler.go
│   │   ├── proxy/
│   │   │   └── proxy_handler.go
│   │   └── middleware/
│   │       ├── auth.go
│   │       └── ratelimit.go
│   ├── domain/                  # Business logic (pure Go, no dependencies)
│   │   ├── customer/
│   │   │   ├── model.go        # Structs
│   │   │   ├── repository.go   # Interface
│   │   │   ├── service.go      # Business logic
│   │   │   └── errors.go       # Domain errors
│   │   └── server/
│   ├── infrastructure/          # External dependencies
│   │   ├── database/
│   │   │   ├── postgres.go
│   │   │   └── customer_repository.go
│   │   └── cache/
│   │       └── redis.go
│   └── pkg/                     # Internal utilities
│       ├── config/
│       ├── logger/
│       └── validator/
├── migrations/                  # Database migrations
└── go.mod
```

**Rules:**
- `internal/` code cannot be imported by other projects
- `domain/` should have NO external dependencies (only stdlib)
- `infrastructure/` implements interfaces defined in `domain/`
- `api/` only calls `domain/` services, never repositories directly

### 7.2 Frontend (Next.js) Organization

```
frontend/admin-panel/
├── app/                         # Next.js App Router
│   ├── (auth)/                  # Route group (layout)
│   │   └── login/page.tsx
│   ├── (dashboard)/
│   │   ├── layout.tsx          # Dashboard layout
│   │   ├── page.tsx            # Dashboard home
│   │   └── servers/
│   │       ├── page.tsx        # Server list
│   │       └── [id]/page.tsx   # Server detail
│   └── api/                     # API routes
│       └── auth/
├── components/
│   ├── ui/                      # shadcn/ui components
│   │   ├── button.tsx
│   │   └── card.tsx
│   ├── forms/                   # Form components
│   │   └── AddServerForm.tsx
│   └── tables/                  # Table components
│       └── ServerTable.tsx
├── lib/
│   ├── api/                     # API clients
│   │   ├── client.ts           # Axios instance
│   │   ├── server.ts           # Server API
│   │   └── customer.ts         # Customer API
│   ├── hooks/                   # Custom hooks
│   │   └── useServer.ts
│   └── utils.ts                 # Utilities
├── types/
│   ├── server.ts
│   └── customer.ts
└── public/
    └── assets/
```

---

## 8. API Design

### 8.1 RESTful Conventions

**Follow these patterns:**

```
# Resource-based URLs
GET    /api/admin/servers          # List servers
POST   /api/admin/servers          # Create server
GET    /api/admin/servers/:id      # Get server
PUT    /api/admin/servers/:id      # Update server
DELETE /api/admin/servers/:id      # Delete server

# Nested resources
GET    /api/admin/servers/:id/metrics     # Server metrics
POST   /api/admin/servers/:id/rebalance   # Server action

# Filters via query params
GET    /api/admin/customers?status=active&plan=pro&page=1&limit=20
```

### 8.2 Response Format

**Standardized JSON response:**

```typescript
// Success response
{
  "success": true,
  "data": {
    "id": "uuid",
    "name": "Server 1"
  },
  "meta": {           // Optional: for pagination
    "total": 100,
    "page": 1,
    "per_page": 20
  }
}

// Error response
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid input",
    "details": {       // Optional: field-specific errors
      "email": "Invalid email format",
      "password": "Password too short"
    }
  }
}
```

### 8.3 HTTP Status Codes

Use appropriate status codes:

```
200 OK                  - Success (GET, PUT)
201 Created             - Success (POST)
204 No Content          - Success (DELETE)
400 Bad Request         - Validation error
401 Unauthorized        - Authentication required
403 Forbidden           - Insufficient permissions
404 Not Found           - Resource not found
409 Conflict            - Resource already exists
422 Unprocessable Entity - Semantic error
429 Too Many Requests   - Rate limit exceeded
500 Internal Server Error - Server error
503 Service Unavailable  - Server overloaded
```

---

## 9. Database Operations

### 9.1 Migrations

**Use golang-migrate:**

```bash
# Install
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Create migration
migrate create -ext sql -dir migrations -seq add_customers_table

# Run migrations
migrate -path migrations -database "postgresql://user:pass@localhost:5432/dbname?sslmode=disable" up

# Rollback
migrate -path migrations -database "..." down 1
```

**Migration file naming:**
```
migrations/
├── 000001_initial_schema.up.sql
├── 000001_initial_schema.down.sql
├── 000002_add_api_keys.up.sql
└── 000002_add_api_keys.down.sql
```

**Migration template:**
```sql
-- 000001_initial_schema.up.sql
BEGIN;

CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_customers_email ON customers(email);

COMMIT;
```

```sql
-- 000001_initial_schema.down.sql
BEGIN;

DROP TABLE IF EXISTS customers;

COMMIT;
```

### 9.2 Query Optimization

**Use indexes:**
```sql
-- Index for foreign keys
CREATE INDEX idx_customers_server_id ON customers(assigned_server_id);

-- Composite index for common queries
CREATE INDEX idx_customers_status_plan ON customers(status, plan)
  WHERE deleted_at IS NULL;

-- Partial index for active records only
CREATE INDEX idx_active_customers ON customers(id)
  WHERE status = 'active' AND deleted_at IS NULL;
```

**Avoid N+1 queries:**
```go
// ❌ BAD: N+1 query
func (r *ServerRepository) ListWithCustomers(ctx context.Context) ([]Server, error) {
    servers, _ := r.List(ctx)

    for i := range servers {
        // This runs a query for EACH server (N+1 problem)
        customers, _ := r.customerRepo.GetByServerID(ctx, servers[i].ID)
        servers[i].Customers = customers
    }

    return servers, nil
}

// ✅ GOOD: Single join query
func (r *ServerRepository) ListWithCustomers(ctx context.Context) ([]Server, error) {
    query := `
        SELECT
            s.id, s.name, s.status,
            json_agg(json_build_object(
                'id', c.id,
                'email', c.email
            )) FILTER (WHERE c.id IS NOT NULL) as customers
        FROM servers s
        LEFT JOIN customers c ON c.assigned_server_id = s.id
        WHERE s.deleted_at IS NULL
        GROUP BY s.id
    `

    var servers []Server
    err := r.db.SelectContext(ctx, &servers, query)
    return servers, err
}
```

---

## 10. Error Handling

### 10.1 Error Types

**Define domain-specific errors:**

```go
package customer

import "errors"

var (
    ErrNotFound           = errors.New("customer not found")
    ErrEmailAlreadyExists = errors.New("email already exists")
    ErrInvalidPlan        = errors.New("invalid plan")
    ErrServerNotAssigned  = errors.New("server not assigned")
)
```

### 10.2 Error Wrapping

```go
import "github.com/pkg/errors"

func (s *CustomerService) CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*Customer, error) {
    // Validate
    if err := req.Validate(); err != nil {
        return nil, errors.Wrap(err, "validation failed")
    }

    // Check email
    existing, err := s.repo.GetByEmail(ctx, req.Email)
    if err != nil && !errors.Is(err, ErrNotFound) {
        return nil, errors.Wrap(err, "failed to check existing email")
    }
    if existing != nil {
        return nil, ErrEmailAlreadyExists
    }

    // Create
    customer := req.ToCustomer()
    if err := s.repo.Create(ctx, customer); err != nil {
        return nil, errors.Wrap(err, "failed to create customer")
    }

    return &customer, nil
}
```

### 10.3 Error Logging

```go
func (h *CustomerHandler) Create(c *fiber.Ctx) error {
    var req customer.CreateCustomerRequest
    if err := c.BodyParser(&req); err != nil {
        h.logger.WithError(err).Warn("Invalid request body")
        return c.Status(400).JSON(fiber.Map{
            "error": "Invalid request body",
        })
    }

    cust, err := h.service.CreateCustomer(c.Context(), req)
    if err != nil {
        // Log with context
        h.logger.WithFields(logrus.Fields{
            "error":  err.Error(),
            "email":  req.Email,
            "action": "create_customer",
        }).Error("Failed to create customer")

        // Return appropriate status code
        if errors.Is(err, customer.ErrEmailAlreadyExists) {
            return c.Status(409).JSON(fiber.Map{
                "error": err.Error(),
            })
        }

        return c.Status(500).JSON(fiber.Map{
            "error": "Internal server error",
        })
    }

    return c.Status(201).JSON(fiber.Map{
        "success": true,
        "data":    cust,
    })
}
```

---

## 11. Deployment

### 11.1 Docker Build

**Dockerfile best practices:**

```dockerfile
# backend/management/Dockerfile

# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server ./cmd/server

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/server .

# Expose port
EXPOSE 8080

# Run
CMD ["./server"]
```

### 11.2 Environment Variables

**Use .env for local, secrets for production:**

```bash
# .env.example
APP_ENV=development
APP_PORT=8080

DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=secret
DB_NAME=whatsapp_management

REDIS_URL=redis://localhost:6379

JWT_SECRET=your-secret-key
API_KEY_PREFIX=ak_live_
```

**Load in Go:**
```go
package config

import (
    "github.com/spf13/viper"
)

func Load() error {
    viper.SetConfigFile(".env")
    viper.AutomaticEnv()

    if err := viper.ReadInConfig(); err != nil {
        return err
    }

    return nil
}

func Get(key string) string {
    return viper.GetString(key)
}
```

---

## 12. Troubleshooting

### 12.1 Common Issues

**Issue: Database connection refused**
```bash
# Check if PostgreSQL is running
sudo systemctl status postgresql

# Check connection
psql -h localhost -U postgres -d whatsapp_management

# Fix: Update connection string in .env
DB_HOST=localhost  # Not 127.0.0.1 if using Docker
```

**Issue: Rate limit not working**
```bash
# Check Redis connection
redis-cli ping

# Check keys
redis-cli KEYS "ratelimit:*"

# Fix: Ensure Redis is accessible from app
REDIS_URL=redis://redis:6379  # If using Docker Compose
```

**Issue: API key validation fails**
```go
// Debug: Log API key hash comparison
func ValidateAPIKey(plainKey, hash string) bool {
    log.Printf("Validating key: %s (prefix: %s)", plainKey[:10], plainKey[:8])
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plainKey))
    if err != nil {
        log.Printf("Validation failed: %v", err)
    }
    return err == nil
}
```

### 12.2 Performance Debugging

**Enable query logging:**
```go
// PostgreSQL
import "github.com/jmoiron/sqlx"

db.MustExec("SET log_statement = 'all'")
```

**Profile Go application:**
```go
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()

// Access profiling: http://localhost:6060/debug/pprof/
```

---

## 13. Quick Reference

### 13.1 Common Commands

```bash
# Backend (Go)
cd backend/management
go run cmd/server/main.go
go test ./...
go test -v -run TestCustomerService_CreateCustomer

# Frontend (Next.js)
cd frontend/admin-panel
npm run dev
npm run build
npm run test

# Database
migrate -path migrations -database "..." up
psql -h localhost -U postgres -d whatsapp_management

# Docker
docker-compose up -d
docker-compose logs -f management
docker exec -it management_api sh

# Git
git checkout -b feature/add-server-management
git commit -m "feat: add server management API"
git push origin feature/add-server-management
```

### 13.2 Useful Resources

- **Go:** https://go.dev/doc/
- **Fiber:** https://docs.gofiber.io/
- **Next.js:** https://nextjs.org/docs
- **PostgreSQL:** https://www.postgresql.org/docs/
- **Docker:** https://docs.docker.com/

---

## 14. Checklist for New Features

**Before submitting PR:**

- [ ] Code follows style guide
- [ ] All functions have comments (exported ones)
- [ ] Input validation added
- [ ] Error handling implemented
- [ ] Tests written (unit + integration)
- [ ] Database migrations created (if schema changes)
- [ ] API documentation updated
- [ ] Logging added
- [ ] Security reviewed (no secrets, SQL injection, XSS)
- [ ] Performance considered (indexes, N+1 queries)
- [ ] Local testing passed
- [ ] CI/CD pipeline passes

---

**Last Updated:** 2025-11-19
**Maintained By:** Development Team
**Questions?** Create an issue or contact the team lead.
