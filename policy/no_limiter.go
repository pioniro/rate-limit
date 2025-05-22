// Package policy provides rate limiting policy implementations.
package policy

import (
	"context"
	"fmt"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
)

// NoLimiter implements a non-limiting rate limiter.
// This can be used in cases where an implementation requires a
// limiter, but no rate limit should be enforced. All operations
// are always allowed with no delay.
type NoLimiter struct{}

// NewNoLimiter creates a new instance of NoLimiter that implements
// the RateLimit interface but allows all operations.
func NewNoLimiter() *NoLimiter {
	return &NoLimiter{}
}

// Reserve always returns an immediate reservation with no wait time.
// This implements the Reserve method of the RateLimit interface but
// never limits any operation.
func (n *NoLimiter) Reserve(ctx context.Context, _ string, _ int) (ratelimit.Reservation, error) {
	select {
	case <-ctx.Done():
		return ratelimit.Reservation{}, fmt.Errorf("context canceled during reservation operation: %w", ctx.Err())
	default:
		// Immediately allow the request without waiting
		return ratelimit.Reservation{
			Until: time.Now(),
		}, nil
	}
}

// Allow always returns an allowance with Allowed=true.
// This implements the Allow method of the RateLimit interface but
// always allows all operations.
func (n *NoLimiter) Allow(ctx context.Context, _ string, _ int) (*ratelimit.Allowance, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context canceled during allow operation: %w", ctx.Err())
	default:
		// Always allow
		return &ratelimit.Allowance{
			Allowed: true,
			Until:   time.Now(),
		}, nil
	}
}

// Consume always returns true as there is no limit.
// This implements the Consume method of the RateLimit interface but
// always allows all operations.
func (n *NoLimiter) Consume(ctx context.Context, _ string, _ int) (bool, error) {
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("context canceled during consume operation: %w", ctx.Err())
	default:
		// Always allow consumption
		return true, nil
	}
}
