package policy

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestNoLimiter_Consume(t *testing.T) {
	limiter := NewNoLimiter()
	ctx := context.Background()

	// Test consuming a single token
	allowed, err := limiter.Consume(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption to be allowed")
	}

	// Test consuming a large number of tokens
	allowed, err = limiter.Consume(ctx, "key", 1_000_000)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}
	if !allowed {
		t.Fatalf("Expected consumption of 1,000,000 tokens to be allowed")
	}

	// Test consuming tokens with multiple keys
	for i := 0; i < 100; i++ {
		key := "key" + string(rune(i))
		allowed, err = limiter.Consume(ctx, key, 10)
		if err != nil {
			t.Fatalf("Consume failed: %v", err)
		}
		if !allowed {
			t.Fatalf("Expected consumption to be allowed for key %s", key)
		}
	}
}

func TestNoLimiter_Reserve(t *testing.T) {
	limiter := NewNoLimiter()
	ctx := context.Background()

	// Test reserving a single token
	reservation, err := limiter.Reserve(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}

	// The reservation should be for immediate use
	if reservation.Until.After(time.Now().Add(100 * time.Millisecond)) {
		t.Fatalf("Expected reservation to be immediate, got %v", reservation.Until)
	}

	// Test reserving a large number of tokens
	reservation, err = limiter.Reserve(ctx, "key", math.MaxInt32)
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	if reservation.Until.After(time.Now().Add(100 * time.Millisecond)) {
		t.Fatalf("Expected reservation to be immediate even for large token count, got %v", reservation.Until)
	}
}

func TestNoLimiter_Allow(t *testing.T) {
	limiter := NewNoLimiter()
	ctx := context.Background()

	// Test Allow with a single token
	allowance, err := limiter.Allow(ctx, "key", 1)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}
	if !allowance.Allowed {
		t.Fatalf("Expected allowance to be allowed")
	}

	// The retry time should be immediate
	if allowance.Until.After(time.Now().Add(100 * time.Millisecond)) {
		t.Fatalf("Expected retry time to be immediate, got %v", allowance.Until)
	}

	// Test Allow with a large number of tokens
	allowance, err = limiter.Allow(ctx, "key", 1_000_000)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}
	if !allowance.Allowed {
		t.Fatalf("Expected allowance of 1,000,000 tokens to be allowed")
	}
}

func TestNoLimiter_ContextCancellation(t *testing.T) {
	limiter := NewNoLimiter()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// All operations should return an error
	_, err := limiter.Consume(ctx, "key", 1)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}

	_, err = limiter.Reserve(ctx, "key", 1)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}

	_, err = limiter.Allow(ctx, "key", 1)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}
}
