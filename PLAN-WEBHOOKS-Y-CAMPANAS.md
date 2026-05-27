# GoWA — Planes de Trabajo Adicionales

---

## PLAN 1: Webhooks por Device/Session

### Problema actual

`WHATSAPP_WEBHOOK` es global — todos los devices reenvían eventos a las mismas URLs. No hay forma de que Device A envíe eventos a `https://mi-backend/webhook-personal` y Device B a `https://mi-backend/webhook-empresa`.

### Arquitectura (misma que Chatwoot multi-device)

```
Nueva tabla: device_webhook_configs (SQLite, en chatstorage.db)
┌─────────────┬──────────────────────────────────────────────┐
│ device_id   │ 573166203787@s.whatsapp.net                  │
│ webhook_url │ https://mi-backend/webhook-personal           │
│ secret      │ mi-secret-hmac                                │
│ events      │ message,message.ack,message.reaction          │
│ enabled     │ true                                          │
│ headers     │ {"X-Custom": "value"} (JSON, opcional)        │
└─────────────┴──────────────────────────────────────────────┘
```

Un device puede tener N webhook URLs (relación 1:N).

### Fases

#### FASE W0 — Modelo y repositorio
- `domains/webhook/webhook.go` — DTO `DeviceWebhookConfig{ID, DeviceID, WebhookURL, Secret, Events, Enabled, Headers}`
- `domains/webhook/interfaces.go` — `IDeviceWebhookRepository{Migrate, Save, GetByDeviceID, GetAll, Delete}`
- `infrastructure/webhook/device_webhook_repository.go` — SQLite impl

#### FASE W1 — Registry de webhooks
- `infrastructure/webhook/registry.go` — `WebhookRegistry`
  - `GetWebhooksForDevice(deviceID) []DeviceWebhookConfig`
  - Cache en memoria + Refresh()
  - Fallback: si no hay config por device, usar `config.WhatsappWebhook` global

#### FASE W2 — Forward por device
- Modificar `infrastructure/whatsapp/webhook_forward.go` → `forwardToWebhooks()`
  - Extraer `device_id` del payload
  - Si hay webhooks específicos para ese device → usarlos
  - Si no → fallback a webhooks globales (backward compatible)
  - Filtrado de eventos por la lista `events` del config
  - HMAC con el `secret` individual

#### FASE W3 — API REST CRUD
- `ui/rest/webhook_config.go`
  - `GET /webhook/configs` — listar todos
  - `GET /webhook/configs/:device_id` — webhooks de un device
  - `POST /webhook/configs` — crear
  - `PUT /webhook/configs/:id` — actualizar
  - `DELETE /webhook/configs/:id` — eliminar
  - Cada escritura → registry.Refresh()

#### FASE W4 — UI
- `views/components/WebhookConfig.js` — tabla + formulario
- Sección "Webhooks" en index.html

### Estimado: 10-12 horas

---

## PLAN 2: Campañas de Mensajes Masivos con Anti-Ban

### Advertencia importante

WhatsApp prohíbe el envío masivo no solicitado vía APIs no oficiales (whatsmeow).
Estas features están diseñadas para envío legítimo (notificaciones a clientes,
recordatorios, marketing opt-in) con protecciones anti-bloqueo.
El usuario es responsable del uso conforme a los términos de WhatsApp.

### Arquitectura

```
Módulo: campaigns (nuevo dominio completo)

┌─────────────────────────────────────────────────────────┐
│                    CAMPAIGN ENGINE                        │
│                                                           │
│  Campaign ──→ Recipients ──→ Scheduler ──→ Sender Pool   │
│     │              │             │              │         │
│  Templates    CSV/Groups    Delay Engine    Number Rotation│
│  (Spintax)                 (Human curves)   (Health score)│
│     │              │             │              │         │
│     └──────────────┴─────────────┴──────────────┘         │
│                         │                                  │
│                    Metrics/Analytics                       │
│              (sent/delivered/failed/replied)               │
└─────────────────────────────────────────────────────────┘
```

### Tablas SQLite

```sql
-- Campañas
CREATE TABLE campaigns (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft',  -- draft|scheduled|running|paused|completed|cancelled
    template_body   TEXT NOT NULL,                  -- Spintax: "{Hola|Buenas} {nombre}"
    template_media  TEXT,                           -- URL/path de imagen/video (opcional)
    schedule_at     DATETIME,                       -- cuándo arranca
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Pool de números (devices asignados a la campaña)
CREATE TABLE campaign_senders (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    campaign_id     INTEGER NOT NULL REFERENCES campaigns(id),
    device_id       TEXT NOT NULL,                  -- JID del device
    max_daily       INTEGER NOT NULL DEFAULT 200,   -- máximo mensajes/día por número
    health_score    REAL NOT NULL DEFAULT 1.0,      -- 0.0-1.0, baja si hay errores
    sent_today      INTEGER NOT NULL DEFAULT 0,
    last_sent_at    DATETIME,
    enabled         BOOLEAN NOT NULL DEFAULT 1
);

-- Destinatarios
CREATE TABLE campaign_recipients (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    campaign_id     INTEGER NOT NULL REFERENCES campaigns(id),
    phone           TEXT NOT NULL,
    name            TEXT,                           -- para spintax {nombre}
    variables       TEXT,                           -- JSON: {"empresa": "Fututel", ...}
    status          TEXT NOT NULL DEFAULT 'pending', -- pending|sent|delivered|failed|replied|skipped
    sent_by_device  TEXT,                           -- qué device lo envió
    sent_at         DATETIME,
    delivered_at    DATETIME,
    read_at         DATETIME,
    replied_at      DATETIME,
    error_message   TEXT,
    UNIQUE(campaign_id, phone)
);

-- Templates reutilizables
CREATE TABLE campaign_templates (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    body            TEXT NOT NULL,                  -- Spintax
    media_url       TEXT,
    category        TEXT,                           -- marketing, notificacion, recordatorio
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Fases

#### FASE C0 — Dominio y repositorios
- `domains/campaign/campaign.go` — DTOs (Campaign, Sender, Recipient, Template)
- `domains/campaign/interfaces.go` — ICampaignRepository, ITemplateRepository
- `infrastructure/campaign/sqlite_repository.go` — CRUD + queries de estado
- Migración automática de tablas

#### FASE C1 — Motor de Spintax
- `infrastructure/campaign/spintax.go`
  - Parser de `{opción1|opción2|opción3}` con selección aleatoria
  - Reemplazo de variables: `{nombre}`, `{empresa}`, etc.
  - Cada mensaje generado es único (evita hash duplicado)
  - Test: 1000 mensajes con mismo template → 0 duplicados exactos

#### FASE C2 — Delay Engine (curvas humanas)
- `infrastructure/campaign/delay.go`
  - Distribución gaussiana entre min-max (ej: 15-45 segundos entre mensajes)
  - Pausa larga cada N mensajes (ej: 10-15 min cada 50 mensajes)
  - Horario humano: solo enviar entre 8am-8pm zona horaria del recipient
  - Jitter aleatorio para evitar patrones detectables
  - Factor de velocidad configurable por campaña

```go
type DelayConfig struct {
    MinDelay        time.Duration  // 15s
    MaxDelay        time.Duration  // 45s
    PauseDuration   time.Duration  // 10-15min
    PauseEveryN     int            // cada 50 mensajes
    ActiveHoursStart int           // 8 (8am)
    ActiveHoursEnd   int           // 20 (8pm)
    Timezone        string         // "America/Bogota"
}
```

#### FASE C3 — Sender Pool y rotación de números
- `infrastructure/campaign/sender_pool.go`
  - Selección round-robin entre devices habilitados
  - Respeta max_daily por device
  - Health score: baja en errores/timeouts, sube con entregas exitosas
  - Device con health < 0.3 → se desactiva automáticamente
  - Reset diario de sent_today (cron a medianoche)

```go
type SenderPool struct {
    senders   []CampaignSender
    mu        sync.RWMutex
    nextIndex int
}

func (p *SenderPool) NextAvailable() (*CampaignSender, error)
func (p *SenderPool) ReportResult(deviceID string, success bool)
func (p *SenderPool) ResetDaily()
```

#### FASE C4 — Presencia y lectura simulada
- `infrastructure/campaign/presence.go`
  - Antes de enviar: `SendPresence("composing")` al destino
  - Delay de typing: 2-5 segundos (proporcional al largo del mensaje)
  - Después de enviar: `SendPresence("paused")`
  - Auto mark-read de respuestas entrantes con delay (3-10 seg)
  - Simular "online" en horario activo, "offline" fuera de horario

#### FASE C5 — Campaign Runner (orquestador)
- `infrastructure/campaign/runner.go`
  - Goroutine principal que procesa la cola de recipients
  - Ciclo: pick recipient → pick sender → spintax → delay → presence → send → log result
  - Pausable/resumible (status running ↔ paused)
  - Context cancelable para shutdown limpio
  - Métricas en tiempo real (sent, delivered, failed, replied)

```go
type CampaignRunner struct {
    campaign    *Campaign
    pool        *SenderPool
    delay       *DelayEngine
    spintax     *SpintaxEngine
    presence    *PresenceSimulator
    repo        ICampaignRepository
    sendUsecase domainSend.ISendUsecase
    ctx         context.Context
    cancel      context.CancelFunc
}

func (r *CampaignRunner) Start()
func (r *CampaignRunner) Pause()
func (r *CampaignRunner) Resume()
func (r *CampaignRunner) Stop()
func (r *CampaignRunner) Stats() CampaignStats
```

#### FASE C6 — Monitoreo de ratio sent/received
- `infrastructure/campaign/health_monitor.go`
  - Trackea respuestas a mensajes de campaña
  - Si ratio sent/received < 0.05 (menos del 5% responde) → alerta
  - Si ratio < 0.02 → pausa automática de la campaña
  - Dashboard de health por device

#### FASE C7 — API REST
- `ui/rest/campaign.go`

| Método | Ruta | Acción |
|--------|------|--------|
| GET | /campaigns | Listar campañas |
| POST | /campaigns | Crear campaña |
| GET | /campaigns/:id | Detalle + stats |
| PUT | /campaigns/:id | Actualizar |
| DELETE | /campaigns/:id | Eliminar (si draft) |
| POST | /campaigns/:id/start | Iniciar |
| POST | /campaigns/:id/pause | Pausar |
| POST | /campaigns/:id/resume | Reanudar |
| POST | /campaigns/:id/cancel | Cancelar |
| GET | /campaigns/:id/recipients | Lista de destinatarios + status |
| POST | /campaigns/:id/recipients | Importar (JSON o CSV) |
| GET | /campaigns/:id/stats | Métricas en tiempo real |
| GET | /campaign-templates | Listar templates |
| POST | /campaign-templates | Crear template |
| PUT | /campaign-templates/:id | Actualizar template |
| DELETE | /campaign-templates/:id | Eliminar template |

#### FASE C8 — Importación de destinatarios
- `infrastructure/campaign/importer.go`
  - CSV: `phone,name,variable1,variable2,...`
  - JSON array: `[{"phone":"573166203787","name":"Gerlén","empresa":"Fututel"}]`
  - Desde grupo WhatsApp: extraer participantes vía API de grupo
  - Deduplicación automática
  - Validación de formato de teléfono

#### FASE C9 — Warmup (opcional, post-MVP)
- `infrastructure/campaign/warmup.go`
  - Programa de warmup de 7-15 días para números nuevos
  - Día 1-3: solo recibir mensajes (no enviar masivo)
  - Día 4-7: enviar 10-20 mensajes/día, incrementando gradualmente
  - Día 8-15: incrementar a 50, luego 100, luego 200
  - Conversaciones simuladas entre devices propios (alianza de warmup)
  - Tracking de health score durante warmup

#### FASE C10 — UI
- `views/components/CampaignManager.js`
  - Dashboard de campañas con status
  - Editor de template con preview de spintax
  - Importador de CSV/JSON con preview
  - Monitor en tiempo real (progress bar, stats)
  - Health dashboard por device

### Estimado: 40-60 horas (es un módulo completo)

### Orden recomendado (MVP primero)
```
C0 → C1 → C2 → C3 → C5 → C7 → C8 → C10  (MVP funcional)
                                  ↓
                    C4 → C6 → C9  (mejoras post-MVP)
```

---

## Prompts para Claude Code

### Prompt para Plan 1 (Webhooks por device):

```
Implement per-device webhook configuration for GoWA, following the same architecture
as the Chatwoot multi-device integration we just built. Use the same patterns:
SQLite table in chatstorage.db, global registry with getter/setter, backward
compatible fallback to WHATSAPP_WEBHOOK env var.

Plan phases: W0 (domain + repo), W1 (registry), W2 (forward by device),
W3 (REST API CRUD), W4 (Vue UI).

Key file to modify: infrastructure/whatsapp/webhook_forward.go → forwardToWebhooks()
must check per-device webhooks first, fallback to global.

Each device can have multiple webhook URLs. Support event filtering and
per-webhook HMAC secrets. Follow existing code conventions (AGENTS.md).
```

### Prompt para Plan 2 (Campañas masivas):

```
Implement a campaign/mass messaging module for GoWA with anti-ban protections.
This is a new domain (domains/campaign/) with its own infrastructure layer.

Core features:
1. Spintax template engine: "{Hola|Buenas} {nombre}" → unique messages
2. Human-curve delay engine: gaussian distribution 15-45s between messages,
   10-15min pause every 50 messages, active hours only (8am-8pm timezone)
3. Sender pool with number rotation: round-robin across devices, max 200/day
   per number, health score tracking (auto-disable on errors)
4. Presence simulation: typing indicator before send, proportional to message length
5. Campaign runner: pausable/resumable goroutine, real-time stats
6. Sent/received ratio monitoring: auto-pause if reply rate < 2%

REST API: full CRUD for campaigns, templates, recipients (CSV/JSON import).
Vue UI: campaign dashboard, template editor with spintax preview, real-time monitor.

SQLite tables: campaigns, campaign_senders, campaign_recipients, campaign_templates.
Follow existing conventions (AGENTS.md, SOLID, path aliases, no any).

Start with phases C0-C1-C2-C3-C5-C7-C8-C10 (MVP), then C4-C6-C9 (post-MVP).
```

---

## Prioridad recomendada

1. **AHORA**: Commit y push de lo que ya tienes (Chatwoot multi-device + delivery status)
2. **Siguiente**: Plan 1 (webhooks por device) — complementa el multi-device
3. **Después**: Plan 2 (campañas masivas) — es un módulo grande independiente