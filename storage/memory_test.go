package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	ratelimit "github.com/pioniro/rate-limit"
)

func TestInMemoryStorage_SaveAndFetch(t *testing.T) {
	ctx := context.Background()
	storage := NewInMemoryStorage()

	// Create a state
	state := ratelimit.State{
		ID: "test",
		Data: map[string]interface{}{
			"key1": "value1",
			"key2": 42.0,
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Save the state
	err := storage.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Fetch the state
	fetchedState, err := storage.Fetch(ctx, "test")
	if err != nil {
		t.Fatalf("Failed to fetch state: %v", err)
	}

	if fetchedState == nil {
		t.Fatalf("Expected to fetch state, got nil")
	}

	// Verify the state
	if fetchedState.ID != "test" {
		t.Fatalf("Expected ID to be 'test', got %s", fetchedState.ID)
	}

	if val, ok := fetchedState.Data["key1"].(string); !ok || val != "value1" {
		t.Fatalf("Expected key1 to be 'value1', got %v", fetchedState.Data["key1"])
	}

	if val, ok := fetchedState.Data["key2"].(float64); !ok || val != 42.0 {
		t.Fatalf("Expected key2 to be 42.0, got %v", fetchedState.Data["key2"])
	}
}

func TestInMemoryStorage_Delete(t *testing.T) {
	ctx := context.Background()
	storage := NewInMemoryStorage()

	// Create a state
	state := ratelimit.State{
		ID: "test",
		Data: map[string]interface{}{
			"key": "value",
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Save the state
	err := storage.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Delete the state
	err = storage.Delete(ctx, "test")
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	// Verify it's deleted
	fetchedState, err := storage.Fetch(ctx, "test")
	if !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("Expected ErrStateNotFound, got %v", err)
	}

	if fetchedState != nil {
		t.Fatalf("Expected state to be nil, got %v", fetchedState)
	}
}

func TestInMemoryStorage_Expiration(t *testing.T) {
	ctx := context.Background()
	storage := NewInMemoryStorage()

	// Create a state with a short expiration
	state := ratelimit.State{
		ID:        "test",
		Data:      map[string]interface{}{"key": "value"},
		ExpiresAt: time.Now().Add(10 * time.Millisecond),
	}

	// Save the state
	err := storage.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify it exists
	fetchedState, err := storage.Fetch(ctx, "test")
	if err != nil {
		t.Fatalf("Failed to fetch state: %v", err)
	}

	if fetchedState == nil {
		t.Fatalf("Expected to fetch state, got nil")
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Verify it's expired
	fetchedState, err = storage.Fetch(ctx, "test")
	if !errors.Is(err, ErrStateExpired) {
		t.Fatalf("Expected ErrStateExpired, got %v", err)
	}

	if fetchedState != nil {
		t.Fatalf("Expected state to be nil, got %v", fetchedState)
	}
}

func TestInMemoryStorage_SetExpiration(t *testing.T) {
	ctx := context.Background()
	storage := NewInMemoryStorage()

	// Create a state with long expiration
	state := ratelimit.State{
		ID:        "test",
		Data:      map[string]interface{}{"key": "value"},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	// Save the state
	err := storage.Save(ctx, state)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Set a short expiration
	err = storage.SetExpiration(ctx, "test", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to set expiration: %v", err)
	}

	// Verify it exists
	fetchedState, err := storage.Fetch(ctx, "test")
	if err != nil {
		t.Fatalf("Failed to fetch state: %v", err)
	}

	if fetchedState == nil {
		t.Fatalf("Expected to fetch state, got nil")
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Verify it's expired
	fetchedState, err = storage.Fetch(ctx, "test")
	if !errors.Is(err, ErrStateExpired) {
		t.Fatalf("Expected ErrStateExpired, got %v", err)
	}

	if fetchedState != nil {
		t.Fatalf("Expected state to be nil, got %v", fetchedState)
	}
}

func TestInMemoryStorage_Cleanup(t *testing.T) {
	ctx := context.Background()
	storage := NewInMemoryStorage()

	// Create several states with varying expiration times
	states := []struct {
		id        string
		expiresAt time.Time
	}{
		{"test1", time.Now().Add(-time.Hour)},   // Already expired
		{"test2", time.Now().Add(-time.Minute)}, // Already expired
		{"test3", time.Now().Add(time.Hour)},    // Not expired
	}

	for _, s := range states {
		state := ratelimit.State{
			ID:        s.id,
			Data:      map[string]interface{}{"key": "value"},
			ExpiresAt: s.expiresAt,
		}

		if err := storage.Save(ctx, state); err != nil {
			t.Fatalf("Failed to save state %s: %v", s.id, err)
		}
	}

	// Run cleanup
	storage.Cleanup()

	// Verify expired states are removed
	for _, s := range states {
		fetchedState, err := storage.Fetch(ctx, s.id)

		if s.expiresAt.Before(time.Now()) {
			// Should be expired
			if !errors.Is(err, ErrStateNotFound) {
				t.Fatalf("Expected ErrStateNotFound for state %s, got %v", s.id, err)
			}
			if fetchedState != nil {
				t.Fatalf("Expected state %s to be nil, got %v", s.id, fetchedState)
			}
		} else {
			// Should not be expired
			if err != nil {
				t.Fatalf("Failed to fetch state %s: %v", s.id, err)
			}
			if fetchedState == nil {
				t.Fatalf("Expected state %s to not be expired", s.id)
			}
		}
	}
}

func TestInMemoryStorage_ContextCancellation(t *testing.T) {
	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	storage := NewInMemoryStorage()
	state := ratelimit.State{
		ID:        "test",
		Data:      nil,
		ExpiresAt: time.Time{},
	}

	// All operations should return an error
	_, err := storage.Fetch(ctx, "test")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}

	err = storage.Save(ctx, state)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}

	err = storage.Delete(ctx, "test")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}

	err = storage.SetExpiration(ctx, "test", time.Hour)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Expected context.Canceled error, got %v", err)
	}
}
