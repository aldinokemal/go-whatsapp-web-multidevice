# GoWA — Chatwoot Multi-Device / Multi-Inbox Integration

## Análisis del Estado Actual

### Arquitectura actual (singleton)

```
config/settings.go          → Variables globales: ChatwootURL, ChatwootAPIToken, ChatwootAccountID, ChatwootInboxID
                               ↓
chatwoot.GetDefaultClient() → sync.Once → UN solo *Client{BaseURL, APIToken, AccountID, InboxID}
                               ↓
webhook_forward.go          → forwardToChatwoot() llama GetDefaultClient() — ignora device_id
                               ↓
ui/rest/chatwoot.go         → HandleWebhook usa config.ChatwootDeviceID — UN solo device
```

**Problema central:** Todo el módulo Chatwoot es un singleton que lee de variables globales. No hay forma de mapear Device A → Inbox X, Device B → Inbox Y.

### Flujo de datos bidireccional

```
WhatsApp → GoWA:
  event_handler.go → handler() → handleMessage() → forwardPayloadToConfiguredWebhooks()
    → forwardToChatwoot() → chatwoot.GetDefaultClient() → syncMessageToChatwoot()
    El payload YA incluye "device_id" en el JSON, pero forwardToChatwoot() lo ignora.

Chatwoot → GoWA (webhook):
  ui/rest/chatwoot.go → HandleWebhook() → ResolveDevice(config.ChatwootDeviceID)
    → SendText/SendImage/etc al destino WhatsApp
    El payload de Chatwoot incluye account.id y conversation.inbox_id, pero se ignoran.
```

### Puntos de acoplamiento a modificar

| Archivo | Acoplamiento | Cambio requerido |
|---------|-------------|-----------------|
| `config/settings.go` | Variables globales Chatwoot* | Mantener como fallback/default, agregar flag `ChatwootMultiDeviceEnabled` |
| `infrastructure/chatwoot/client.go` | `GetDefaultClient()` singleton | Nuevo `ClientRegistry` que retorna `*Client` por device_id |
| `infrastructure/whatsapp/webhook_forward.go` | `forwardToChatwoot()` usa `GetDefaultClient()` | Extraer device_id del payload, buscar client correcto |
| `ui/rest/chatwoot.go` | `HandleWebhook` usa `config.ChatwootDeviceID` | Buscar device por inbox_id del webhook payload |
| `infrastructure/chatwoot/sync.go` | `SyncService` recibe un solo `*Client` | Recibir client por parámetro o desde registry |

---

## Base de datos

**No se necesita PostgreSQL ni Redis.** SQLite (el `whatsapp.db` existente o `chatstorage.db`) es suficiente.

### Nueva tabla: `chatwoot_device_configs`

```sql
CREATE TABLE IF NOT EXISTS chatwoot_device_configs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id       TEXT    NOT NULL UNIQUE,    -- "628xxx@s.whatsapp.net"
    chatwoot_url    TEXT    NOT NULL,            -- "https://chatwoot.fututel.com"
    api_token       TEXT    NOT NULL,
    account_id      INTEGER NOT NULL,
    inbox_id        INTEGER NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT 1,
    import_messages BOOLEAN NOT NULL DEFAULT 0,
    days_limit      INTEGER NOT NULL DEFAULT 3,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Índice para lookup por inbox_id (webhook Chatwoot → device)
CREATE INDEX IF NOT EXISTS idx_chatwoot_device_configs_inbox 
    ON chatwoot_device_configs(account_id, inbox_id) WHERE enabled = 1;
```

**Por qué inbox_id no es UNIQUE:** Podrías tener dos devices en el mismo inbox (edge case), pero el índice permite lookup rápido. Si quieres restringirlo, agrega `UNIQUE(account_id, inbox_id)`.

---

## Plan de Trabajo — Fases

### FASE 0: Preparación y repositorio de configuración
**Estimado: 2-3 horas**

**Objetivo:** Crear la capa de persistencia para configs Chatwoot por device, sin tocar nada del flujo existente.

#### 0.1 — Modelo de dominio
**Archivo nuevo:** `domains/chatwoot/chatwoot.go`

```go
package chatwoot

type DeviceConfig struct {
    ID             int    `json:"id"`
    DeviceID       string `json:"device_id"`
    ChatwootURL    string `json:"chatwoot_url"`
    APIToken       string `json:"api_token"`
    AccountID      int    `json:"account_id"`
    InboxID        int    `json:"inbox_id"`
    Enabled        bool   `json:"enabled"`
    ImportMessages bool   `json:"import_messages"`
    DaysLimit      int    `json:"days_limit"`
}
```

**Archivo nuevo:** `domains/chatwoot/interfaces.go`

```go
package chatwoot

type IDeviceConfigRepository interface {
    Migrate() error
    Save(cfg *DeviceConfig) error
    GetByDeviceID(deviceID string) (*DeviceConfig, error)
    GetByInboxID(accountID, inboxID int) (*DeviceConfig, error)
    GetAll() ([]*DeviceConfig, error)
    Delete(deviceID string) error
}
```

#### 0.2 — Implementación SQLite
**Archivo nuevo:** `infrastructure/chatwoot/device_config_repository.go`

- Implementa `IDeviceConfigRepository`
- Usa el mismo `*sql.DB` del chatstorage (o abre `whatsapp.db`)
- `Migrate()` ejecuta el CREATE TABLE + índice
- CRUD estándar

#### 0.3 — Inicialización
**Archivo a modificar:** `cmd/helpers.go` o `cmd/rest.go`

- Llamar `repo.Migrate()` al iniciar
- Si `config.ChatwootEnabled` y no hay registros en la tabla, auto-crear uno con los valores de las env vars actuales (migración transparente)

**Criterio de completado:** La tabla existe, se puede hacer CRUD, los valores actuales de env vars se migran automáticamente. El flujo actual NO cambia.

---

### FASE 1: Client Registry (reemplazo del singleton)
**Estimado: 3-4 horas**

**Objetivo:** Reemplazar `GetDefaultClient()` con un registry que retorna `*Client` por device_id, manteniendo backward compatibility.

#### 1.1 — ClientRegistry
**Archivo nuevo:** `infrastructure/chatwoot/registry.go`

```go
type ClientRegistry struct {
    mu       sync.RWMutex
    clients  map[string]*Client  // key: device_id
    repo     IDeviceConfigRepository
}

func NewClientRegistry(repo IDeviceConfigRepository) *ClientRegistry

// GetClientForDevice retorna el *Client configurado para ese device.
// Si no hay config específica y ChatwootEnabled==true, retorna el default (env vars).
func (r *ClientRegistry) GetClientForDevice(deviceID string) (*Client, error)

// GetClientForInbox retorna el *Client y device_id para un inbox dado.
// Usado por el webhook handler de Chatwoot.
func (r *ClientRegistry) GetClientForInbox(accountID, inboxID int) (*Client, string, error)

// Refresh recarga configs desde la DB (llamar después de CRUD).
func (r *ClientRegistry) Refresh() error
```

**Lógica interna de GetClientForDevice:**
1. Buscar en cache `clients[deviceID]`
2. Si no existe, buscar en repo `GetByDeviceID(deviceID)`
3. Si no existe Y hay config global (env vars), retornar `GetDefaultClient()` (backward compat)
4. Si no existe nada, retornar `nil, ErrNoConfig`

#### 1.2 — Inicialización global del registry
**Archivo a modificar:** `cmd/rest.go`

```go
// Después de inicializar el DeviceManager y chatStorageRepo:
cwConfigRepo := chatwootdb.NewDeviceConfigRepository(db)
cwConfigRepo.Migrate()
cwRegistry := chatwoot.NewClientRegistry(cwConfigRepo)
// Inyectar en donde se necesite
```

#### 1.3 — Variable global temporal
**Archivo a modificar:** `infrastructure/chatwoot/client.go`

Agregar:
```go
var globalRegistry *ClientRegistry

func SetGlobalRegistry(r *ClientRegistry) { globalRegistry = r }
func GetGlobalRegistry() *ClientRegistry  { return globalRegistry }
```

Esto permite que `webhook_forward.go` acceda al registry sin refactorizar todas las firmas de función de golpe.

**Criterio de completado:** El registry existe, se inicializa al boot, `GetDefaultClient()` sigue funcionando como fallback. Nada se rompe.

---

### FASE 2: Forward WhatsApp → Chatwoot (multi-device)
**Estimado: 2-3 horas**

**Objetivo:** Cuando llega un mensaje de WhatsApp, usar el client correcto según el device que lo recibió.

#### 2.1 — Modificar `forwardToChatwoot()`
**Archivo:** `infrastructure/whatsapp/webhook_forward.go`

Cambio en `forwardToChatwoot()`:

```go
func forwardToChatwoot(ctx context.Context, payload map[string]any, eventName string) {
    // NUEVO: extraer device_id del payload
    deviceID, _ := payload["device_id"].(string)
    
    // NUEVO: buscar client por device_id
    registry := chatwoot.GetGlobalRegistry()
    var cw *chatwoot.Client
    if registry != nil && deviceID != "" {
        var err error
        cw, err = registry.GetClientForDevice(deviceID)
        if err != nil {
            logrus.Warnf("Chatwoot: No config for device %s: %v", deviceID, err)
            return
        }
    } else {
        // Fallback al singleton (backward compat)
        cw = chatwoot.GetDefaultClient()
    }
    
    if !cw.IsConfigured() {
        logrus.Warn("Chatwoot: Client not configured")
        return
    }
    
    // ... resto igual ...
}
```

**Cambio mínimo, máximo impacto.** Solo se modifica el punto donde se obtiene el `*Client`.

**Criterio de completado:** Device A manda mensajes al Inbox X en Chatwoot, Device B al Inbox Y. Verificable con logs.

---

### FASE 3: Webhook Chatwoot → GoWA (multi-device)
**Estimado: 3-4 horas**

**Objetivo:** Cuando Chatwoot envía un webhook (agente responde), rutear al device correcto.

#### 3.1 — Modificar `HandleWebhook()`
**Archivo:** `ui/rest/chatwoot.go`

El payload de Chatwoot incluye `account.id` y `conversation.inbox_id`. Usarlos para resolver el device:

```go
func (h *ChatwootHandler) HandleWebhook(c *fiber.Ctx) error {
    var payload chatwoot.WebhookPayload
    if err := c.BodyParser(&payload); err != nil {
        return utils.ResponseError(c, "Invalid payload")
    }
    
    // NUEVO: resolver device por inbox_id
    registry := chatwoot.GetGlobalRegistry()
    var instance *whatsapp.DeviceInstance
    var resolvedID string
    
    if registry != nil {
        // Buscar qué device está asociado a este inbox
        _, deviceID, err := registry.GetClientForInbox(payload.Account.ID, payload.Conversation.InboxID)
        if err == nil && deviceID != "" {
            instance, resolvedID, err = h.DeviceManager.ResolveDevice(deviceID)
        }
    }
    
    // Fallback al config global si no se encontró
    if instance == nil {
        var err error
        instance, resolvedID, err = h.DeviceManager.ResolveDevice(config.ChatwootDeviceID)
        if err != nil {
            // ... error handling existente ...
        }
    }
    
    // ... resto igual ...
}
```

#### 3.2 — Agregar `InboxID` al WebhookPayload
**Archivo:** `infrastructure/chatwoot/types.go`

El `ConversationWebhook` actual no tiene `InboxID`. Agregarlo:

```go
type ConversationWebhook struct {
    ID      int              `json:"id"`
    InboxID int              `json:"inbox_id"`  // NUEVO
    Meta    ConversationMeta `json:"meta"`
}
```

Verificar que Chatwoot realmente envía `inbox_id` en el webhook payload (sí lo hace en v4.x).

**Criterio de completado:** Un agente responde en Chatwoot Inbox X → mensaje sale por Device A. Responde en Inbox Y → sale por Device B.

---

### FASE 4: API REST para gestionar configs
**Estimado: 2-3 horas**

**Objetivo:** CRUD de mappings device ↔ Chatwoot inbox via REST API.

#### 4.1 — Endpoints nuevos
**Archivo nuevo:** `ui/rest/chatwoot_config.go`

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/chatwoot/configs` | Listar todos los mappings |
| GET | `/chatwoot/configs/:device_id` | Obtener config de un device |
| POST | `/chatwoot/configs` | Crear mapping device ↔ inbox |
| PUT | `/chatwoot/configs/:device_id` | Actualizar mapping |
| DELETE | `/chatwoot/configs/:device_id` | Eliminar mapping |

**Request body (POST/PUT):**
```json
{
    "device_id": "628xxx@s.whatsapp.net",
    "chatwoot_url": "https://chatwoot.fututel.com",
    "api_token": "vMqzdhUxR6ACqjje4ixaScz6",
    "account_id": 2,
    "inbox_id": 67,
    "enabled": true,
    "import_messages": false,
    "days_limit": 3
}
```

#### 4.2 — Registrar rutas
**Archivo a modificar:** `cmd/rest.go`

```go
if config.ChatwootEnabled {
    chatwootConfigHandler := rest.NewChatwootConfigHandler(cwConfigRepo, cwRegistry)
    apiGroup.Get("/chatwoot/configs", chatwootConfigHandler.List)
    apiGroup.Get("/chatwoot/configs/:device_id", chatwootConfigHandler.Get)
    apiGroup.Post("/chatwoot/configs", chatwootConfigHandler.Create)
    apiGroup.Put("/chatwoot/configs/:device_id", chatwootConfigHandler.Update)
    apiGroup.Delete("/chatwoot/configs/:device_id", chatwootConfigHandler.Delete)
}
```

#### 4.3 — Refresh del registry después de CRUD
Cada operación de escritura (Create/Update/Delete) llama `registry.Refresh()` para recargar la cache.

**Criterio de completado:** Puedes crear/editar/borrar mappings via curl o Postman. Los cambios surten efecto sin reiniciar.

---

### FASE 5: SyncService multi-device
**Estimado: 1-2 horas**

**Objetivo:** El sync de historial usa el client correcto por device.

#### 5.1 — Modificar SyncHistory endpoint
**Archivo:** `ui/rest/chatwoot.go` → `SyncHistory()`

En vez de `chatwoot.GetDefaultClient()`, usar:

```go
registry := chatwoot.GetGlobalRegistry()
cwClient, err := registry.GetClientForDevice(resolvedID)
if err != nil {
    cwClient = chatwoot.GetDefaultClient() // fallback
}
```

#### 5.2 — Eliminar singleton de SyncService
**Archivo:** `infrastructure/chatwoot/sync.go`

El `GetSyncService()` con `sync.Once` asume un solo client. Cambiar a crear instancias por request con el client correcto, o cambiar `SyncService` para que acepte el `*Client` como parámetro en `SyncHistory()` en vez de en el constructor.

**Criterio de completado:** `POST /chatwoot/sync?device_id=X` usa el inbox correcto para ese device.

---

### FASE 6: UI (opcional pero recomendada)
**Estimado: 3-4 horas**

**Objetivo:** Gestionar mappings device ↔ inbox desde la UI web.

#### 6.1 — Componente Vue
**Archivo nuevo:** `views/components/ChatwootConfig.js`

- Tabla con los mappings existentes
- Formulario para agregar/editar (device selector, URL, token, account ID, inbox ID)
- Botón de test connection (verificar que el token funcione contra la URL)
- Toggle enabled/disabled

#### 6.2 — Registrar en index.html
Agregar sección "Chatwoot" entre "Chat Management" y el footer.

---

## Resumen de archivos

### Archivos nuevos (6)
| Archivo | Fase |
|---------|------|
| `domains/chatwoot/chatwoot.go` | 0 |
| `domains/chatwoot/interfaces.go` | 0 |
| `infrastructure/chatwoot/device_config_repository.go` | 0 |
| `infrastructure/chatwoot/registry.go` | 1 |
| `ui/rest/chatwoot_config.go` | 4 |
| `views/components/ChatwootConfig.js` | 6 |

### Archivos modificados (7)
| Archivo | Fase | Impacto |
|---------|------|---------|
| `cmd/rest.go` | 0, 1, 4 | Inicialización del repo y registry, rutas nuevas |
| `infrastructure/chatwoot/client.go` | 1 | Agregar global registry getter/setter |
| `infrastructure/chatwoot/types.go` | 3 | Agregar InboxID a ConversationWebhook |
| `infrastructure/whatsapp/webhook_forward.go` | 2 | forwardToChatwoot() usa registry |
| `ui/rest/chatwoot.go` | 3, 5 | HandleWebhook y SyncHistory usan registry |
| `infrastructure/chatwoot/sync.go` | 5 | SyncService recibe client por parámetro |
| `views/index.html` | 6 | Sección Chatwoot config |

### Archivos NO modificados
- `config/settings.go` — Las env vars se mantienen como fallback/default
- `infrastructure/whatsapp/event_handler.go` — No necesita cambios
- `infrastructure/whatsapp/device_manager.go` — No necesita cambios
- Todo el módulo `domains/send/`, `usecase/`, `validations/` — Intactos

---

## Orden de ejecución recomendado

```
FASE 0 (repo + migración)     → Puedes deployar sin romper nada
FASE 1 (registry)             → Puedes deployar sin romper nada  
FASE 2 (WA → Chatwoot)        → Primer cambio funcional: mensajes entrantes ruteados
FASE 3 (Chatwoot → WA)        → Segundo cambio funcional: respuestas ruteadas
FASE 4 (API REST)              → Gestión sin editar .env ni reiniciar
FASE 5 (sync multi-device)    → Sync history por device
FASE 6 (UI)                   → Nice to have
```

Fases 0-1 son deployables sin riesgo (backward compatible). Fases 2-3 son el core funcional. Fases 4-6 son calidad de vida.

**Tiempo total estimado: 15-20 horas de desarrollo.**

---

## Testing

### Por fase
- **Fase 0:** Unit test del repository (CRUD)
- **Fase 1:** Unit test del registry (cache, fallback a default, refresh)
- **Fase 2:** Test con 2 devices conectados, verificar que cada uno envía al inbox correcto en Chatwoot
- **Fase 3:** Test respondiendo desde 2 inboxes distintos en Chatwoot, verificar que sale por el device correcto
- **Fase 4:** curl CRUD de configs
- **Fase 5:** Sync con device_id explícito

### Test de integración end-to-end
1. Conectar Device A (número personal) → Inbox 67
2. Conectar Device B (número empresa) → Inbox 68
3. Enviar mensaje a número personal → aparece en Inbox 67
4. Enviar mensaje a número empresa → aparece en Inbox 68
5. Responder desde Inbox 67 → sale por Device A
6. Responder desde Inbox 68 → sale por Device B