package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
)

// Error definitions for memory storage.
var (
	ErrStateNotFound = errors.New("state not found")
	ErrStateExpired  = errors.New("state expired")
)

// DefaultCleanupInterval defines how often expired entries are removed.
const DefaultCleanupInterval = 5 * time.Minute

// Bucket represents a storage bucket.
type Bucket struct {
	State ratelimit.State
}

// InMemoryStorage implements in-memory storage for rate limiting.
type InMemoryStorage struct {
	buckets         map[string]Bucket
	mu              sync.RWMutex
	locks           map[string]*sync.Mutex
	locksMu         sync.Mutex
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
}

// NewInMemoryStorage creates a new in-memory storage.
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		buckets:         make(map[string]Bucket),
		mu:              sync.RWMutex{},
		locks:           make(map[string]*sync.Mutex),
		locksMu:         sync.Mutex{},
		cleanupInterval: DefaultCleanupInterval,
		stopCleanup:     make(chan struct{}),
	}
}

// WithCleanupInterval sets a custom cleanup interval.
func (s *InMemoryStorage) WithCleanupInterval(d time.Duration) *InMemoryStorage {
	s.cleanupInterval = d

	return s
}

// Start begins periodic cleanup of expired entries.
func (s *InMemoryStorage) Start(ctx context.Context) {
	go s.startCleanupCycle(ctx)
}

// Stop halts the periodic cleanup.
func (s *InMemoryStorage) Stop() {
	close(s.stopCleanup)
}

// Save stores a state in memory.
func (s *InMemoryStorage) Save(ctx context.Context, state ratelimit.State) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("context canceled during save operation: %w", ctx.Err())
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// If expiration is not set, default to 1 hour
	if state.ExpiresAt.IsZero() {
		state.ExpiresAt = time.Now().Add(time.Hour)
	}

	s.buckets[state.ID] = Bucket{
		State: state,
	}

	return nil
}

// Fetch retrieves a state from memory, returning nil if expired.
func (s *InMemoryStorage) Fetch(ctx context.Context, stateID string) (*ratelimit.State, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context canceled during fetch operation: %w", ctx.Err())
	default:
	}

	s.mu.RLock()
	bucket, exists := s.buckets[stateID]
	if !exists {
		s.mu.RUnlock()

		return nil, ErrStateNotFound
	}

	// Check if the state has expired
	now := time.Now()
	if now.After(bucket.State.ExpiresAt) {
		s.mu.RUnlock()
		// Schedule cleanup but don't block the caller
		go s.removeExpired(stateID, now)

		return nil, ErrStateExpired
	}

	// Make a copy to avoid race conditions
	stateCopy := bucket.State
	s.mu.RUnlock()

	return &stateCopy, nil
}

// Delete removes a state from memory.
func (s *InMemoryStorage) Delete(ctx context.Context, stateID string) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("context canceled during delete operation: %w", ctx.Err())
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.buckets, stateID)

	return nil
}

// SetExpiration sets the expiration time for a state.
func (s *InMemoryStorage) SetExpiration(ctx context.Context, stateID string, duration time.Duration) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("context canceled during set expiration operation: %w", ctx.Err())
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	bucket, exists := s.buckets[stateID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrStateNotFound, stateID)
	}

	bucket.State.ExpiresAt = time.Now().Add(duration)
	s.buckets[stateID] = bucket // Update the map with the modified bucket

	return nil
}

// removeExpired safely removes an expired state.
func (s *InMemoryStorage) removeExpired(stateID string, checkTime time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check it still exists and is expired
	if bucket, ok := s.buckets[stateID]; ok && checkTime.After(bucket.State.ExpiresAt) {
		delete(s.buckets, stateID)
	}
}

// Cleanup removes all expired states.
func (s *InMemoryStorage) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, bucket := range s.buckets {
		if now.After(bucket.State.ExpiresAt) {
			delete(s.buckets, id)
		}
	}
}

// startCleanupCycle starts a periodic cleanup process.
func (s *InMemoryStorage) startCleanupCycle(ctx context.Context) {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.Cleanup()
		case <-s.stopCleanup:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Lock acquires a lock for the given key.
func (s *InMemoryStorage) Lock(ctx context.Context, key string) (bool, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("context canceled during lock operation: %w", ctx.Err())
	default:
	}

	s.locksMu.Lock()
	mutex, exists := s.locks[key]
	if !exists {
		mutex = &sync.Mutex{}
		s.locks[key] = mutex
	}
	s.locksMu.Unlock()

	// Try to acquire the lock
	mutex.Lock()
	return true, nil
}

// Unlock releases a lock for the given key.
func (s *InMemoryStorage) Unlock(ctx context.Context, key string) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("context canceled during unlock operation: %w", ctx.Err())
	default:
	}

	s.locksMu.Lock()
	mutex, exists := s.locks[key]
	s.locksMu.Unlock()

	if !exists {
		return fmt.Errorf("tried to unlock a non-existent lock for key: %s", key)
	}

	mutex.Unlock()
	return nil
}
