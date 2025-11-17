package chatstorage

import "context"

// Context keys for chat storage operations
type contextKey string

const (
	// CtxKeySessionID is the context key for storing and retrieving the session ID.
	// This key is used to pass the session ID through the call chain when storing
	// messages and chats, ensuring data is properly partitioned by WhatsApp session.
	//
	// Usage:
	//   ctx = context.WithValue(ctx, chatstorage.CtxKeySessionID, "my-session")
	//   err := repo.CreateMessage(ctx, evt)
	//
	// The repository implementations will extract the session ID from the context
	// and use it to partition data in the database. If no session ID is found in
	// the context, implementations should default to "default" for backward compatibility.
	CtxKeySessionID contextKey = "session_id"
)

// WithSessionID returns a new context with the session ID value set.
// This is a convenience function for setting the session ID in the context.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, CtxKeySessionID, sessionID)
}

// GetSessionID extracts the session ID from the context.
// Returns the session ID and a boolean indicating whether it was found.
// If not found, callers should typically default to "default".
func GetSessionID(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(CtxKeySessionID).(string)
	return sessionID, ok
}

// GetSessionIDOrDefault extracts the session ID from the context,
// returning "default" if not found or empty.
func GetSessionIDOrDefault(ctx context.Context) string {
	sessionID, ok := GetSessionID(ctx)
	if !ok || sessionID == "" {
		return "default"
	}
	return sessionID
}
