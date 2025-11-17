# Multi-WhatsApp Session Implementation Guide

## Overview

This document describes the implementation of multi-WhatsApp session functionality in the go-whatsapp-web-multidevice project. The implementation allows users to connect and manage multiple WhatsApp accounts simultaneously within a single application instance.

## ✅ Completed Components

### 1. Session Manager (`src/infrastructure/whatsapp/session_manager.go`)

The `SessionManager` is a singleton that manages multiple WhatsApp sessions:

**Key Features:**
- Thread-safe session management with mutex locks
- Support for multiple concurrent WhatsApp clients
- Default session selection
- Per-session resource management (Client, DB, KeysDB, ChatStorageRepo)

**Main Methods:**
```go
// Session management
AddSession(sessionID string, client *whatsmeow.Client, ...) error
RemoveSession(sessionID string) error
GetSession(sessionID string) (*Session, error)

// Client access
GetClient(sessionID string) (*whatsmeow.Client, error)
GetClientOrDefault(sessionID string) (*whatsmeow.Client, error)

// Status and metadata
GetAllSessionsWithStatus() []map[string]interface{}
GetConnectionStatus(sessionID string) (bool, bool, string, error)

// Default session
GetDefaultSession() string
SetDefaultSession(sessionID string) error
```

**Usage Example:**
```go
sm := whatsapp.GetSessionManager()

// Add a new session
err := sm.AddSession("work", client, db, keysDB, chatRepo)

// Get client for a session
client, err := sm.GetClient("work")

// List all sessions
sessions := sm.GetAllSessionsWithStatus()
```

### 2. Database Schema Updates

**Migration 3** adds multi-session support to the chat storage database:

**Changes:**
- Added `session_id` column to `chats` and `messages` tables
- Updated primary keys: `(jid, session_id)` for chats, `(id, chat_jid, session_id)` for messages
- Created indexes for efficient session-based queries
- Existing data migrated to `session_id = "default"`

**Schema:**
```sql
-- Chats table
CREATE TABLE chats (
    jid TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT 'default',
    name TEXT NOT NULL,
    last_message_time TIMESTAMP NOT NULL,
    ephemeral_expiration INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (jid, session_id)
);

-- Messages table
CREATE TABLE messages (
    id TEXT NOT NULL,
    chat_jid TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT 'default',
    sender TEXT NOT NULL,
    content TEXT,
    timestamp TIMESTAMP NOT NULL,
    is_from_me BOOLEAN DEFAULT FALSE,
    media_type TEXT,
    -- ... other fields ...
    PRIMARY KEY (id, chat_jid, session_id),
    FOREIGN KEY (chat_jid, session_id) REFERENCES chats(jid, session_id)
);
```

### 3. Repository Layer Updates

**Updated Methods in `sqlite_repository.go`:**

- `StoreChat()` - Sets default session_id if not provided
- `GetChat()` - Wrapper for backward compatibility
- `GetChatBySession()` - Session-aware chat retrieval
- `StoreMessage()` - Includes session_id in storage
- `GetMessages()` - Filters by session_id
- `GetChats()` - Filters by session_id
- `CreateMessage()` - Extracts session_id from context
- `StoreSentMessageWithContext()` - Extracts session_id from context

**Context-Based Session Tracking:**
```go
// Use the documented context key from chatstorage package
import domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"

// Set sessionID in context
ctx = domainChatStorage.WithSessionID(ctx, sessionID)

// Extract sessionID from context
sessionID := domainChatStorage.GetSessionIDOrDefault(ctx)

// Or check if it exists
sessionID, ok := domainChatStorage.GetSessionID(ctx)
if !ok {
    sessionID = "default"
}
```

### 4. Domain Model Updates

**Updated Structs:**
```go
type Chat struct {
    JID                 string    `db:"jid"`
    SessionID           string    `db:"session_id"`  // NEW
    Name                string    `db:"name"`
    LastMessageTime     time.Time `db:"last_message_time"`
    // ... other fields
}

type Message struct {
    ID            string    `db:"id"`
    ChatJID       string    `db:"chat_jid"`
    SessionID     string    `db:"session_id"`  // NEW
    Sender        string    `db:"sender"`
    Content       string    `db:"content"`
    // ... other fields
}

type MessageFilter struct {
    ChatJID   string
    SessionID string  // NEW
    Limit     int
    // ... other fields
}

type ChatFilter struct {
    SessionID  string  // NEW
    Limit      int
    // ... other fields
}
```

### 5. Session-Aware Event Handlers

**New Event Handler Architecture:**

```go
// Legacy handler (uses "default" session)
func handler(ctx context.Context, rawEvt any, chatStorageRepo)

// Session-aware handler
func handlerWithSession(ctx context.Context, sessionID string, rawEvt any, chatStorageRepo)

// Session-specific event handlers
func handleMessageWithSession(ctx, sessionID, evt, chatStorageRepo)
func handleLoggedOutWithSession(ctx, sessionID, chatStorageRepo)
func handleDeleteForMeWithSession(ctx, sessionID, evt, chatStorageRepo)
func handleHistorySyncWithSession(ctx, sessionID, evt, chatStorageRepo)
```

**Session Context Flow:**
1. Event received for a specific session
2. Session ID stored in context
3. Event handlers extract session ID from context
4. Chat storage operations use session ID for data partitioning

### 6. Infrastructure Initialization

**New Initialization Functions:**

```go
// Session-aware client initialization
func InitWaCLIWithSession(
    ctx context.Context,
    sessionID string,
    storeContainer, keysStoreContainer *sqlstore.Container,
    chatStorageRepo domainChatStorage.IChatStorageRepository
) (*whatsmeow.Client, error)

// Get client for specific session
func GetClientForSession(sessionID string) (*whatsmeow.Client, error)

// Get client for session or default
// NOTE: Legacy global client fallback has been removed. If sessionID is empty,
// this function will use the default session from SessionManager. Returns an
// error if no sessions are available.
func GetClientOrDefault(sessionID string) (*whatsmeow.Client, error)
```

### 7. REST API Endpoints

**New Session Management Endpoints:**

```plaintext
GET  /sessions                  - List all sessions with status
GET  /sessions/:id/status       - Get status of specific session
POST /sessions/:id/set-default  - Set default session
```

**Response Format:**
```json
{
  "status": 200,
  "code": "SUCCESS",
  "message": "Sessions retrieved successfully",
  "results": {
    "sessions": [
      {
        "session_id": "default",
        "is_connected": true,
        "is_logged_in": true,
        "device_id": "1234567890:1@s.whatsapp.net",
        "is_default": true
      },
      {
        "session_id": "work",
        "is_connected": true,
        "is_logged_in": true,
        "device_id": "9876543210:2@s.whatsapp.net",
        "is_default": false
      }
    ],
    "count": 2
  }
}
```

## 🚧 Pending Implementation

### 1. Update Existing REST API Endpoints

**Goal:** Add optional `session` query parameter to all existing endpoints.

**Example Updates Needed:**

```go
// Before
POST /send/message
{
    "phone": "6281234567890",
    "message": "Hello"
}

// After
POST /send/message?session=work
{
    "phone": "6281234567890",
    "message": "Hello"
}
```

**Files to Update:**
- `src/ui/rest/send.go` - All send endpoints
- `src/ui/rest/message.go` - Message management endpoints
- `src/ui/rest/group.go` - Group management endpoints
- `src/ui/rest/user.go` - User/account endpoints
- `src/ui/rest/chat.go` - Chat management endpoints

**Implementation Pattern:**
```go
func (handler *Send) SendMessage(c *fiber.Ctx) error {
    // Extract session ID from query parameter
    sessionID := c.Query("session", "default")

    // Get client for session
    client, err := whatsapp.GetClientOrDefault(sessionID)
    if err != nil {
        return c.Status(400).JSON(utils.ResponseData{
            Status:  400,
            Code:    "ERROR",
            Message: fmt.Sprintf("Session not found: %v", err),
        })
    }

    // Use client for operations...
    // Pass sessionID in context for chat storage
    ctx := domainChatStorage.WithSessionID(c.UserContext(), sessionID)

    // Continue with normal logic...
}
```

### 2. Update Usecase Layer

**Goal:** Make all use cases session-aware.

**Files to Update:**
- `src/usecase/app.go`
- `src/usecase/send.go`
- `src/usecase/message.go`
- `src/usecase/group.go`
- `src/usecase/user.go`
- `src/usecase/chat.go`

**Pattern:**
```go
// Update interface signatures
type ISendUsecase interface {
    SendText(ctx context.Context, sessionID string, request SendTextRequest) error
    SendImage(ctx context.Context, sessionID string, request SendImageRequest) error
    // ... other methods
}

// Update implementations
func (service *serviceSend) SendText(
    ctx context.Context,
    sessionID string,
    request SendTextRequest,
) error {
    // Get client for session
    client, err := whatsapp.GetClientOrDefault(sessionID)
    if err != nil {
        return err
    }

    // Add sessionID to context for chat storage
    ctx = domainChatStorage.WithSessionID(ctx, sessionID)

    // Use client for operations...
}
```

### 3. Update WebSocket Handlers

**Goal:** Enable real-time updates for all sessions.

**Files to Update:**
- `src/ui/websocket/websocket.go`

**Required Changes:**

1. **Update message structure:**
```go
type WebSocketMessage struct {
    Code      string                 `json:"code"`
    SessionID string                 `json:"session_id,omitempty"`
    Message   string                 `json:"message"`
    Result    map[string]interface{} `json:"result,omitempty"`
}
```

2. **Broadcast session-specific events:**
```go
// In event handlers, broadcast with session ID
func BroadcastLoginSuccess(sessionID string) {
    websocket.Broadcast <- WebSocketMessage{
        Code:      "LOGIN_SUCCESS",
        SessionID: sessionID,
        Message:   fmt.Sprintf("Session %s logged in successfully", sessionID),
    }
}
```

3. **Handle FETCH_DEVICES per session:**
```go
case "FETCH_DEVICES":
    sessionID := msg.SessionID
    if sessionID == "" {
        // Return all sessions
        sm := whatsapp.GetSessionManager()
        sessions := sm.GetAllSessionsWithStatus()
        // Send sessions to WebSocket...
    } else {
        // Return specific session
        // ...
    }
```

### 4. Frontend Updates

**Goal:** Create UI for session selection and management.

**Files to Update:**
- `src/views/index.html`
- Create new components for session management

**Required UI Components:**

1. **Session Selector Component:**
```html
<div class="ui segment">
    <h3>Active Sessions</h3>
    <div class="ui selection dropdown" id="session-selector">
        <input type="hidden" name="session">
        <i class="dropdown icon"></i>
        <div class="default text">Select Session</div>
        <div class="menu">
            <div class="item" data-value="default">Default Session</div>
            <div class="item" data-value="work">Work Session</div>
            <!-- Populated dynamically -->
        </div>
    </div>
</div>
```

2. **Session Status Display:**
```html
<div class="ui cards" id="session-status">
    <div class="card" data-session="default">
        <div class="content">
            <div class="header">Default Session</div>
            <div class="meta">
                <span class="category">
                    <i class="green circle icon"></i> Connected
                </span>
            </div>
            <div class="description">
                Device: 1234567890:1@s.whatsapp.net
            </div>
        </div>
        <div class="extra content">
            <button class="ui button" onclick="setDefault('default')">
                Set as Default
            </button>
        </div>
    </div>
</div>
```

3. **Update All Forms:**
```html
<!-- Add hidden session field to all forms -->
<form id="send-message-form">
    <input type="hidden" name="session" id="current-session" value="default">
    <input type="text" name="phone" placeholder="Phone Number">
    <textarea name="message" placeholder="Message"></textarea>
    <button type="submit">Send</button>
</form>
```

4. **JavaScript for Session Management:**
```javascript
// Global variable for current session
let currentSession = 'default';

// Fetch and display sessions
async function loadSessions() {
    const response = await window.http.get('/sessions');
    const sessions = response.data.results.sessions;

    // Update session selector
    updateSessionSelector(sessions);

    // Update session status cards
    updateSessionStatus(sessions);
}

// Handle session change
function switchSession(sessionID) {
    currentSession = sessionID;

    // Update all forms
    document.querySelectorAll('[name="session"]').forEach(input => {
        input.value = sessionID;
    });

    // Reload data for new session
    reloadChatList();
}

// Set session as default
async function setDefault(sessionID) {
    await window.http.post(`/sessions/${sessionID}/set-default`);
    loadSessions();
}

// Add session parameter to all API calls
window.http.interceptors.request.use(config => {
    if (currentSession && currentSession !== 'default') {
        config.params = config.params || {};
        config.params.session = currentSession;
    }
    return config;
});
```

### 5. Configuration Updates

**Goal:** Support multiple session configurations.

**Potential Additions to `config/settings.go`:**

```go
// Session configuration
type SessionConfig struct {
    SessionID string
    DBURI     string
    DBKeysURI string
    // Other session-specific settings
}

var SessionConfigs = []SessionConfig{
    {
        SessionID: "default",
        DBURI:     "file:storages/whatsapp.db",
        DBKeysURI: "",
    },
    {
        SessionID: "work",
        DBURI:     "file:storages/whatsapp_work.db",
        DBKeysURI: "",
    },
}
```

**Environment Variables:**
```bash
# Default session
DB_URI=file:storages/whatsapp.db
DB_KEYS_URI=

# Additional sessions (comma-separated)
SESSIONS=work,personal
WORK_DB_URI=file:storages/whatsapp_work.db
PERSONAL_DB_URI=file:storages/whatsapp_personal.db
```

### 6. Session Initialization in root.go

**Goal:** Initialize multiple sessions on startup.

**Updates to `src/cmd/root.go`:**

```go
func initApp() {
    // ... existing code ...

    // Initialize session manager
    sm := whatsapp.GetSessionManager()

    // Initialize default session (existing code)
    whatsappDB := whatsapp.InitWaDB(ctx, config.DBURI)
    var keysDB *sqlstore.Container
    if config.DBKeysURI != "" {
        keysDB = whatsapp.InitWaDB(ctx, config.DBKeysURI)
    }

    defaultClient, err := whatsapp.InitWaCLIWithSession(
        ctx, "default", whatsappDB, keysDB, chatStorageRepo,
    )
    if err != nil {
        panic(err)
    }

    // Add to session manager
    err = sm.AddSession("default", defaultClient, whatsappDB, keysDB, chatStorageRepo)
    if err != nil {
        panic(err)
    }

    // Set backward compatibility global client
    whatsappCli = defaultClient

    // Initialize additional sessions from config
    for _, sessionConfig := range config.SessionConfigs {
        if sessionConfig.SessionID == "default" {
            continue // Already initialized
        }

        sessionDB := whatsapp.InitWaDB(ctx, sessionConfig.DBURI)
        var sessionKeysDB *sqlstore.Container
        if sessionConfig.DBKeysURI != "" {
            sessionKeysDB = whatsapp.InitWaDB(ctx, sessionConfig.DBKeysURI)
        }

        sessionClient, err := whatsapp.InitWaCLIWithSession(
            ctx, sessionConfig.SessionID, sessionDB, sessionKeysDB, chatStorageRepo,
        )
        if err != nil {
            logrus.Errorf("Failed to initialize session %s: %v",
                sessionConfig.SessionID, err)
            continue
        }

        err = sm.AddSession(
            sessionConfig.SessionID, sessionClient, sessionDB, sessionKeysDB, chatStorageRepo,
        )
        if err != nil {
            logrus.Errorf("Failed to add session %s to manager: %v",
                sessionConfig.SessionID, err)
        }
    }

    // ... rest of initialization ...
}
```

## Testing Strategy

### Unit Tests

1. **SessionManager Tests:**
```go
func TestSessionManager_AddSession(t *testing.T)
func TestSessionManager_RemoveSession(t *testing.T)
func TestSessionManager_GetClient(t *testing.T)
func TestSessionManager_ConcurrentAccess(t *testing.T)
```

2. **Repository Tests:**
```go
func TestStoreChat_WithSession(t *testing.T)
func TestGetChats_FilterBySession(t *testing.T)
func TestStoreMessage_WithSession(t *testing.T)
```

### Integration Tests

1. **Multi-Session Login:**
   - Start application
   - Login with first WhatsApp account (session: "default")
   - Login with second WhatsApp account (session: "work")
   - Verify both sessions are active

2. **Message Sending:**
   - Send message from "default" session
   - Send message from "work" session
   - Verify messages are sent from correct accounts
   - Verify messages are stored with correct session_id

3. **Chat Storage:**
   - Receive messages on both sessions
   - Verify chats are properly partitioned by session
   - Query chats for specific session
   - Verify correct chats are returned

### Manual Testing Checklist

- [ ] Database migration runs successfully
- [ ] Existing data migrated to "default" session
- [ ] Can list all sessions via `/sessions` endpoint
- [ ] Can get status of specific session
- [ ] Can set default session
- [ ] Can send message from specific session
- [ ] Can receive messages on multiple sessions
- [ ] WebSocket broadcasts work for all sessions
- [ ] Frontend displays all sessions correctly
- [ ] Session selector works in frontend
- [ ] All forms include session parameter
- [ ] Chat list filters by selected session

## Performance Considerations

1. **Database Indexes:**
   - Composite indexes on `(session_id, jid)` for chats
   - Composite indexes on `(session_id, chat_jid)` for messages
   - These are already included in Migration 3

2. **Connection Pooling:**
   - Each session has its own DB connection
   - Monitor connection pool usage with multiple sessions
   - Consider connection limits based on available resources

3. **Memory Usage:**
   - Each WhatsApp client maintains its own state
   - Monitor memory usage with multiple active sessions
   - Consider max session limits

4. **Event Processing:**
   - Each session has its own event handlers
   - Events are processed independently per session
   - Ensure event handlers don't block each other

## Security Considerations

1. **Session Isolation:**
   - Sessions must not access each other's data
   - Database queries must always filter by session_id
   - Verify foreign key constraints enforce isolation

2. **Session Authentication:**
   - Each session requires independent QR code scan/pairing
   - Session credentials stored separately
   - No session can impersonate another

3. **API Access Control:**
   - Consider adding session-level permissions
   - Validate session_id in all API requests
   - Prevent unauthorized session access

## Migration Path

### For Existing Users

1. **Automatic Migration:**
   - On first startup after upgrade, Migration 3 runs automatically
   - All existing data tagged with `session_id = "default"`
   - No manual intervention required
   - No data loss

2. **Backward Compatibility:**
   - All existing API calls work without changes
   - Omitting `session` parameter uses "default" session
   - Global `whatsappCli` variable still works for legacy initialization

3. **Breaking Changes:**
   - **`GetClientOrDefault()` no longer falls back to global `cli` variable**
     - Previously: Would return legacy global client if no sessions existed
     - Now: Returns error if SessionManager has no sessions
     - **Action Required:** Ensure at least one session is initialized via SessionManager
     - Error message: "no sessions available: please initialize at least one session through SessionManager"
   - Code using `GetClientOrDefault()` must handle the case where no sessions exist
   - Legacy global client is only maintained for backward compatibility with `InitWaCLI()`

4. **Gradual Adoption:**
   - Users can continue using single session
   - Multi-session features opt-in
   - Add new sessions when ready

### For New Users

1. **Clean Installation:**
   - Database created with session support
   - First login creates "default" session
   - Can immediately add more sessions

## Troubleshooting

### Database Issues

**Problem:** Migration fails
**Solution:** Check database permissions, verify SQLite version supports ALTER TABLE

**Problem:** Foreign key constraint errors
**Solution:** Ensure chat exists before inserting message, verify session_id matches

### Session Management Issues

**Problem:** Session not found
**Solution:** Check session exists in SessionManager, verify session_id spelling

**Problem:** Client returns nil
**Solution:** Verify session was properly initialized, check initialization logs

**Problem:** "no sessions available: please initialize at least one session through SessionManager"
**Solution:**
- This occurs when `GetClientOrDefault()` is called but SessionManager has no sessions
- Ensure at least one session is initialized via `sm.AddSession()` or `InitWaCLIWithSession()`
- The legacy global client fallback has been removed; use explicit session management
- Check that session initialization completed successfully during app startup

### Event Handling Issues

**Problem:** Messages not stored with correct session
**Solution:** Verify session_id is in context, check event handler passes sessionID

**Problem:** Events not firing for some sessions
**Solution:** Check client event handlers are registered, verify client is connected

## Additional Resources

- [WhatsApp Multi-Device Documentation](https://github.com/tulir/whatsmeow)
- [Session Management Best Practices](https://pkg.go.dev/sync#RWMutex)
- [Database Migration Guide](https://www.sqlite.org/lang_altertable.html)

## Contributors

Implementation by: Claude Code Assistant
Date: 2025-01-17
Version: 1.0

## Changelog

### Version 1.0 (2025-01-17)
- Initial implementation of multi-session infrastructure
- Database schema updates with migration
- SessionManager singleton
- Session-aware event handlers
- REST API endpoints for session management
- Context-based session tracking

### Future Versions
- Complete REST API integration
- Frontend session management UI
- Usecase layer updates
- WebSocket multi-session support
- Comprehensive testing suite
