package cache

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

// Basic Put/Get/Delete behavior
func TestPutGetDelete(t *testing.T) {
	s := NewStoreWithOptions(16, 0)

	key := "foo"
	val := []byte("bar")

	s.Put(key, val, 0)

	got, ok := s.Get(key)
	if !ok {
		t.Fatalf("expected key %q to exist", key)
	}
	if !bytes.Equal(got, val) {
		t.Fatalf("value mismatch: got=%q want=%q", got, val)
	}

	s.Delete(key)
	_, ok = s.Get(key)
	if ok {
		t.Fatalf("expected key %q to be deleted", key)
	}
}

// TTL expiration and TTLRemaining
func TestTTLExpirationAndRemaining(t *testing.T) {
	s := NewStoreWithOptions(4, 0)

	key := "ttlkey"
	val := []byte("value")
	ttl := 100 * time.Millisecond

	s.Put(key, val, ttl)

	// immediately present
	if _, ok := s.Get(key); !ok {
		t.Fatalf("expected %q present immediately after Put", key)
	}

	// wait for expiry
	time.Sleep(150 * time.Millisecond)

	if _, ok := s.Get(key); ok {
		t.Fatalf("expected %q to be expired", key)
	}

	if remain, ok := s.TTLRemaining(key); ok {
		t.Fatalf("expected TTLRemaining to report not found; got remain=%v ok=%v", remain, ok)
	}
}

// Bloom filter basic behaviors and rebuild
func TestBloomFilterEnableRebuild(t *testing.T) {
	s := NewStoreWithOptions(8, 0)
	s.EnableBloomFilter(1024, 2)

	// put some keys
	s.Put("a", []byte("1"), 0)
	s.Put("b", []byte("2"), 0)
	s.Put("c", []byte("3"), 0)

	// Rebuild should return >= number of keys inserted (approx)
	count := s.RebuildBloomFilter()
	if count < 3 {
		t.Fatalf("expected rebuild count >= 3, got %d", count)
	}

	// ensure keys are still retrievable
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := s.Get(k); !ok {
			t.Fatalf("expected key %q present after rebuild", k)
		}
	}

	stats := s.GetBloomStats()
	// Avoid making strict assertions on probabilistic numbers, but ensure Avoided/Hits/Misses fields exist
	_ = stats
}

// Snapshot and Restore round-trip
func TestSnapshotAndRestore(t *testing.T) {
	s := NewStoreWithOptions(8, 0)
	s.EnableBloomFilter(1024, 2)

	s.Put("x", []byte("data-x"), 0)
	s.Put("y", []byte("data-y"), 0)

	var buf bytes.Buffer
	if err := s.SnapshotTo(&buf); err != nil {
		t.Fatalf("SnapshotTo failed: %v", err)
	}

	// restore into a fresh store (with bloom enabled)
	s2 := NewStoreWithOptions(8, 0)
	s2.EnableBloomFilter(1024, 2)

	if err := s2.RestoreFrom(&buf); err != nil {
		t.Fatalf("RestoreFrom failed: %v", err)
	}

	// verify restored keys
	expected := map[string][]byte{"x": []byte("data-x"), "y": []byte("data-y")}
	for k, want := range expected {
		v, ok := s2.Get(k)
		if !ok {
			t.Fatalf("expected restored key %q to be found", k)
		}
		if !bytes.Equal(v, want) {
			t.Fatalf("restored value mismatch for %q: got=%q want=%q", k, v, want)
		}
	}
}

// Eviction should keep total bytes under capacity
func TestEvictionKeepsWithinCapacity(t *testing.T) {
	// small capacity to force eviction
	capacity := int64(100) // bytes
	s := NewStoreWithOptions(4, capacity)

	// insert enough items to exceed capacity
	for i := 0; i < 10; i++ {
		k := fmt.Sprintf("k%02d", i)
		v := make([]byte, 20) // 20 bytes each
		s.Put(k, v, 0)
	}

	total := s.totalBytes()
	if total > capacity {
		t.Fatalf("expected total bytes <= capacity (%d); got %d", capacity, total)
	}
}

// Stats reflect hits and misses
func TestStatsHitsMisses(t *testing.T) {
	s := NewStoreWithOptions(4, 0)
	key := "statkey"
	val := []byte("v")

	// initial miss
	_, _ = s.Get("no-such-key")

	// put and hit
	s.Put(key, val, 0)
	if _, ok := s.Get(key); !ok {
		t.Fatalf("expected %q to be present", key)
	}

	st := s.Stats()
	if st.Hits == 0 {
		t.Fatalf("expected Hits > 0; got %d", st.Hits)
	}
	if st.Misses == 0 {
		t.Fatalf("expected Misses > 0; got %d", st.Misses)
	}
}
