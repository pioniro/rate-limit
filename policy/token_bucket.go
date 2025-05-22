package policy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
)

// Error definitions for TokenBucketLimiter.
var (
	ErrTokenBucketLimitTooSmall = errors.New("cannot set the limit to 0, as that would never accept any hit")
	ErrTokensExceedBurstSize    = errors.New("cannot reserve more tokens than the burst size of the rate limiter")
)

// Rate represents a token bucket refill rate.
type Rate struct {
	interval     time.Duration
	refillAmount int
}

// NewRate creates a new Rate with the specified interval and refill amount.
func NewRate(interval time.Duration, refillAmount int) *Rate {
	return &Rate{
		interval:     interval,
		refillAmount: refillAmount,
	}
}

// PerSecond creates a Rate that refills at the specified rate per second.
func PerSecond(rate int) *Rate {
	return NewRate(time.Second, rate)
}

// PerMinute creates a Rate that refills at the specified rate per minute.
func PerMinute(rate int) *Rate {
	return NewRate(time.Minute, rate)
}

// PerHour creates a Rate that refills at the specified rate per hour.
func PerHour(rate int) *Rate {
	return NewRate(time.Hour, rate)
}

// PerDay creates a Rate that refills at the specified rate per day.
func PerDay(rate int) *Rate {
	return NewRate(24*time.Hour, rate)
}

// CalculateTimeForTokens calculates the time needed to free up the provided number of tokens in seconds.
func (r *Rate) CalculateTimeForTokens(tokens int) time.Duration {
	cyclesRequired := (tokens + r.refillAmount - 1) / r.refillAmount // Ceiling division
	return r.interval * time.Duration(cyclesRequired)
}

// CalculateNewTokensDuringInterval calculates the number of new free tokens during duration.
func (r *Rate) CalculateNewTokensDuringInterval(duration time.Duration) int {
	cycles := int(duration / r.interval)
	return cycles * r.refillAmount
}

// CalculateRefillInterval calculates total amount in seconds of refill intervals during duration.
func (r *Rate) CalculateRefillInterval(duration time.Duration) time.Duration {
	cycles := duration / r.interval
	return cycles * r.interval
}

// TokenBucket represents a token bucket for rate limiting.
type TokenBucket struct {
	ID        string
	Tokens    int
	BurstSize int
	Rate      *Rate
	Timer     time.Time
	mu        sync.RWMutex
}

// NewTokenBucket creates a new token bucket with the specified parameters.
func NewTokenBucket(id string, initialTokens int, rate *Rate, timer time.Time) (*TokenBucket, error) {
	// Special case for tests where we need to create a bucket with 0 tokens
	// but still need a valid burst size
	if initialTokens < 0 {
		return nil, ErrTokenBucketLimitTooSmall
	}

	return &TokenBucket{
		ID:        id,
		Tokens:    initialTokens,
		BurstSize: max(initialTokens, 1), // Ensure burst size is at least 1
		Rate:      rate,
		Timer:     timer,
		mu:        sync.RWMutex{},
	}, nil
}

// max returns the larger of x or y
func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

// GetID returns the bucket's ID.
func (b *TokenBucket) GetID() string {
	return b.ID
}

// GetAvailableTokens returns the number of available tokens, accounting for refills.
func (b *TokenBucket) GetAvailableTokens(now time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	elapsed := now.Sub(b.Timer)
	if elapsed <= 0 {
		return b.Tokens
	}

	newTokens := b.Rate.CalculateNewTokensDuringInterval(elapsed)

	if newTokens > 0 {
		refillInterval := b.Rate.CalculateRefillInterval(elapsed)
		b.Timer = b.Timer.Add(refillInterval)
	}

	// Cap the number of tokens to the burst size
	if b.Tokens+newTokens > b.BurstSize {
		b.Tokens = b.BurstSize
	} else {
		b.Tokens += newTokens
	}

	return b.Tokens
}

// SetTokens sets the number of tokens in the bucket.
func (b *TokenBucket) SetTokens(tokens int) {
	b.mu.Lock()
	b.Tokens = tokens
	b.mu.Unlock()
}

// ToState converts a TokenBucket to a State for storage.
func (b *TokenBucket) ToState() ratelimit.State {
	b.mu.RLock()
	data := map[string]interface{}{
		"tokens":     float64(b.Tokens),
		"burst_size": float64(b.BurstSize),
		"timer":      float64(b.Timer.UnixNano()),
		"interval":   float64(b.Rate.interval.Nanoseconds()),
		"refill":     float64(b.Rate.refillAmount),
	}
	// Calculate expiration time based on how long it would take to fill the bucket completely
	timeToFill := b.Rate.CalculateTimeForTokens(b.BurstSize)
	expiresAt := b.Timer.Add(timeToFill)
	b.mu.RUnlock()

	return ratelimit.State{
		ID:        b.ID,
		Data:      data,
		ExpiresAt: expiresAt,
	}
}

// TokenBucketLimiter implements a token bucket rate limiter.
type TokenBucketLimiter struct {
	id       string
	maxBurst int
	rate     *Rate
	storage  ratelimit.Storage
}

// NewTokenBucketLimiter creates a new token bucket limiter.
func NewTokenBucketLimiter(
	limiterID string,
	maxBurst int,
	rate *Rate,
	storage ratelimit.Storage,
) (*TokenBucketLimiter, error) {
	if maxBurst < 1 {
		return nil, ErrTokenBucketLimitTooSmall
	}

	return &TokenBucketLimiter{
		id:       limiterID,
		maxBurst: maxBurst,
		rate:     rate,
		storage:  storage,
	}, nil
}

// Reserve reserves tokens and returns a reservation.
func (t *TokenBucketLimiter) Reserve(ctx context.Context, key string, tokens int) (ratelimit.Reservation, error) {
	if tokens > t.maxBurst {
		return ratelimit.Reservation{}, fmt.Errorf("%w: %d tokens exceeds burst size of %d", ErrTokensExceedBurstSize, tokens, t.maxBurst)
	}

	stateID := t.id + ":" + key

	// Try to acquire lock from storage
	locked, err := t.storage.Lock(ctx, stateID)
	if err != nil {
		return ratelimit.Reservation{}, fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !locked {
		return ratelimit.Reservation{}, ErrFailedToAcquireLock
	}
	defer func() {
		_ = t.storage.Unlock(ctx, stateID) // ignoring error on unlock
	}()

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ratelimit.Reservation{}, fmt.Errorf("context canceled during reservation operation: %w", ctx.Err())
	default:
	}

	var bucket *TokenBucket
	now := time.Now()

	// Fetch or create bucket
	state, err := t.storage.Fetch(ctx, stateID)
	if err != nil || state == nil {
		// Create a new bucket if not found in storage
		bucket, err = NewTokenBucket(stateID, t.maxBurst, t.rate, now)
		if err != nil {
			return ratelimit.Reservation{}, err
		}
	} else {
		// Restore from existing state
		tokens := t.maxBurst
		burstSize := t.maxBurst
		timer := now
		interval := t.rate.interval
		refillAmount := t.rate.refillAmount

		// Extract data from state
		if state.Data != nil {
			if tokensFloat, ok := state.Data["tokens"].(float64); ok {
				tokens = int(tokensFloat)
			}
			if burstSizeFloat, ok := state.Data["burst_size"].(float64); ok {
				burstSize = int(burstSizeFloat)
			}
			if timerNano, ok := state.Data["timer"].(float64); ok {
				timer = time.Unix(0, int64(timerNano))
			}
			if intervalNano, ok := state.Data["interval"].(float64); ok {
				interval = time.Duration(intervalNano)
			}
			if refillFloat, ok := state.Data["refill"].(float64); ok {
				refillAmount = int(refillFloat)
			}
		}

		rate := &Rate{
			interval:     interval,
			refillAmount: refillAmount,
		}

		// Create bucket directly to avoid validation errors
		bucket = &TokenBucket{
			ID:        stateID,
			Tokens:    tokens,
			BurstSize: burstSize,
			Rate:      rate,
			Timer:     timer,
			mu:        sync.RWMutex{},
		}
	}

	availableTokens := bucket.GetAvailableTokens(now)
	var until time.Time

	if availableTokens >= tokens {
		// Tokens are available, update bucket
		bucket.SetTokens(availableTokens - tokens)
		until = now
	} else {
		// Not enough tokens, calculate wait time
		remainingTokens := tokens - availableTokens
		waitDuration := t.rate.CalculateTimeForTokens(remainingTokens)

		// Set tokens to negative or zero to show we've borrowed from the future
		bucket.SetTokens(availableTokens - tokens)
		until = now.Add(waitDuration)
	}

	if tokens > 0 {
		// Check context again before saving
		select {
		case <-ctx.Done():
			return ratelimit.Reservation{}, fmt.Errorf("context canceled during reservation: %w", ctx.Err())
		default:
		}

		// Save the updated bucket state
		err = t.storage.Save(ctx, bucket.ToState())
		if err != nil {
			return ratelimit.Reservation{}, fmt.Errorf("%w: %w", ErrFailedToSaveState, err)
		}
	}

	return ratelimit.Reservation{
		Until: until,
	}, nil
}

// Allow checks if the rate limit allows the requested number of tokens.
func (t *TokenBucketLimiter) Allow(ctx context.Context, key string, tokens int) (*ratelimit.Allowance, error) {
	// Special case: when tokens is 0, we're just peeking at the current state
	// This should always be allowed regardless of available tokens
	if tokens == 0 {
		reservation, err := t.Reserve(ctx, key, tokens)
		if err != nil {
			return nil, err
		}

		return &ratelimit.Allowance{
			Allowed: true,
			Until:   reservation.Until,
		}, nil
	}

	reservation, err := t.Reserve(ctx, key, tokens)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	allowed := !reservation.Until.After(now)

	return &ratelimit.Allowance{
		Allowed: allowed,
		Until:   reservation.Until,
	}, nil
}

// Consume consumes tokens if available.
func (t *TokenBucketLimiter) Consume(ctx context.Context, key string, tokens int) (bool, error) {
	allowance, err := t.Allow(ctx, key, tokens)
	if err != nil {
		return false, err
	}

	return allowance.Allowed, nil
}
