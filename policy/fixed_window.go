package policy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
)

// Error definitions for FixedWindowLimiter.
var (
	ErrLimitTooSmall       = errors.New("cannot set the limit to 0, as that would never accept any hit")
	ErrTokensExceedLimit   = errors.New("cannot reserve more tokens than the size of the rate limiter")
	ErrFailedToAcquireLock = errors.New("failed to acquire lock")
	ErrFailedToSaveState   = errors.New("failed to save window state")
)

// Window represents a fixed time window for rate limiting.
type Window struct {
	ID       string
	HitCount int
	Interval time.Duration
	MaxSize  int
	Timer    time.Time
	mu       sync.RWMutex
}

// GetID returns the window's ID.
func (w *Window) GetID() string {
	return w.ID
}

// ToState converts a Window to a State for storage.
func (w *Window) ToState() ratelimit.State {
	w.mu.RLock()
	data := map[string]interface{}{
		"hit_count": float64(w.HitCount),
		"interval":  w.Interval.Milliseconds(),
		"max_size":  float64(w.MaxSize),
		"timer":     float64(w.Timer.UnixNano()),
	}
	expiresAt := w.Timer.Add(w.Interval)
	w.mu.RUnlock()

	return ratelimit.State{
		ID:        w.ID,
		Data:      data,
		ExpiresAt: expiresAt,
	}
}

// Add adds hits to the window, resetting if necessary.
func (w *Window) Add(hits int, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if now.Sub(w.Timer) > w.Interval {
		// reset window
		w.Timer = now
		w.HitCount = 0
	}

	w.HitCount += hits
}

// GetAvailableTokens returns the number of tokens available.
func (w *Window) GetAvailableTokens(now time.Time) int {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// if now is more than the window interval in the past, all tokens are available
	if now.Sub(w.Timer) > w.Interval {
		return w.MaxSize
	}

	return w.MaxSize - w.HitCount
}

// CalculateTimeForTokens calculates the time to wait for tokens.
func (w *Window) CalculateTimeForTokens(tokens int, now time.Time) time.Duration {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if (w.MaxSize - w.HitCount) >= tokens {
		return 0
	}

	resetTime := w.Timer.Add(w.Interval)

	if waitDuration := resetTime.Sub(now); waitDuration >= 0 {
		return waitDuration
	}

	return 0
}

// FixedWindowLimiter implements a fixed window rate limiter.
type FixedWindowLimiter struct {
	id       string
	limit    int
	interval time.Duration
	storage  ratelimit.Storage
}

// NewFixedWindowLimiter creates a new fixed window limiter.
func NewFixedWindowLimiter(
	limiterID string,
	limit int,
	interval time.Duration,
	storage ratelimit.Storage,
) (*FixedWindowLimiter, error) {
	if limit < 1 {
		return nil, ErrLimitTooSmall
	}

	return &FixedWindowLimiter{
		id:       limiterID,
		limit:    limit,
		interval: interval,
		storage:  storage,
	}, nil
}

// Reserve reserves tokens and returns a reservation.
func (f *FixedWindowLimiter) Reserve(ctx context.Context, key string, tokens int) (ratelimit.Reservation, error) {
	if tokens > f.limit {
		return ratelimit.Reservation{}, fmt.Errorf("%w: %d tokens exceeds limit of %d", ErrTokensExceedLimit, tokens, f.limit)
	}

	stateID := f.id + ":" + key

	// Try to acquire lock from storage
	locked, err := f.storage.Lock(ctx, stateID)
	if err != nil {
		return ratelimit.Reservation{}, fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !locked {
		return ratelimit.Reservation{}, ErrFailedToAcquireLock
	}
	defer func() {
		_ = f.storage.Unlock(ctx, stateID) // ignoring error on unlock
	}()

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ratelimit.Reservation{}, fmt.Errorf("context canceled during reservation operation: %w", ctx.Err())
	default:
	}

	var window *Window

	// Fetch or create window
	state, err := f.storage.Fetch(ctx, stateID)
	if err != nil || state == nil {
		// Create a new window if not found in storage
		window = &Window{
			ID:       stateID,
			HitCount: 0,
			Interval: f.interval,
			MaxSize:  f.limit,
			Timer:    time.Now(),
			mu:       sync.RWMutex{},
		}
	} else {
		// Deserialize the Window from the State
		window = &Window{
			ID:       state.ID,
			HitCount: 0,
			Interval: f.interval,
			MaxSize:  f.limit,
			Timer:    time.Now(),
			mu:       sync.RWMutex{},
		}

		// Restore data from state if available
		if state.Data != nil {
			// Get hit count (required)
			if hitCountFloat, ok := state.Data["hit_count"].(float64); ok {
				window.HitCount = int(hitCountFloat)
			}

			// Get interval (optional with fallback)
			if intervalMs, ok := state.Data["interval"].(float64); ok {
				window.Interval = time.Duration(intervalMs) * time.Millisecond
			} else {
				window.Interval = f.interval
			}

			// Get max size (optional with fallback)
			if maxSizeFloat, ok := state.Data["max_size"].(float64); ok {
				window.MaxSize = int(maxSizeFloat)
			} else {
				window.MaxSize = f.limit
			}

			// Get timer (optional with fallback)
			if timerNano, ok := state.Data["timer"].(float64); ok {
				window.Timer = time.Unix(0, int64(timerNano))
			} else {
				// Check if we need to reset the window due to expiry
				now := time.Now()
				if window.HitCount > 0 && now.Sub(window.Timer) > window.Interval {
					window.Timer = now
					window.HitCount = 0
				}
			}
		}
	}

	now := time.Now()
	availableTokens := window.GetAvailableTokens(now)

	var until time.Time

	switch {
	case tokens == 0:
		waitDuration := window.CalculateTimeForTokens(1, now)
		until = now.Add(waitDuration)
	case availableTokens >= tokens:
		window.Add(tokens, now)
		until = now
	default:
		waitDuration := window.CalculateTimeForTokens(tokens, now)
		window.Add(tokens, now)
		until = now.Add(waitDuration)
	}

	if tokens > 0 {
		// Check context again before saving
		select {
		case <-ctx.Done():
			return ratelimit.Reservation{}, fmt.Errorf("context canceled during reservation: %w", ctx.Err())
		default:
		}

		err = f.storage.Save(ctx, window.ToState())
		if err != nil {
			return ratelimit.Reservation{}, fmt.Errorf("%w: %w", ErrFailedToSaveState, err)
		}
	}

	return ratelimit.Reservation{
		Until: until,
	}, nil
}

// Allow checks if the rate limit allows the requested number of tokens.
func (f *FixedWindowLimiter) Allow(ctx context.Context, key string, tokens int) (*ratelimit.Allowance, error) {
	// Special case: when tokens is 0, we're just peeking at the current state
	// This should always be allowed regardless of available tokens
	if tokens == 0 {
		reservation, err := f.Reserve(ctx, key, tokens)
		if err != nil {
			return nil, err
		}

		return &ratelimit.Allowance{
			Allowed: true,
			Until:   reservation.Until,
		}, nil
	}

	reservation, err := f.Reserve(ctx, key, tokens)
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
func (f *FixedWindowLimiter) Consume(ctx context.Context, key string, tokens int) (bool, error) {
	allowance, err := f.Allow(ctx, key, tokens)
	if err != nil {
		return false, err
	}

	return allowance.Allowed, nil
}
