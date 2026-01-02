// File: internal/engine/concurrency/singleflight.go
package concurrency

import (
	"context"
	"time"

	"golang.org/x/sync/singleflight"
)

// Manager handles request coalescing (Thundering Herd protection)
type Manager struct {
	g singleflight.Group
}

// NewManager creates singleflight manager
func NewManager() *Manager {
	return &Manager{}
}

// LoadResult represents the result of a load operation
type LoadResult struct {
	Data []byte
	TTL  time.Duration
}

// Do executes fn once for duplicate calls
//
// Usage:
//
//	data, found, err := manager.Do(ctx, "key123", func(ctx context.Context) ([]byte, time.Duration, error) {
//	    // Expensive operation (DB query, API call, etc.)
//	    return fetchFromDB(ctx, "key123")
//	})
func (m *Manager) Do(
	ctx context.Context,
	key string,
	fn func(context.Context) ([]byte, time.Duration, error),
) ([]byte, bool, error) {
	if key == "" {
		return nil, false, nil
	}

	// Wrap the function to match singleflight signature
	resCh := m.g.DoChan(key, func() (interface{}, error) {
		// Call the actual loader function
		data, ttl, err := fn(ctx)
		if err != nil {
			return nil, err
		}

		// Wrap result in struct
		return &LoadResult{
			Data: data,
			TTL:  ttl,
		}, nil
	})

	select {
	case res := <-resCh:
		if res.Err != nil {
			return nil, false, res.Err
		}

		// Extract result
		if result, ok := res.Val.(*LoadResult); ok {
			return result.Data, true, nil
		}

		return nil, false, nil

	case <-ctx.Done():
		// Context cancelled/timeout
		return nil, false, ctx.Err()
	}
}

// DoWithCallback executes fn once and calls onResult for each waiter
// Useful when you want to cache the result yourself
func (m *Manager) DoWithCallback(
	ctx context.Context,
	key string,
	fn func(context.Context) ([]byte, time.Duration, error),
	onResult func(data []byte, ttl time.Duration),
) ([]byte, bool, error) {
	data, found, err := m.Do(ctx, key, fn)

	if err == nil && found && onResult != nil {
		// Get TTL from shared state if needed
		// For now, call with zero TTL
		onResult(data, 0)
	}

	return data, found, err
}

// Forget removes key from singleflight cache
// Call this after storing the result to allow future requests to execute
func (m *Manager) Forget(key string) {
	m.g.Forget(key)
}

// Stats returns singleflight statistics (if available)
// Note: golang.org/x/sync/singleflight doesn't expose stats
// This is a placeholder for custom metrics
type Stats struct {
	ActiveCalls    int
	TotalCalls     int
	CoalescedCalls int
}

// GetStats returns current statistics (placeholder)
func (m *Manager) GetStats() Stats {
	// Placeholder - actual implementation would need custom tracking
	return Stats{}
}
