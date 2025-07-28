package whatsapp

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

// WebhookRateLimiter provides rate limiting for webhook goroutines
type WebhookRateLimiter struct {
	limiter *rate.Limiter
	semaphore chan struct{}
	mu sync.Mutex
}

var (
	webhookLimiter *WebhookRateLimiter
	limiterOnce sync.Once
)

// getWebhookLimiter returns a singleton webhook rate limiter
func getWebhookLimiter() *WebhookRateLimiter {
	limiterOnce.Do(func() {
		// Allow 20 webhook requests per second with burst of 40
		webhookLimiter = &WebhookRateLimiter{
			limiter: rate.NewLimiter(rate.Limit(20), 40),
			// Limit concurrent goroutines to 100
			semaphore: make(chan struct{}, 100),
		}
	})
	return webhookLimiter
}

// Acquire attempts to acquire a slot for webhook processing
func (w *WebhookRateLimiter) Acquire() bool {
	// Try to acquire semaphore slot first (non-blocking)
	select {
	case w.semaphore <- struct{}{}:
		// Semaphore acquired, now check rate limiter with minimal lock time
		w.mu.Lock()
		allowed := w.limiter.Allow()
		w.mu.Unlock()
		
		if !allowed {
			// Rate limiter denied, release semaphore and return false
			select {
			case <-w.semaphore:
			default:
				// Semaphore already released, should not happen
			}
			return false
		}
		// Both semaphore and rate limit token acquired successfully
		return true
	default:
		// Semaphore full, no rate limit token consumed
		return false
	}
}

// Release releases a semaphore slot
func (w *WebhookRateLimiter) Release() {
	select {
	case <-w.semaphore:
		// Successfully released semaphore slot
	default:
		// Log potential double-release or programming error for debugging
		logrus.Warn("Attempted to release webhook semaphore slot, but semaphore is already empty - possible double release")
	}
}

// Wait waits for rate limiting with timeout
func (w *WebhookRateLimiter) Wait(timeout time.Duration) bool {
	// Create context for timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	// Try to acquire semaphore slot first (non-blocking check)
	select {
	case w.semaphore <- struct{}{}:
		// Semaphore acquired, now wait for rate limiter token with minimal lock time
		w.mu.Lock()
		err := w.limiter.Wait(ctx)
		w.mu.Unlock()
		
		if err != nil {
			// Rate limiter failed, release semaphore and return false
			select {
			case <-w.semaphore:
			default:
				// Semaphore already released, should not happen
			}
			return false
		}
		// Both semaphore and rate limit token acquired successfully
		return true
	default:
		// Semaphore full, no rate limit token consumed
		return false
	}
}