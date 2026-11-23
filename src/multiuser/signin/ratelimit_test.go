// src/multiuser/signin/ratelimit_test.go
package signin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimiterWindow(t *testing.T) {
	l := NewLimiter(3, time.Hour)
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		require.True(t, l.Allow("+27825550100", now))
	}
	require.False(t, l.Allow("+27825550100", now))
	require.True(t, l.Allow("+27825550101", now), "per key")
	require.True(t, l.Allow("+27825550100", now.Add(61*time.Minute)), "window resets")
}

func TestLimiterBoundsMemory(t *testing.T) {
	l := NewLimiter(1, time.Hour)
	l.maxKeys = 2
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	require.True(t, l.Allow("a", now))
	require.True(t, l.Allow("b", now))
	require.False(t, l.Allow("c", now), "full of live windows: a new key is refused, never unbounded growth")
	require.True(t, l.Allow("c", now.Add(time.Hour)), "stale windows are swept to make room")
	require.LessOrEqual(t, len(l.hits), 2)
}

func TestLimiterZeroMaxRefuses(t *testing.T) {
	require.False(t, NewLimiter(0, time.Hour).Allow("a", time.Now()))
}
