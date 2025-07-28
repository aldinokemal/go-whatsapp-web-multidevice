package whatsapp

import (
	"context"
	"sync"
	"time"

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
	w.mu.Lock()
	defer w.mu.Unlock()
	
	// Check if we can acquire from rate limiter
	if !w.limiter.Allow() {
		return false
	}
	
	// Try to acquire semaphore slot (non-blocking)
	select {
	case w.semaphore <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release releases a semaphore slot
func (w *WebhookRateLimiter) Release() {
	select {
	case <-w.semaphore:
	default:
		// Semaphore already empty, nothing to release
	}
}

// Wait waits for rate limiting with timeout
func (w *WebhookRateLimiter) Wait(timeout time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	// Try to wait for rate limiter with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	
	if err := w.limiter.Wait(ctx); err != nil {
		return false
	}
	
	// Try to acquire semaphore slot with timeout
	select {
	case w.semaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}