package whatsapp

import (
	"context"
	"fmt"
	"sort"
	"sync"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

// Session represents a single WhatsApp session with its associated resources
type Session struct {
	ID              string
	Client          *whatsmeow.Client
	DB              *sqlstore.Container
	KeysDB          *sqlstore.Container
	ChatStorageRepo domainChatStorage.IChatStorageRepository
}

// SessionManager manages multiple WhatsApp sessions
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	default  string // Default session ID
}

// Global session manager instance
var (
	sessionManager     *SessionManager
	sessionManagerOnce sync.Once
)

// GetSessionManager returns the singleton SessionManager instance
func GetSessionManager() *SessionManager {
	sessionManagerOnce.Do(func() {
		sessionManager = &SessionManager{
			sessions: make(map[string]*Session),
		}
	})
	return sessionManager
}

// AddSession adds a new session to the manager
func (sm *SessionManager) AddSession(sessionID string, client *whatsmeow.Client, db *sqlstore.Container, keysDB *sqlstore.Container, chatStorageRepo domainChatStorage.IChatStorageRepository) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[sessionID]; exists {
		return fmt.Errorf("session %s already exists", sessionID)
	}

	sm.sessions[sessionID] = &Session{
		ID:              sessionID,
		Client:          client,
		DB:              db,
		KeysDB:          keysDB,
		ChatStorageRepo: chatStorageRepo,
	}

	// Set as default if it's the first session
	if len(sm.sessions) == 1 {
		sm.default = sessionID
		logrus.Infof("Session %s set as default session", sessionID)
	}

	logrus.Infof("Session %s added to session manager (total sessions: %d)", sessionID, len(sm.sessions))
	return nil
}

// RemoveSession removes a session from the manager and closes its database handles
func (sm *SessionManager) RemoveSession(sessionID string) error {
	// Step 1: Lock mutex and copy session out, then remove from map
	sm.mu.Lock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		sm.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Copy the session value before removing
	sessionCopy := *session

	// Remove from sessions map
	delete(sm.sessions, sessionID)

	// Update default session if removed
	remainingCount := len(sm.sessions)
	if sm.default == sessionID {
		sm.default = ""
		// Set a new default from remaining sessions (deterministically)
		if remainingCount > 0 {
			// Gather remaining session IDs into a slice
			sessionIDs := make([]string, 0, remainingCount)
			for id := range sm.sessions {
				sessionIDs = append(sessionIDs, id)
			}
			// Sort for deterministic selection
			sort.Strings(sessionIDs)
			// Pick the first session alphabetically as the new default
			sm.default = sessionIDs[0]
			logrus.Infof("Session %s set as new default session", sm.default)
		}
	}

	// Unlock before performing I/O operations
	sm.mu.Unlock()

	// Step 2: Perform cleanup I/O operations without holding the lock
	// Disconnect the client if it's connected
	if sessionCopy.Client != nil && sessionCopy.Client.IsConnected() {
		sessionCopy.Client.Disconnect()
		logrus.Debugf("Disconnected client for session %s", sessionID)
	}

	// Close database handles if they're still open
	// Note: It's safe to call Close multiple times on sqlstore.Container
	if sessionCopy.DB != nil {
		if err := sessionCopy.DB.Close(); err != nil {
			logrus.Warnf("Failed to close main DB for session %s during removal: %v", sessionID, err)
		} else {
			logrus.Debugf("Closed main DB for session %s", sessionID)
		}
	}

	if sessionCopy.KeysDB != nil {
		if err := sessionCopy.KeysDB.Close(); err != nil {
			logrus.Warnf("Failed to close keys DB for session %s during removal: %v", sessionID, err)
		} else {
			logrus.Debugf("Closed keys DB for session %s", sessionID)
		}
	}

	logrus.Infof("Session %s removed from session manager (remaining sessions: %d)", sessionID, remainingCount)
	return nil
}

// GetSession returns a specific session by ID
func (sm *SessionManager) GetSession(sessionID string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	return session, nil
}

// GetClient returns the WhatsApp client for a specific session
func (sm *SessionManager) GetClient(sessionID string) (*whatsmeow.Client, error) {
	session, err := sm.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return session.Client, nil
}

// GetDB returns the database container for a specific session
func (sm *SessionManager) GetDB(sessionID string) (*sqlstore.Container, error) {
	session, err := sm.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	return session.DB, nil
}

// GetDefaultSession returns the default session ID
func (sm *SessionManager) GetDefaultSession() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.default
}

// SetDefaultSession sets the default session
func (sm *SessionManager) SetDefaultSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[sessionID]; !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	sm.default = sessionID
	logrus.Infof("Default session set to %s", sessionID)
	return nil
}

// GetAllSessions returns all session IDs
func (sm *SessionManager) GetAllSessions() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessionIDs := make([]string, 0, len(sm.sessions))
	for id := range sm.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	return sessionIDs
}

// GetAllSessionsWithStatus returns all sessions with their connection status
func (sm *SessionManager) GetAllSessionsWithStatus() []map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]map[string]interface{}, 0, len(sm.sessions))
	for id, session := range sm.sessions {
		isConnected := false
		isLoggedIn := false
		deviceID := ""

		if session.Client != nil {
			isConnected = session.Client.IsConnected()
			isLoggedIn = session.Client.IsLoggedIn()
			if session.Client.Store != nil && session.Client.Store.ID != nil {
				deviceID = session.Client.Store.ID.String()
			}
		}

		result = append(result, map[string]interface{}{
			"session_id":   id,
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
			"device_id":    deviceID,
			"is_default":   id == sm.default,
		})
	}

	return result
}

// UpdateSession atomically updates all fields of an existing session
// All fields are updated together under mutex lock to ensure consistency
func (sm *SessionManager) UpdateSession(sessionID string, client *whatsmeow.Client, db *sqlstore.Container, keysDB *sqlstore.Container, chatStorageRepo domainChatStorage.IChatStorageRepository) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Update all session fields atomically
	session.Client = client
	session.DB = db
	session.KeysDB = keysDB
	session.ChatStorageRepo = chatStorageRepo

	logrus.Infof("Session %s updated successfully", sessionID)
	return nil
}

// SessionExists checks if a session exists
func (sm *SessionManager) SessionExists(sessionID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	_, exists := sm.sessions[sessionID]
	return exists
}

// GetSessionCount returns the number of active sessions
func (sm *SessionManager) GetSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return len(sm.sessions)
}

// GetConnectionStatus returns the connection status for a specific session
func (sm *SessionManager) GetConnectionStatus(sessionID string) (isConnected bool, isLoggedIn bool, deviceID string, err error) {
	session, err := sm.GetSession(sessionID)
	if err != nil {
		return false, false, "", err
	}

	if session.Client == nil {
		return false, false, "", nil
	}

	isConnected = session.Client.IsConnected()
	isLoggedIn = session.Client.IsLoggedIn()

	if session.Client.Store != nil && session.Client.Store.ID != nil {
		deviceID = session.Client.Store.ID.String()
	}

	return isConnected, isLoggedIn, deviceID, nil
}

// DisconnectAll disconnects all sessions
func (sm *SessionManager) DisconnectAll() {
	// Step 1: Collect sessions that need disconnecting while holding the lock
	sm.mu.Lock()

	type sessionToDisconnect struct {
		id     string
		client *whatsmeow.Client
	}

	sessionsToDisconnect := make([]sessionToDisconnect, 0, len(sm.sessions))
	for id, session := range sm.sessions {
		if session.Client != nil && session.Client.IsConnected() {
			sessionsToDisconnect = append(sessionsToDisconnect, sessionToDisconnect{
				id:     id,
				client: session.Client,
			})
		}
	}

	sm.mu.Unlock()

	// Step 2: Perform I/O operations without holding the lock
	for _, s := range sessionsToDisconnect {
		s.client.Disconnect()
		logrus.Infof("Session %s disconnected", s.id)
	}
}
