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
