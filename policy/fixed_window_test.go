package policy

import (
	"context"
	"sync"
	"testing"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
	"github.com/pioniro/rate-limit/storage"
)

func TestFixedWindowLimiter_Consume(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Create a limiter with 10 tokens per minute
	limiter, err := NewFixedWindowLimiter("test", 10, time.Minute, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Consume 9 tokens
	for i := 0; i < 9; i++ {
		allowed, err := limiter.Consume(ctx, "key", 1)
		if err != nil {
			t.Fatalf("Consume failed: %v", err)
		}
		if !allowed {
			t.Fatalf("Expected token %d to be allowed", i+1)
		}
	}

	// Consume the 10th token, should be allowed
	allowed, err := limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected 10th token to be allowed")
	}

	// Consume the 11th token, should be denied
	allowed, err = limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if allowed {
		t.Fatalf("Expected 11th token to be denied")
	}

	// Check the allowance details
	allowance, err := limiter.Allow(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}
	if allowance.Allowed {
		t.Fatalf("Expected allowance to be denied")
	}

	// Verify the retry time is approximately 1 minute in the future
	expectedRetry := time.Now().Add(time.Minute)
	if allowance.Until.Before(expectedRetry.Add(-2*time.Second)) ||
		allowance.Until.After(expectedRetry.Add(2*time.Second)) {
		t.Fatalf("Expected retry time to be around %v, got %v", expectedRetry, allowance.Until)
	}
}

func TestFixedWindowLimiter_ConsumeOutsideInterval(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Use a shorter interval for testing
	testInterval := 5 * time.Second
	limiter, err := NewFixedWindowLimiter("test", 10, testInterval, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Start window by consuming 1 token
	allowed, err := limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected first token to be allowed")
	}

	// Consume 9 more tokens to reach the limit
	allowed, err = limiter.Consume(ctx, "key", 9)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected 9 tokens to be allowed")
	}

	// Mock time passing by creating a new Window with the timer set to the future
	now := time.Now()
	window := &Window{
		ID:       "test:key",
		HitCount: 10,
		Interval: testInterval,
		MaxSize:  10,
		Timer:    now.Add(-testInterval - time.Second), // Just past the interval
		mu:       sync.RWMutex{},
	}

	// Save the modified window
	err = store.Save(ctx, window.ToState())
	if err != nil {
		t.Fatalf("Failed to save window: %v", err)
	}

	// Should be able to consume 10 tokens in the new window
	allowed, err = limiter.Consume(ctx, "key", 10)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected 10 tokens to be allowed in new window")
	}
}

func TestFixedWindowLimiter_PeekConsume(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Create a limiter with 10 tokens per minute
	limiter, err := NewFixedWindowLimiter("test", 10, time.Minute, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Consume 9 tokens
	allowed, err := limiter.Consume(ctx, "key", 9)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected 9 tokens to be allowed")
	}

	// Peek by consuming 0 tokens
	for i := 0; i < 2; i++ {
		allowance, err := limiter.Allow(ctx, "key", 0)
		if err != nil {
			t.Fatalf("Allow failed: %v", err)
		}
		if !allowance.Allowed {
			t.Fatalf("Expected peek to be allowed")
		}
		if allowance.Until.After(time.Now().Add(time.Second)) {
			t.Fatalf("Expected peek to have immediate retry time")
		}
	}

	// Consume the last token
	allowed, err = limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected last token to be allowed")
	}

	// Peek again - should still be allowed but with future retry time
	allowance, err := limiter.Allow(ctx, "key", 0)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}
	if !allowance.Allowed {
		t.Fatalf("Expected peek to be allowed even when at limit")
	}

	// Try to consume one more token - should be denied
	allowed, err = limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if allowed {
		t.Fatalf("Expected consumption to be denied when at limit")
	}
}

func TestFixedWindowLimiter_InvalidToken(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()

	// Create a limiter with 10 tokens per minute
	limiter, err := NewFixedWindowLimiter("test", 10, time.Minute, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Try to reserve more tokens than the limit
	_, err = limiter.Reserve(context.Background(), "key", 11)
	if err == nil {
		t.Fatalf("Expected error when reserving more tokens than the limit")
	}
}

func TestFixedWindowLimiter_WindowReset(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Use a short interval for testing
	interval := 2 * time.Second
	limiter, err := NewFixedWindowLimiter("test", 5, interval, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Consume all tokens
	allowed, err := limiter.Consume(ctx, "key", 5)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected 5 tokens to be allowed")
	}

	// Try to consume one more - should be denied
	allowed, err = limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if allowed {
		t.Fatalf("Expected consumption to be denied when at limit")
	}

	// Mock time passing by creating a new Window with the timer set to the past
	now := time.Now()
	window := &Window{
		ID:       "test:key",
		HitCount: 5,
		Interval: interval,
		MaxSize:  5,
		Timer:    now.Add(-interval - time.Second), // Just past the interval
		mu:       sync.RWMutex{},
	}

	// Save the modified window
	err = store.Save(ctx, window.ToState())
	if err != nil {
		t.Fatalf("Failed to save window: %v", err)
	}

	// Should be able to consume again after the interval
	allowed, err = limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed after interval reset")
	}
}

func TestFixedWindowLimiter_MalformedWindowFromStorage(t *testing.T) {
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Create a malformed state
	malformedState := ratelimit.State{
		ID: "test:key",
		Data: map[string]interface{}{
			"invalid_key": "invalid_value",
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Save the malformed state
	err := store.Save(ctx, malformedState)
	if err != nil {
		t.Fatalf("Failed to save malformed state: %v", err)
	}

	// Create a limiter
	limiter, err := NewFixedWindowLimiter("test", 10, time.Minute, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Should still be able to consume
	allowed, err := limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed with malformed window")
	}
}
