package chatstorage

import (
	"context"
	"testing"
)

func TestWithSessionID(t *testing.T) {
	ctx := context.Background()
	sessionID := "test-session"

	// Set session ID in context
	ctx = WithSessionID(ctx, sessionID)

	// Retrieve it
	retrieved, ok := GetSessionID(ctx)
	if !ok {
		t.Error("GetSessionID returned false, expected true")
	}

	if retrieved != sessionID {
		t.Errorf("GetSessionID returned %q, expected %q", retrieved, sessionID)
	}
}

func TestGetSessionID_NotSet(t *testing.T) {
	ctx := context.Background()

	// Try to retrieve without setting
	_, ok := GetSessionID(ctx)
	if ok {
		t.Error("GetSessionID returned true for empty context, expected false")
	}
}

func TestGetSessionIDOrDefault(t *testing.T) {
	tests := []struct {
		name           string
		ctx            context.Context
		expectedResult string
	}{
		{
			name:           "context with session ID",
			ctx:            WithSessionID(context.Background(), "my-session"),
			expectedResult: "my-session",
		},
		{
			name:           "context without session ID",
			ctx:            context.Background(),
			expectedResult: "default",
		},
		{
			name:           "context with empty session ID",
			ctx:            WithSessionID(context.Background(), ""),
			expectedResult: "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSessionIDOrDefault(tt.ctx)
			if result != tt.expectedResult {
				t.Errorf("GetSessionIDOrDefault() = %q, want %q", result, tt.expectedResult)
			}
		})
	}
}

func TestContextKey_TypeSafety(t *testing.T) {
	ctx := context.Background()

	// Set using WithSessionID
	ctx = WithSessionID(ctx, "test")

	// Try to retrieve with wrong type (should fail gracefully)
	wrongValue := ctx.Value(CtxKeySessionID)
	if _, ok := wrongValue.(string); !ok {
		t.Error("Context value should be a string")
	}
}

func TestContextKey_Uniqueness(t *testing.T) {
	ctx := context.Background()

	// Set session ID
	ctx = WithSessionID(ctx, "session1")

	// Set another value with a different key (simulating potential conflicts)
	type otherKey string
	ctx = context.WithValue(ctx, otherKey("session_id"), "other-value")

	// Our session ID should be unchanged
	sessionID := GetSessionIDOrDefault(ctx)
	if sessionID != "session1" {
		t.Errorf("Session ID was affected by other context value, got %q", sessionID)
	}

	// The other value should also be unchanged
	otherValue := ctx.Value(otherKey("session_id"))
	if otherValue != "other-value" {
		t.Error("Other context value was affected")
	}
}
