package policy

import (
	"context"
	"testing"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
	"github.com/pioniro/rate-limit/storage"
)

func TestTokenBucketLimiter_Consume(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Create a limiter with 10 tokens and refill rate of 1 token per second
	rate := PerSecond(1)
	limiter, err := NewTokenBucketLimiter("test", 10, rate, store)
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

	// Verify the retry time is in the future (time to refill 1 token)
	checkTime := time.Now()
	if !allowance.Until.After(checkTime) {
		t.Fatalf("Expected retry time to be in the future, got %v", allowance.Until)
	}
}

func TestTokenBucketLimiter_TokenRefill(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Create a limiter with 5 tokens and refill rate of 1 token per second
	rate := PerSecond(1)
	limiter, err := NewTokenBucketLimiter("test", 5, rate, store)
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

	// Create a TokenBucket with the timer set to the past to simulate time passing
	// We'll manipulate the state directly to avoid token bucket creation errors
	now := time.Now()
	state := ratelimit.State{
		ID: "test:key",
		Data: map[string]interface{}{
			"tokens":     float64(0),
			"burst_size": float64(5),
			"timer":      float64(now.Add(-3 * time.Second).UnixNano()),
			"interval":   float64(time.Second.Nanoseconds()),
			"refill":     float64(1),
		},
		ExpiresAt: now.Add(time.Hour),
	}

	// Save the state directly
	err = store.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Should be able to consume 3 tokens after 3 seconds (due to refill)
	allowed, err = limiter.Consume(ctx, "key", 3)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed after tokens refilled")
	}

	// But not 2 more
	allowed, err = limiter.Consume(ctx, "key", 2)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if allowed {
		t.Fatalf("Expected consumption to be denied when no tokens available")
	}
}

func TestTokenBucketLimiter_PeekConsume(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Create a limiter with 10 tokens and refill rate of 1 token per second
	rate := PerSecond(1)
	limiter, err := NewTokenBucketLimiter("test", 10, rate, store)
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

func TestTokenBucketLimiter_InvalidToken(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()

	// Create a limiter with 10 tokens and refill rate of 1 token per second
	rate := PerSecond(1)
	limiter, err := NewTokenBucketLimiter("test", 10, rate, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Try to reserve more tokens than the burst size
	_, err = limiter.Reserve(context.Background(), "key", 11)
	if err == nil {
		t.Fatalf("Expected error when reserving more tokens than the burst size")
	}
}

func TestTokenBucketLimiter_BurstConsumption(t *testing.T) {
	t.Parallel()
	// Create a new storage
	store := storage.NewInMemoryStorage()
	ctx := context.Background()

	// Create a limiter with 10 tokens and refill rate of 1 token per 5 seconds
	rate := NewRate(5*time.Second, 1)
	limiter, err := NewTokenBucketLimiter("test", 10, rate, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Consume all tokens at once (burst consumption)
	allowed, err := limiter.Consume(ctx, "key", 10)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected burst consumption to be allowed")
	}

	// Try to consume one more token - should be denied
	allowed, err = limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if allowed {
		t.Fatalf("Expected consumption to be denied after burst")
	}

	// Check how long until we can consume again
	allowance, err := limiter.Allow(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}

	// Should be in the future (time to refill 1 token)
	checkTime := time.Now()
	if !allowance.Until.After(checkTime) {
		t.Fatalf("Expected retry time to be in the future, got %v", allowance.Until)
	}
}

func TestTokenBucketLimiter_MalformedBucketFromStorage(t *testing.T) {
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
	rate := PerSecond(1)
	limiter, err := NewTokenBucketLimiter("test", 10, rate, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Should still be able to consume
	allowed, err := limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed with malformed bucket")
	}
}

func TestRateMethods(t *testing.T) {
	t.Parallel()

	// Test various rate creation methods
	rates := []*Rate{
		PerSecond(1),
		PerMinute(5),
		PerHour(10),
		PerDay(20),
		NewRate(30*time.Second, 3),
	}

	// Verify rate calculations
	for i, rate := range rates {
		// Calculate time for tokens
		tokensTime := rate.CalculateTimeForTokens(5)
		if tokensTime <= 0 {
			t.Fatalf("Rate %d: Expected non-zero time for tokens", i)
		}

		// Calculate new tokens during interval
		duration := 10 * rate.interval
		newTokens := rate.CalculateNewTokensDuringInterval(duration)
		expected := 10 * rate.refillAmount
		if newTokens != expected {
			t.Fatalf("Rate %d: Expected %d new tokens, got %d", i, expected, newTokens)
		}

		// Calculate refill interval
		refillInterval := rate.CalculateRefillInterval(duration + rate.interval/2)
		if refillInterval != duration {
			t.Fatalf("Rate %d: Expected refill interval %v, got %v", i, duration, refillInterval)
		}
	}
}

func TestTokenBucket_GetAvailableTokens(t *testing.T) {
	t.Parallel()

	// Create a bucket with 5 tokens, refill 1 token per second
	rate := PerSecond(1)
	bucket, err := NewTokenBucket("test", 5, rate, time.Now())
	if err != nil {
		t.Fatalf("Failed to create token bucket: %v", err)
	}

	// Use 3 tokens
	bucket.SetTokens(2)

	// Check available tokens after 0 seconds (should be 2)
	available := bucket.GetAvailableTokens(time.Now())
	if available != 2 {
		t.Fatalf("Expected 2 available tokens, got %d", available)
	}

	// Check available tokens after 2 seconds (should be 4)
	now := time.Now()
	available = bucket.GetAvailableTokens(now.Add(2 * time.Second))
	if available != 4 {
		t.Fatalf("Expected 4 available tokens after 2 seconds, got %d", available)
	}

	// Check available tokens after 5 seconds (should be capped at 5)
	available = bucket.GetAvailableTokens(now.Add(5 * time.Second))
	if available != 5 {
		t.Fatalf("Expected 5 available tokens after 5 seconds (capped at burst size), got %d", available)
	}
}
