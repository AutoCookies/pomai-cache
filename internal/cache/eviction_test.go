package cache

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func makeValue(n int, b byte) []byte {
	return bytes.Repeat([]byte{b}, n)
}

func TestEvictionKeepsUnderCapacity(t *testing.T) {
	// small capacity to force eviction
	const capacity = 200
	s := NewStoreWithOptions(8, int64(capacity))

	// Insert 20 items of size 20 => would be 400 bytes without eviction
	itemSize := 20
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key-%02d", i)
		s.Put(key, makeValue(itemSize, byte('a'+(i%26))), 0)
	}

	stats := s.Stats()
	if stats.Bytes > int64(capacity) {
		t.Fatalf("bytes exceed capacity after evictions: got %d, cap %d", stats.Bytes, capacity)
	}
	if stats.Evictions == 0 {
		t.Fatalf("expected evictions to have occurred, got 0")
	}
	if stats.Items == 0 {
		t.Fatalf("expected some items to remain after eviction")
	}
	t.Logf("after inserts: bytes=%d items=%d evictions=%d", stats.Bytes, stats.Items, stats.Evictions)
}

func TestEvictionPrefersRecentlyUsed(t *testing.T) {
	// Make sampling deterministic for the test to avoid probabilistic flakiness.
	// Choose a fixed seed so the sampling sequence is reproducible.
	rand.Seed(42)

	// This test verifies (approximately) that recently accessed keys are less likely to be evicted.
	// Because eviction is approximate (sampling-based), the result is probabilistic; we assert a
	// weak guarantee: at least one of the recently touched keys survives after forced evictions.
	const capacity = 200
	s := NewStoreWithOptions(16, int64(capacity))

	// insert 10 items of size 20 => exactly fills capacity
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("base-%02d", i)
		s.Put(key, makeValue(20, byte('A'+(i%26))), 0)
	}

	// touch some keys to mark them recently used
	recentKeys := []string{"base-00", "base-01", "base-02"}
	for _, k := range recentKeys {
		if _, ok := s.Get(k); !ok {
			t.Fatalf("expected key %s present right after put", k)
		}
		// small sleep to create distinct lastAccess timestamps
		time.Sleep(2 * time.Millisecond)
	}

	// now insert additional large items to force eviction
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("big-%02d", i)
		s.Put(key, makeValue(80, byte('x'+(i%10))), 0)
	}

	stats := s.Stats()
	t.Logf("after forcing eviction: bytes=%d items=%d evictions=%d", stats.Bytes, stats.Items, stats.Evictions)

	// Check that at least one of the recently accessed keys still exists
	found := 0
	for _, k := range recentKeys {
		if _, ok := s.Get(k); ok {
			found++
		}
	}
	if found == 0 {
		t.Fatalf("expected at least one recent key to remain after eviction, but none were found")
	}
	t.Logf("recent keys remaining: %d/%d", found, len(recentKeys))
}
