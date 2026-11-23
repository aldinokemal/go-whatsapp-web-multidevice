package signin

import (
	"sync"
	"time"
)

// defaultMaxKeys bounds the limiter's memory. Past it, stale windows are swept;
// if every window is still live, new keys are refused (fail closed).
const defaultMaxKeys = 10000

// Limiter is a per-key fixed window, in memory. Enough for one instance.
type Limiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	maxKeys   int
	lastSweep time.Time
	hits      map[string]*windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, maxKeys: defaultMaxKeys, hits: map[string]*windowCount{}}
}

// Allow records one hit for key at now and reports whether it is within the limit.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.max <= 0 {
		return false
	}
	w, ok := l.hits[key]
	if ok && now.Sub(w.start) < l.window {
		if w.n >= l.max {
			return false
		}
		w.n++
		return true
	}
	if !ok && len(l.hits) >= l.maxKeys {
		// Sweep at most once per window so a flood of new keys costs O(1) each,
		// not a full map walk per message.
		if now.Sub(l.lastSweep) >= l.window || l.lastSweep.IsZero() {
			l.lastSweep = now
			for k, v := range l.hits {
				if now.Sub(v.start) >= l.window {
					delete(l.hits, k)
				}
			}
		}
		if len(l.hits) >= l.maxKeys {
			return false
		}
	}
	l.hits[key] = &windowCount{start: now, n: 1}
	return true
}
