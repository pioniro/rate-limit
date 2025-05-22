package ratelimit

import (
	"context"
	"time"
)

type RateLimit interface {
	Reserve(ctx context.Context, key string, tokens int) (Reservation, error)
	Allow(ctx context.Context, key string, tokens int) (*Allowance, error)
	Consume(ctx context.Context, key string, tokens int) (bool, error)
}

type Reservation struct {
	Until time.Time
}

type Allowance struct {
	Allowed bool
	Until   time.Time
}

type Storage interface {
	Save(ctx context.Context, state State) error
	Fetch(ctx context.Context, stateID string) (*State, error)
	Delete(ctx context.Context, stateID string) error

	// Mutex-like functionality

	Lock(ctx context.Context, key string) (bool, error)
	Unlock(ctx context.Context, key string) error
}

type State struct {
	ID        string
	Data      map[string]interface{} // Store additional data
	ExpiresAt time.Time
}
