package integration

import (
	"context"
	"testing"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
	"github.com/pioniro/rate-limit/policy"
	"github.com/pioniro/rate-limit/storage"
)

func TestRateLimiterInterface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := storage.NewInMemoryStorage()

	// Test different policy implementations
	limiters := []struct {
		name    string
		limiter ratelimit.RateLimit
	}{
		{
			name: "FixedWindowLimiter",
			limiter: func() ratelimit.RateLimit {
				l, err := policy.NewFixedWindowLimiter("test-fixed", 5, time.Minute, store)
				if err != nil {
					t.Fatalf("Failed to create FixedWindowLimiter: %v", err)
				}

				return l
			}(),
		},
		{
			name: "TokenBucketLimiter",
			limiter: func() ratelimit.RateLimit {
				rate := policy.PerSecond(1)
				l, err := policy.NewTokenBucketLimiter("test-token", 5, rate, store)
				if err != nil {
					t.Fatalf("Failed to create TokenBucketLimiter: %v", err)
				}

				return l
			}(),
		},
		{
			name:    "NoLimiter",
			limiter: policy.NewNoLimiter(),
		},
	}

	for _, l := range limiters {
		l := l // Create a new variable to avoid loop variable capture
		t.Run(l.name, func(t *testing.T) {
			t.Parallel()
			limiter := l.limiter

			// Test basic functionality
			allowed, err := limiter.Consume(ctx, "test-key", 1)
			if err != nil {
				t.Fatalf("Consume failed: %v", err)
			}
			if !allowed {
				t.Fatalf("Expected consumption to be allowed")
			}

			// Test allowance
			allowance, err := limiter.Allow(ctx, "test-key", 1)
			if err != nil {
				t.Fatalf("Allow failed: %v", err)
			}
			if l.name == "NoLimiter" && !allowance.Allowed {
				t.Fatalf("Expected NoLimiter to always allow")
			}

			// Test reservation
			reservation, err := limiter.Reserve(ctx, "test-key", 1)
			if err != nil {
				t.Fatalf("Reserve failed: %v", err)
			}
			if l.name == "NoLimiter" && !reservation.Until.Before(time.Now().Add(time.Second)) {
				t.Fatalf("Expected NoLimiter to provide immediate reservation")
			}
		})
	}
}

func TestRateLimiterUsagePattern(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := storage.NewInMemoryStorage()

	// Create a rate limiter with a low limit and short interval for testing
	limiter, err := policy.NewFixedWindowLimiter("test-usage", 3, 100*time.Millisecond, store)
	if err != nil {
		t.Fatalf("Failed to create limiter: %v", err)
	}

	// Consume all available tokens
	for i := 0; i < 3; i++ {
		allowed, err := limiter.Consume(ctx, "test-key", 1)
		if err != nil {
			t.Fatalf("Consume failed: %v", err)
		}
		if !allowed {
			t.Fatalf("Expected token %d to be allowed", i+1)
		}
	}

	// Try to consume one more, should be denied
	allowed, err := limiter.Consume(ctx, "test-key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if allowed {
		t.Fatalf("Expected consumption to be denied when limit is reached")
	}

	// Check the current allowance
	allowance, err := limiter.Allow(ctx, "test-key", 1)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}
	if allowance.Allowed {
		t.Fatalf("Expected allowance to be denied when limit is reached")
	}

	// Wait for the window to reset
	time.Sleep(110 * time.Millisecond)

	// Should be able to consume again
	allowed, err = limiter.Consume(ctx, "test-key", 1)
	if err != nil {
		t.Fatalf("Consume failed after reset: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed after window reset")
	}
}

func TestTokenBucketUsagePattern(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := storage.NewInMemoryStorage()

	// Create a token bucket limiter with 5 tokens max and refill rate of 1 token per 100ms
	rate := policy.PerSecond(1) // 1 token per second for more predictable timing
	limiter, err := policy.NewTokenBucketLimiter("test-token-bucket", 5, rate, store)
	if err != nil {
		t.Fatalf("Failed to create token bucket limiter: %v", err)
	}

	// Example 1: Demonstrate burst capacity
	// Token bucket allows consuming multiple tokens at once up to the burst limit
	t.Log("Example 1: Demonstrating burst capacity")
	allowed, err := limiter.Consume(ctx, "burst-key", 3)
	if err != nil {
		t.Fatalf("Burst consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected burst consumption to be allowed")
	}
	t.Log("Successfully consumed 3 tokens at once (burst)")

	// Example 2: Demonstrate token refill
	t.Log("Example 2: Demonstrating token refill")
	// First, consume all remaining tokens
	allowed, err = limiter.Consume(ctx, "refill-key", 5)
	if err != nil {
		t.Fatalf("Initial consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected initial consumption to be allowed")
	}
	t.Log("Successfully consumed all 5 tokens")

	// Try to consume one more, should be denied
	allowed, err = limiter.Consume(ctx, "refill-key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if allowed {
		t.Fatalf("Expected consumption to be denied when bucket is empty")
	}
	t.Log("Correctly denied consumption when bucket is empty")

	// Create a state with a timer in the past to simulate token refill
	now := time.Now()
	state := ratelimit.State{
		ID: "test-token-bucket:refill-key",
		Data: map[string]interface{}{
			"tokens":     float64(0),
			"burst_size": float64(5),
			"timer":      float64(now.Add(-2 * time.Second).UnixNano()),
			"interval":   float64(time.Second.Nanoseconds()),
			"refill":     float64(1),
		},
		ExpiresAt: now.Add(time.Hour),
	}

	// Save the state to simulate time passing
	err = store.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}
	t.Log("Simulated 2 seconds passing, 2 tokens should be refilled")

	// Should be able to consume 2 tokens now
	allowed, err = limiter.Consume(ctx, "refill-key", 2)
	if err != nil {
		t.Fatalf("Consume failed after refill: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed after token refill")
	}
	t.Log("Successfully consumed 2 tokens after refill")

	// Example 3: Demonstrate reservation
	t.Log("Example 3: Demonstrating reservation")

	// Create a state with full tokens for the reservation example
	now = time.Now()
	reserveState := ratelimit.State{
		ID: "test-token-bucket:reserve-key",
		Data: map[string]interface{}{
			"tokens":     float64(5), // Start with full tokens
			"burst_size": float64(5),
			"timer":      float64(now.UnixNano()),
			"interval":   float64(time.Second.Nanoseconds()),
			"refill":     float64(1),
		},
		ExpiresAt: now.Add(time.Hour),
	}

	// Save the state to ensure we have a fresh bucket with full tokens
	err = store.Save(ctx, reserveState)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}
	t.Log("Created a fresh bucket with 5 tokens for reservation example")

	// Make an immediate reservation for 3 tokens
	reservation, err := limiter.Reserve(ctx, "reserve-key", 3)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	t.Logf("Made a reservation for 3 tokens, can be used at: %v", reservation.Until)

	// The reservation time should be immediate since we have 5 tokens available
	if reservation.Until.After(time.Now().Add(10 * time.Millisecond)) {
		t.Fatalf("Expected reservation time to be immediate, got: %v", reservation.Until)
	}

	// Consume 2 more tokens, leaving 0 tokens
	allowed, err = limiter.Consume(ctx, "reserve-key", 2)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed when 2 tokens remain")
	}
	t.Log("Successfully consumed 2 more tokens, 0 tokens remain")

	// Now reserve 2 more tokens - this should give us a future time
	reservation, err = limiter.Reserve(ctx, "reserve-key", 2)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	t.Logf("Made a reservation for 2 more tokens, can be used at: %v", reservation.Until)

	// The reservation time should be in the future
	if !reservation.Until.After(time.Now()) {
		t.Fatalf("Expected reservation time to be in the future")
	}
	t.Log("Reservation time is correctly in the future")
}
