# Migration Guide
# From Current Monolith to Multi-Server Architecture

**Version:** 2.0.0
**Date:** 2025-11-19
**Estimated Time:** 12 weeks

---

## 📋 Overview

This guide provides step-by-step instructions for migrating from the current monolithic WhatsApp API to a scalable multi-server SaaS platform.

---

## 🎯 Migration Goals

- ✅ Zero downtime migration
- ✅ Preserve all existing functionality
- ✅ Clean separation of concerns (backend/frontend)
- ✅ Support multi-server architecture
- ✅ Enhanced security
- ✅ Backward compatibility during transition

---

## 📊 Current vs. New Structure

### Current Structure
```
go-whatsapp/
├── src/
│   ├── cmd/              # CLI commands
│   ├── domains/          # Business logic
│   ├── infrastructure/   # WhatsApp integration
│   ├── ui/rest/          # REST API
│   ├── ui/mcp/           # MCP server
│   ├── usecase/          # Use cases
│   ├── validations/      # Validation
│   ├── pkg/              # Utilities
│   ├── views/            # HTML templates
│   └── config/           # Configuration
├── go.mod
└── README.md
```

### New Structure
```
whatsapp-saas-platform/
├── backend/
│   ├── management/       # NEW: Management system
│   └── worker/           # REFACTORED: From src/
├── frontend/
│   ├── admin-panel/      # NEW: Admin dashboard
│   └── customer-dashboard/  # NEW: Customer dashboard
├── infrastructure/       # NEW: DevOps configs
├── docs/                 # Documentation
├── scripts/              # Utility scripts
├── PRD.md
├── AGENT.md
└── MIGRATION_GUIDE.md    # This file
```

---

## 🗺️ Phase-by-Phase Migration Plan

### **Phase 0: Preparation (Week 1-2)**

#### Week 1: Environment Setup

**Day 1-2: Create New Repository Structure**

```bash
# 1. Create backup of current codebase
cd /home/user/go-whatsapp
git checkout -b backup/pre-migration-$(date +%Y%m%d)
git push origin backup/pre-migration-$(date +%Y%m%d)

# 2. Create new branch for migration
git checkout -b feature/v2-architecture

# 3. Create new folder structure
mkdir -p backend/management/cmd/server
mkdir -p backend/management/internal/{api,domain,infrastructure,pkg}
mkdir -p backend/management/migrations

mkdir -p backend/worker/cmd/worker
mkdir -p backend/worker/internal/{api,domain,infrastructure,usecase,pkg}

mkdir -p frontend/admin-panel
mkdir -p frontend/customer-dashboard

mkdir -p infrastructure/{docker,kubernetes,ansible,terraform}
mkdir -p docs/{architecture,api,deployment,guides}
mkdir -p scripts/{migration,deployment,backup}
```

**Day 3-4: Setup Database**

```bash
# 1. Install PostgreSQL (if not already installed)
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install postgresql postgresql-contrib

# macOS
brew install postgresql@16

# 2. Create database
sudo -u postgres psql
```

```sql
CREATE DATABASE whatsapp_management;
CREATE USER whatsapp_admin WITH ENCRYPTED PASSWORD 'your_secure_password';
GRANT ALL PRIVILEGES ON DATABASE whatsapp_management TO whatsapp_admin;
\q
```

```bash
# 3. Install migration tool
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# 4. Copy database schema from PRD.md to migration file
cat > backend/management/migrations/000001_initial_schema.up.sql << 'EOF'
-- Copy the SQL schema from PRD.md Section 8.1
EOF

# 5. Run migrations
cd backend/management
migrate -path migrations \
  -database "postgresql://whatsapp_admin:your_secure_password@localhost:5432/whatsapp_management?sslmode=disable" \
  up
```

**Day 5: Setup Redis**

```bash
# Install Redis
# Ubuntu/Debian
sudo apt-get install redis-server

# macOS
brew install redis

# Start Redis
redis-server

# Test connection
redis-cli ping
# Should return: PONG
```

#### Week 2: Backend Foundation

**Day 1-3: Create Management API Skeleton**

```bash
cd backend/management

# Initialize Go module
go mod init github.com/yourusername/whatsapp-saas/backend/management

# Install dependencies
go get github.com/gofiber/fiber/v2
go get github.com/jmoiron/sqlx
go get github.com/lib/pq
go get github.com/redis/go-redis/v9
go get github.com/sirupsen/logrus
go get github.com/spf13/viper
go get github.com/google/uuid
go get golang.org/x/crypto/bcrypt
go get github.com/golang-jwt/jwt/v5
```

**Create main.go:**
```bash
cat > cmd/server/main.go << 'EOF'
package main

import (
    "log"
    "github.com/gofiber/fiber/v2"
)

func main() {
    app := fiber.New(fiber.Config{
        AppName: "WhatsApp Management API v2.0",
    })

    app.Get("/health", func(c *fiber.Ctx) error {
        return c.JSON(fiber.Map{
            "status": "healthy",
            "version": "2.0.0",
        })
    })

    log.Fatal(app.Listen(":8080"))
}
EOF

# Test
go run cmd/server/main.go
# Visit: http://localhost:8080/health
```

**Day 4-5: Refactor Worker Backend**

```bash
# Copy existing src/ to backend/worker/
cp -r src/* backend/worker/

# Create new structure
cd backend/worker
mkdir -p internal/api
mkdir -p internal/domain
mkdir -p internal/infrastructure
mkdir -p internal/usecase
mkdir -p internal/pkg

# Move files to new structure
mv domains/* internal/domain/
mv infrastructure/* internal/infrastructure/
mv usecase/* internal/usecase/
mv validations/* internal/pkg/validation/
mv pkg/* internal/pkg/
mv ui/rest/* internal/api/
```

**Update imports:**
```bash
# Find and replace old import paths
find backend/worker -name "*.go" -type f -exec sed -i 's|"go-whatsapp/src/domains|"github.com/yourusername/whatsapp-saas/backend/worker/internal/domain|g' {} \;
find backend/worker -name "*.go" -type f -exec sed -i 's|"go-whatsapp/src/infrastructure|"github.com/yourusername/whatsapp-saas/backend/worker/internal/infrastructure|g' {} \;
find backend/worker -name "*.go" -type f -exec sed -i 's|"go-whatsapp/src/usecase|"github.com/yourusername/whatsapp-saas/backend/worker/internal/usecase|g' {} \;
```

---

### **Phase 1: Core Backend (Week 3-6)**

#### Week 3: Management API - Server Registry

**Implement Server Domain:**

```bash
cd backend/management/internal/domain/server

# Create model.go
cat > model.go << 'EOF'
package server

import (
    "time"
    "github.com/google/uuid"
)

type Server struct {
    ID            uuid.UUID  `json:"id" db:"id"`
    Name          string     `json:"name" db:"name"`
    IPAddress     string     `json:"ip_address" db:"ip_address"`
    APIURL        string     `json:"api_url" db:"api_url"`
    Status        string     `json:"status" db:"status"`
    MaxCapacity   int        `json:"max_capacity" db:"max_capacity"`
    CurrentLoad   int        `json:"current_load" db:"current_load"`
    HealthStatus  string     `json:"health_status" db:"health_status"`
    LastHeartbeat *time.Time `json:"last_heartbeat" db:"last_heartbeat"`
    CreatedAt     time.Time  `json:"created_at" db:"created_at"`
    UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}
EOF

# Create repository.go (interface)
cat > repository.go << 'EOF'
package server

import (
    "context"
    "github.com/google/uuid"
)

type IServerRepository interface {
    Create(ctx context.Context, server Server) error
    GetByID(ctx context.Context, id uuid.UUID) (*Server, error)
    List(ctx context.Context) ([]Server, error)
    Update(ctx context.Context, server Server) error
    Delete(ctx context.Context, id uuid.UUID) error
}
EOF

# Create service.go (business logic)
# ... (implement according to AGENT.md patterns)
```

**Implement PostgreSQL Repository:**

```bash
cd backend/management/internal/infrastructure/database

# Create server_repository.go
# ... (implement according to AGENT.md patterns)
```

**Create API Handlers:**

```bash
cd backend/management/internal/api/admin

# Create server_handler.go
# ... (implement according to AGENT.md patterns)
```

#### Week 4: Management API - Customer Management

Similar to Week 3, implement:
- Customer domain
- Customer repository
- Customer service
- Customer API handlers
- API key generation & validation

#### Week 5: Management API - Authentication & Security

Implement:
- JWT authentication for admin
- API key middleware for customers
- Rate limiting with Redis
- Audit logging
- Security headers

#### Week 6: Worker Backend Refactoring

Tasks:
- Multi-instance manager (handle multiple WhatsApp connections)
- Customer isolation
- Registration with management system
- Health reporting endpoint
- Graceful shutdown

**Key File: instance_manager.go**

```go
// backend/worker/internal/infrastructure/whatsapp/instance_manager.go
package whatsapp

import (
    "context"
    "sync"
    "go.mau.fi/whatsmeow"
)

type InstanceManager struct {
    instances    sync.Map  // customerID -> *WhatsAppInstance
    maxInstances int
    mu           sync.RWMutex
}

type WhatsAppInstance struct {
    CustomerID   string
    Client       *whatsmeow.Client
    Status       string
    LastActivity time.Time
}

func NewInstanceManager(maxInstances int) *InstanceManager {
    return &InstanceManager{
        maxInstances: maxInstances,
    }
}

func (m *InstanceManager) CreateInstance(ctx context.Context, customerID string) error {
    // Implementation
}

func (m *InstanceManager) RemoveInstance(ctx context.Context, customerID string) error {
    // Implementation
}

func (m *InstanceManager) GetInstance(customerID string) (*WhatsAppInstance, bool) {
    val, ok := m.instances.Load(customerID)
    if !ok {
        return nil, false
    }
    return val.(*WhatsAppInstance), true
}
```

---

### **Phase 2: Frontend Development (Week 7-8)**

#### Week 7: Admin Panel

**Setup Next.js:**

```bash
cd frontend/admin-panel

# Create Next.js app
npx create-next-app@latest . \
  --typescript \
  --tailwind \
  --app \
  --src-dir \
  --import-alias "@/*"

# Install dependencies
npm install @tanstack/react-query axios zustand
npm install @radix-ui/react-dialog @radix-ui/react-dropdown-menu
npm install lucide-react date-fns
npm install next-auth

# Install shadcn/ui
npx shadcn-ui@latest init
npx shadcn-ui@latest add button card input table
```

**Create pages according to structure in PRD.md Section 7.1**

**Example: Server List Page**

```typescript
// app/(dashboard)/servers/page.tsx
'use client'

import { useQuery } from '@tanstack/react-query'
import { ServerService } from '@/lib/api/server'
import { ServerTable } from '@/components/tables/ServerTable'
import { Button } from '@/components/ui/button'
import Link from 'next/link'

export default function ServersPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['servers'],
    queryFn: () => ServerService.list(),
  })

  return (
    <div className="p-6">
      <div className="flex justify-between items-center mb-6">
        <h1 className="text-3xl font-bold">Servers</h1>
        <Link href="/servers/add">
          <Button>Add Server</Button>
        </Link>
      </div>

      {isLoading ? (
        <div>Loading...</div>
      ) : (
        <ServerTable data={data?.servers || []} />
      )}
    </div>
  )
}
```

#### Week 8: Customer Dashboard

Similar to Admin Panel, but customer-facing features:
- WhatsApp connection (QR code)
- Send messages
- Chat list
- API key management
- Usage statistics

---

### **Phase 3: Integration & Testing (Week 9-10)**

#### Week 9: Integration

**1. Connect Frontend to Backend:**

```typescript
// frontend/admin-panel/lib/api/client.ts
import axios from 'axios'

const apiClient = axios.create({
  baseURL: process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080',
  timeout: 30000,
})

apiClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('auth_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

export { apiClient }
```

**2. Implement Proxy in Management API:**

```go
// backend/management/internal/api/proxy/proxy_handler.go
package proxy

import (
    "github.com/gofiber/fiber/v2"
    "io"
    "net/http"
)

type ProxyHandler struct {
    serverRepo server.IServerRepository
}

func (h *ProxyHandler) ProxyRequest(c *fiber.Ctx) error {
    // Get customer from context (set by auth middleware)
    customer := c.Locals("customer").(*Customer)

    // Get assigned server
    server, err := h.serverRepo.GetByID(c.Context(), customer.AssignedServerID)
    if err != nil {
        return c.Status(500).JSON(fiber.Map{"error": "Server not found"})
    }

    // Build target URL
    targetURL := server.APIURL + c.Path()

    // Create proxy request
    req, _ := http.NewRequest(c.Method(), targetURL, c.Request().BodyStream())

    // Copy headers
    for key, values := range c.GetReqHeaders() {
        for _, value := range values {
            req.Header.Add(key, value)
        }
    }

    // Send request
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return c.Status(500).JSON(fiber.Map{"error": "Proxy failed"})
    }
    defer resp.Body.Close()

    // Copy response
    c.Status(resp.StatusCode)
    for key, values := range resp.Header {
        for _, value := range values {
            c.Set(key, value)
        }
    }

    io.Copy(c.Response().BodyWriter(), resp.Body)
    return nil
}
```

**3. Docker Compose for Local Development:**

```yaml
# docker-compose.yml
version: '3.8'

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: whatsapp_management
      POSTGRES_USER: whatsapp_admin
      POSTGRES_PASSWORD: password123
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  management-api:
    build:
      context: ./backend/management
      dockerfile: Dockerfile
    ports:
      - "8080:8080"
    environment:
      DB_HOST: postgres
      DB_PORT: 5432
      DB_USER: whatsapp_admin
      DB_PASSWORD: password123
      DB_NAME: whatsapp_management
      REDIS_URL: redis://redis:6379
    depends_on:
      - postgres
      - redis

  worker:
    build:
      context: ./backend/worker
      dockerfile: Dockerfile
    ports:
      - "3000:3000"
    environment:
      MANAGEMENT_URL: http://management-api:8080
      DB_URI: sqlite:///app/storages/whatsapp.db
    volumes:
      - worker_data:/app/storages

  admin-panel:
    build:
      context: ./frontend/admin-panel
      dockerfile: Dockerfile
    ports:
      - "3001:3000"
    environment:
      NEXT_PUBLIC_API_URL: http://localhost:8080

  customer-dashboard:
    build:
      context: ./frontend/customer-dashboard
      dockerfile: Dockerfile
    ports:
      - "3002:3000"
    environment:
      NEXT_PUBLIC_API_URL: http://localhost:8080

volumes:
  postgres_data:
  worker_data:
```

**Run everything:**
```bash
docker-compose up -d
```

#### Week 10: Testing

**1. Unit Tests:**
```bash
# Backend
cd backend/management
go test ./...

cd ../worker
go test ./...

# Frontend
cd ../../frontend/admin-panel
npm run test

cd ../customer-dashboard
npm run test
```

**2. Integration Tests:**
```bash
# See AGENT.md Section 6.2 for examples
```

**3. Load Testing:**
```bash
# Install k6
brew install k6  # macOS
# or
sudo apt-get install k6  # Ubuntu

# Create load test script
cat > scripts/load-test.js << 'EOF'
import http from 'k6/http'
import { check } from 'k6'

export const options = {
  vus: 100,  // 100 virtual users
  duration: '5m',
}

export default function () {
  const res = http.post('http://localhost:8080/api/v1/send/message', JSON.stringify({
    phone: '+6281234567890',
    message: 'Load test message'
  }), {
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': 'ak_live_your_test_key',
    },
  })

  check(res, {
    'status is 200': (r) => r.status === 200,
    'response time < 500ms': (r) => r.timings.duration < 500,
  })
}
EOF

# Run load test
k6 run scripts/load-test.js
```

---

### **Phase 4: Deployment (Week 11-12)**

#### Week 11: Staging Deployment

**1. Setup Staging Environment:**

```bash
# Provision VPS (example: DigitalOcean)
# - Management: 2 CPU, 4GB RAM
# - Worker: 4 CPU, 8GB RAM
# - Database: 2 CPU, 4GB RAM

# Setup using Ansible
cd infrastructure/ansible

cat > inventory/staging.yml << 'EOF'
all:
  children:
    management:
      hosts:
        staging-management:
          ansible_host: 192.168.1.10
          ansible_user: root
    workers:
      hosts:
        staging-worker-1:
          ansible_host: 192.168.1.11
          ansible_user: root
    databases:
      hosts:
        staging-db:
          ansible_host: 192.168.1.12
          ansible_user: root
EOF

# Run playbook
ansible-playbook -i inventory/staging.yml deploy-all.yml
```

**2. Deploy Applications:**

```bash
# Build and push Docker images
docker build -t registry.example.com/management:staging backend/management
docker push registry.example.com/management:staging

docker build -t registry.example.com/worker:staging backend/worker
docker push registry.example.com/worker:staging

# SSH to servers and pull images
ssh root@192.168.1.10
docker pull registry.example.com/management:staging
docker run -d -p 8080:8080 \
  -e DB_HOST=192.168.1.12 \
  --name management \
  registry.example.com/management:staging
```

**3. Run Smoke Tests:**

```bash
# Test management API
curl https://staging-management.example.com/health

# Test worker
curl https://staging-worker.example.com/health

# Test frontend
curl https://staging-admin.example.com
```

#### Week 12: Production Deployment

**1. Gradual Migration Strategy:**

**Step 1: Deploy management system (doesn't affect existing service)**
```bash
# Deploy to production VPS
ansible-playbook -i inventory/production.yml deploy-management.yml

# Verify: https://management.yourapp.com
```

**Step 2: Register existing server as VPS-1**
```bash
# Using management API
curl -X POST https://management.yourapp.com/api/admin/servers \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "VPS-1-Legacy",
    "ip_address": "your.existing.server.ip",
    "max_capacity": 100,
    "cpu_cores": 4,
    "ram_gb": 8
  }'
```

**Step 3: Migrate 10% of customers (test group)**
```bash
# Script to migrate customers
cat > scripts/migration/migrate-customers.sh << 'EOF'
#!/bin/bash

# Get 10% of customers
CUSTOMERS=$(curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  https://management.yourapp.com/api/admin/customers?limit=10)

# For each customer, update to use new system
for customer in $CUSTOMERS; do
  customer_id=$(echo $customer | jq -r '.id')

  # Assign to server
  curl -X POST \
    https://management.yourapp.com/api/admin/customers/$customer_id/reassign \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -d '{"server_id": "vps-1-uuid"}'
done
EOF

chmod +x scripts/migration/migrate-customers.sh
./scripts/migration/migrate-customers.sh
```

**Step 4: Monitor for 24 hours**
- Check error rates
- Check response times
- Check customer complaints
- Check server metrics

**Step 5: Migrate 50% of customers**
- Repeat Step 3 with 50%
- Monitor for 48 hours

**Step 6: Migrate 100% of customers**
- Migrate remaining customers
- Deprecate old endpoints (with 30-day notice)

**2. Update DNS:**
```bash
# Point API subdomain to management system
api.yourapp.com -> management.yourapp.com

# Point dashboard to new frontend
dashboard.yourapp.com -> customer-dashboard.yourapp.com
admin.yourapp.com -> admin-panel.yourapp.com
```

**3. Monitoring Setup:**

```bash
# Install Prometheus
docker run -d -p 9090:9090 \
  -v ./infrastructure/monitoring/prometheus:/etc/prometheus \
  prom/prometheus

# Install Grafana
docker run -d -p 3000:3000 grafana/grafana

# Import dashboards from infrastructure/monitoring/grafana/
```

---

## 🔄 Rollback Plan

If critical issues occur during migration:

```bash
# 1. Switch DNS back to old system
# Update DNS records to point to original server

# 2. Restore database from backup
pg_restore -h localhost -U whatsapp_admin \
  -d whatsapp_management backup_before_migration.dump

# 3. Stop new services
docker-compose down

# 4. Notify customers
# Send email/SMS about temporary rollback

# 5. Post-mortem
# Analyze what went wrong
# Fix issues
# Plan retry
```

---

## ✅ Post-Migration Checklist

After successful migration:

- [ ] All customers migrated successfully
- [ ] Old endpoints deprecated with notices
- [ ] Documentation updated
- [ ] Team trained on new system
- [ ] Monitoring dashboards configured
- [ ] Backup strategy implemented
- [ ] Disaster recovery plan tested
- [ ] Performance benchmarks met
- [ ] Security audit passed
- [ ] Customer communication sent

---

## 📞 Support During Migration

**Communication Plan:**

1. **Week before migration:**
   - Email to all customers about upcoming changes
   - Blog post explaining new features
   - FAQ document

2. **During migration:**
   - Status page (status.yourapp.com)
   - Real-time updates via Twitter/Discord
   - Support team on standby

3. **After migration:**
   - Follow-up email
   - Survey for feedback
   - Offer credits for any downtime

---

## 🐛 Known Issues & Solutions

### Issue 1: WhatsApp Connection Lost During Migration

**Cause:** Instance restart causes WhatsApp disconnection

**Solution:**
- Implement graceful shutdown in worker
- Save session before restart
- Auto-reconnect on startup

```go
// backend/worker/internal/infrastructure/whatsapp/graceful_shutdown.go
func (m *InstanceManager) GracefulShutdown(ctx context.Context) error {
    // Disconnect all instances gracefully
    m.instances.Range(func(key, value interface{}) bool {
        instance := value.(*WhatsAppInstance)
        instance.Client.Disconnect()
        // Save session to database
        return true
    })
    return nil
}
```

### Issue 2: Database Connection Pool Exhausted

**Cause:** Too many concurrent connections

**Solution:**
- Increase connection pool size
- Implement connection pooling with PgBouncer

```bash
# Install PgBouncer
sudo apt-get install pgbouncer

# Configure
sudo nano /etc/pgbouncer/pgbouncer.ini
# max_client_conn = 1000
# default_pool_size = 25
```

### Issue 3: Rate Limiting Not Synced Across Servers

**Cause:** Each server has independent Redis cache

**Solution:**
- Use centralized Redis cluster
- Implement distributed rate limiting

```bash
# Setup Redis Cluster
docker run -d --name redis-cluster \
  -p 6379:6379 \
  redis:7-alpine redis-server --cluster-enabled yes
```

---

## 📚 Additional Resources

- **PRD.md** - Complete product requirements
- **AGENT.md** - Development guidelines
- **docs/architecture/** - Architecture diagrams
- **docs/api/** - API documentation
- **docs/deployment/** - Deployment guides

---

## 🎯 Success Metrics

Track these metrics post-migration:

| Metric | Target | Actual |
|--------|--------|--------|
| Migration completion rate | 100% | |
| Customer churn during migration | < 2% | |
| Downtime | < 1 hour | |
| Bug reports | < 10 critical | |
| Performance (response time) | < 200ms p95 | |
| Cost increase | < 20% | |

---

## 📝 Migration Log Template

Keep a log during migration:

```
Date: 2025-11-19
Phase: Phase 1 - Week 3
Status: In Progress

Completed:
- ✅ Server domain implementation
- ✅ PostgreSQL repository
- ✅ API handlers

In Progress:
- 🔄 Testing server CRUD operations

Blocked:
- None

Issues:
- Minor: Database migration took longer than expected (30min vs 10min)

Next Steps:
- Complete testing
- Move to customer management
```

---

**Questions?** Contact the migration team lead or create an issue in the repository.

**Last Updated:** 2025-11-19
