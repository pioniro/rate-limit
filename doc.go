// Package ratelimit provides a flexible and extensible rate limiting library for Go applications.
//
// This package offers multiple rate limiting algorithms and storage backends to help
// control the rate of operations in your applications. It's designed to be thread-safe,
// context-aware, and suitable for both single-instance and distributed applications.
//
// # Features
//
// - Multiple rate limiting algorithms:
//   - Fixed Window: Limits requests within a fixed time window
//   - Token Bucket: Allows burst traffic while maintaining a long-term rate limit
//   - No Limiter: A pass-through limiter that allows all operations (useful for testing)
//
// - Pluggable storage backends:
//   - In-memory storage with automatic cleanup and configurable expiration
//   - (More storage backends coming soon)
//
// - Thread-safe operations
// - Context-aware API with cancellation support
// - Distributed rate limiting support with mutex interface
//
// # Basic Usage
//
// Here's a simple example using a Fixed Window limiter:
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"time"
//
//		"github.com/pioniro/rate-limit"
//		"github.com/pioniro/rate-limit/policy"
//		"github.com/pioniro/rate-limit/storage"
//	)
//
//	func main() {
//		// Create an in-memory storage
//		store := storage.NewInMemoryStorage()
//
//		// Start the cleanup process
//		ctx := context.Background()
//		store.Start(ctx)
//		defer store.Stop()
//
//		// Create a fixed window rate limiter
//		limiter, err := policy.NewFixedWindowLimiter(
//			"login",        // limiter ID
//			10,             // limit (10 requests)
//			15*time.Minute, // interval (15 minutes)
//			store,          // storage backend
//		)
//		if err != nil {
//			panic(err)
//		}
//
//		// Check if action is allowed
//		key := "user123" // unique key for the entity being rate limited
//		allowance, err := limiter.Allow(ctx, key, 1)
//		if err != nil {
//			panic(err)
//		}
//
//		if allowance.Allowed {
//			fmt.Println("Request allowed")
//		} else {
//			fmt.Printf("Rate limit exceeded. Try again after %s\n",
//				time.Until(allowance.Until))
//		}
//	}
//
// # Token Bucket Example
//
// The Token Bucket algorithm allows for burst traffic while maintaining a long-term rate:
//
//	// Create a token bucket limiter with 100 tokens that refills at 10 tokens per second
//	limiter, err := policy.NewTokenBucketLimiter(
//		"api",                // limiter ID
//		100,                  // burst size (max tokens)
//		policy.PerSecond(10), // refill rate (10 tokens per second)
//		store,                // storage backend
//	)
//	if err != nil {
//		panic(err)
//	}
//
//	// Consume 5 tokens
//	allowed, err := limiter.Consume(ctx, "client1", 5)
//	if err != nil {
//		panic(err)
//	}
//
//	if allowed {
//		// Process the request
//	} else {
//		// Rate limit exceeded
//	}
//
// # No Limiter Example
//
// The NoLimiter is a pass-through limiter that allows all operations, useful for testing or conditional rate limiting:
//
//	// Create a no-op limiter that allows all operations
//	limiter := policy.NewNoLimiter()
//
//	// This will always return true
//	allowed, err := limiter.Consume(ctx, "any-key", 999999)
//	if err != nil {
//		panic(err)
//	}
//
//	// allowed will always be true
//	fmt.Println("Allowed:", allowed)
//
// # Advanced Usage: Reservation
//
// For more control, you can use the Reserve method to get a reservation:
//
//	reservation, err := limiter.Reserve(ctx, "client1", 10)
//	if err != nil {
//		panic(err)
//	}
//
//	// Check if tokens are immediately available
//	if time.Now().After(reservation.Until) {
//		// Process immediately
//	} else {
//		// Wait until tokens are available
//		time.Sleep(time.Until(reservation.Until))
//		// Now process the request
//	}
//
// # Distributed Rate Limiting
//
// The package supports distributed rate limiting through the Storage interface.
// The built-in InMemoryStorage provides mutex-like functionality for single-instance
// applications, but you can implement your own Storage backend for distributed scenarios.
//
// # Thread Safety
//
// All operations in this package are thread-safe and can be used concurrently.
//
// # Context Support
//
// All methods accept a context.Context parameter, allowing for cancellation and timeouts.
package ratelimit
