package whatsapp

import (
	"context"
	"fmt"
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

// RemoveSession removes a session from the manager
func (sm *SessionManager) RemoveSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Disconnect the client if it's connected
	if session.Client != nil && session.Client.IsConnected() {
		session.Client.Disconnect()
	}

	delete(sm.sessions, sessionID)

	// Update default session if removed
	if sm.default == sessionID {
		sm.default = ""
		// Set a new default from remaining sessions
		for id := range sm.sessions {
			sm.default = id
			logrus.Infof("Session %s set as new default session", id)
			break
		}
	}

	logrus.Infof("Session %s removed from session manager (remaining sessions: %d)", sessionID, len(sm.sessions))
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

// UpdateSession updates an existing session's client and databases
func (sm *SessionManager) UpdateSession(sessionID string, client *whatsmeow.Client, db *sqlstore.Container) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.Client = client
	session.DB = db

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
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for id, session := range sm.sessions {
		if session.Client != nil && session.Client.IsConnected() {
			session.Client.Disconnect()
			logrus.Infof("Session %s disconnected", id)
		}
	}
}
