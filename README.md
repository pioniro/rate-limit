# Rate Limiter for Go
[![Go Reference](https://pkg.go.dev/badge/github.com/pioniro/rate-limit.svg)](https://pkg.go.dev/github.com/pioniro/rate-limit)
[![Build status](https://img.shields.io/circleci/build/github/pioniro/rate-limit?style=plastic)](https://app.circleci.com/pipelines/github/pioniro/)
[![Go Report Card](https://goreportcard.com/badge/github.com/pioniro/rate-limit)](https://goreportcard.com/report/github.com/pioniro/rate-limit)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/pioniro/rate-limit/blob/main/LICENSE)
[![Coverage](https://coveralls.io/repos/github/pioniro/rate-limit/badge.svg?branch=main)](https://coveralls.io/github/pioniro/rate-limit)

A flexible and extensible rate limiting library for Go applications, providing various rate limiting algorithms and storage backends.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Rate Limiting Policies](#rate-limiting-policies)
  - [Fixed Window](#fixed-window)
  - [Token Bucket](#token-bucket)
  - [No Limiter](#no-limiter)
- [Storage Backends](#storage-backends)
  - [In-Memory Storage](#in-memory-storage)
- [Usage Guide](#usage-guide)
  - [Rate Limiting Methods](#rate-limiting-methods)
- [API Reference](#api-reference)
- [Project Status](#project-status)
- [License](#license)

## Features

- Multiple rate limiting algorithms:
  - Fixed Window: Limits requests within a fixed time window
  - Token Bucket: Allows burst traffic while maintaining a long-term rate limit
  - No Limiter: A pass-through limiter that allows all operations (useful for testing or conditional rate limiting)
- Pluggable storage backends:
  - In-memory storage with automatic cleanup and configurable expiration
  - (More storage backends coming soon)
- Thread-safe operations
- Context-aware API with cancellation support
- Distributed rate limiting support with mutex interface

## Installation

```bash
go get github.com/pioniro/rate-limit
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/pioniro/rate-limit"
	"github.com/pioniro/rate-limit/policy"
	"github.com/pioniro/rate-limit/storage"
)

func main() {
	// Create an in-memory storage
	store := storage.NewInMemoryStorage()

	// Start the cleanup process
	ctx := context.Background()
	store.Start(ctx)
	defer store.Stop()

	// Create a fixed window rate limiter
	// Parameters: id, limit, interval, storage, mutex (optional)
	limiter, err := policy.NewFixedWindowLimiter(
		"login", // limiter ID
		10,      // limit (10 requests)
		15*time.Minute, // interval (15 minutes)
		store,   // storage backend
		nil,     // no mutex for this example
	)
	if err != nil {
		panic(err)
	}

	// Check if action is allowed
	key := "user123" // unique key for the entity being rate limited
	allowance, err := limiter.Allow(ctx, key, 1)
	if err != nil {
		panic(err)
	}

	if allowance.Allowed {
		fmt.Println("Request allowed")
		// ... execute the code
	} else {
		fmt.Printf("Rate limit exceeded. Try again after %s\n", 
			time.Until(allowance.Until))
	}

	// Alternative: Consume tokens directly
	allowed, err := limiter.Consume(ctx, key, 1)
	if err != nil {
		panic(err)
	}

	if allowed {
		fmt.Println("Request allowed")
		// ... execute the code
	} else {
		fmt.Println("Rate limit exceeded")
	}

	// Advanced: Reserve tokens
	reservation, err := limiter.Reserve(ctx, key, 1)
	if err != nil {
		panic(err)
	}

	// Check if we need to wait
	waitTime := time.Until(reservation.Until)
	if waitTime > 0 {
		fmt.Printf("Waiting for %s\n", waitTime)
		time.Sleep(waitTime)
	}

	// Now we can proceed
	fmt.Println("Request processed after reservation")
}
```

## Rate Limiting Policies

### Fixed Window

The Fixed Window algorithm counts requests in a fixed time window. When the window expires, the counter resets.

```go
limiter, err := policy.NewFixedWindowLimiter(
    "api",           // limiter ID
    100,             // limit (100 requests)
    time.Hour,       // interval (1 hour)
    store,           // storage backend
    nil,             // no mutex
)
```

### Token Bucket

The Token Bucket algorithm models a bucket that is continuously refilled with tokens at a fixed rate. Each request consumes one or more tokens from the bucket. If the bucket has enough tokens, the request is allowed; otherwise, it's denied.

```go
// Create a rate of 10 tokens per minute
rate := policy.PerMinute(10)

// Or use other predefined rates
// rate := policy.PerSecond(1)
// rate := policy.PerHour(100)
// rate := policy.PerDay(1000)

// Or create a custom rate
// rate := policy.NewRate(30 * time.Second, 5) // 5 tokens every 30 seconds

// Create a token bucket limiter with a maximum burst of 20 tokens
limiter, err := policy.NewTokenBucketLimiter(
    "api",           // limiter ID
    20,              // burst size (maximum tokens)
    rate,            // refill rate
    store,           // storage backend
)
```

The Token Bucket algorithm is ideal for:
- Handling burst traffic while maintaining a long-term rate limit
- APIs that need to allow occasional spikes in usage
- Scenarios where you want to smooth out traffic rather than enforce strict windows

### No Limiter

The No Limiter is a pass-through implementation that allows all operations without any rate limiting. This is useful for testing, development environments, or when you need to conditionally apply rate limiting.

```go
// Create a no-op rate limiter that allows all operations
limiter := policy.NewNoLimiter()

// All operations will be allowed without any limits
allowed, err := limiter.Consume(ctx, "any-key", 1) // always returns true
```

## Storage Backends

### In-Memory Storage

The in-memory storage keeps rate limiter state in memory with automatic cleanup of expired entries.

```go
// Create with default cleanup interval (5 minutes)
store := storage.NewInMemoryStorage()

// Or with custom cleanup interval
store := storage.NewInMemoryStorage().WithCleanupInterval(10 * time.Minute)

// Start the cleanup process
ctx := context.Background()
store.Start(ctx)
defer store.Stop()

// You can also set custom expiration for specific states
err := store.SetExpiration(ctx, "state-id", 30 * time.Minute)
```

## Usage Guide

### Rate Limiting Methods

The library provides three main methods for rate limiting:

1. **Consume**: The simplest method that just checks if the operation is allowed and consumes tokens if it is.
   ```go
   allowed, err := limiter.Consume(ctx, key, 1)
   if allowed {
       // Proceed with the operation
   } else {
       // Rate limit exceeded
   }
   ```

2. **Allow**: Similar to Consume but provides more information about when the rate limit will reset.
   ```go
   allowance, err := limiter.Allow(ctx, key, 1)
   if allowance.Allowed {
       // Proceed with the operation
   } else {
       // Rate limit exceeded, can retry after allowance.Until
       fmt.Printf("Try again after %s\n", time.Until(allowance.Until))
   }
   ```

3. **Reserve**: Advanced method that reserves tokens and tells you when you can proceed.
   ```go
   reservation, err := limiter.Reserve(ctx, key, 1)
   if err != nil {
       // Handle error
   }

   waitTime := time.Until(reservation.Until)
   if waitTime > 0 {
       // Wait until the reservation time
       time.Sleep(waitTime)
   }
   // Now proceed with the operation
   ```

Choose the method that best fits your use case:
- Use **Consume** for simple yes/no rate limiting
- Use **Allow** when you need to inform users when they can retry
- Use **Reserve** for advanced scenarios where you want to queue operations

## API Reference

### Core Interfaces

#### RateLimit

```go
type RateLimit interface {
    Reserve(ctx context.Context, key string, tokens int) (Reservation, error)
    Allow(ctx context.Context, key string, tokens int) (*Allowance, error)
    Consume(ctx context.Context, key string, tokens int) (bool, error)
}
```

#### Storage

```go
type Storage interface {
    Save(ctx context.Context, state State) error
    Fetch(ctx context.Context, stateId string) (*State, error)
    Delete(ctx context.Context, stateId string) error
}
```

#### Mutex

```go
type Mutex interface {
    Lock(ctx context.Context) (bool, error)
    Unlock(ctx context.Context) error
}
```

### Development

1. Fork the repository
2. Create a feature branch: `git checkout -b feature-name`
3. Commit your changes: `git commit -m 'Add some feature'`
4. Push to the branch: `git push origin feature-name`
5. Submit a pull request

### Running Tests

```bash
# Run all tests
make tests

# Run a specific test
go test -v ./policy -run TestFixedWindowLimiter
```

The test suite includes:
- Unit tests for each rate limiting policy
- Unit tests for storage backends
- Integration tests that verify the complete rate limiting workflow

## Project Status

This project is actively maintained. The current focus is on:

1. Adding more rate limiting algorithms (Sliding Window)
2. Implementing additional storage backends (Redis, Database)
3. Improving performance and scalability
4. Enhancing documentation and examples

Recent updates:
- Added Token Bucket algorithm implementation

## License

[MIT License](LICENSE)
