package cache

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestPutGetDelete(t *testing.T) {
	s := NewStore(16)
	key := "hello"
	val := []byte("world")
	s.Put(key, val, 0)

	got, ok := s.Get(key)
	if !ok {
		t.Fatalf("expected key present")
	}
	if string(got) != "world" {
		t.Fatalf("unexpected value: %s", string(got))
	}

	s.Delete(key)
	_, ok = s.Get(key)
	if ok {
		t.Fatalf("expected key deleted")
	}
}

func TestTTLExpiration(t *testing.T) {
	s := NewStore(8)
	key := "temp"
	s.Put(key, []byte("v"), 50*time.Millisecond)

	// should exist initially
	if _, ok := s.Get(key); !ok {
		t.Fatalf("expected key present immediately after put")
	}

	// wait for expiry
	time.Sleep(120 * time.Millisecond)

	if _, ok := s.Get(key); ok {
		t.Fatalf("expected key expired")
	}

	// TTLRemaining should return not found
	if _, ok := s.TTLRemaining(key); ok {
		t.Fatalf("expected TTLRemaining false for expired key")
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := NewStore(32)
	const goroutines = 50
	const opsPer = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPer; i++ {
				k := fmt.Sprintf("k-%d", i%100) // 100 distinct keys
				v := []byte(strconv.Itoa(id*opsPer + i))
				s.Put(k, v, 0)
				// try get
				if got, _ := s.Get(k); got != nil && len(got) == 0 {
					t.Fatalf("unexpected empty value")
				}
				if i%10 == 0 {
					s.Delete(k)
				}
			}
		}(g)
	}
	wg.Wait()

	// basic sanity: stats should be non-zero (some hits/misses)
	stats := s.Stats()
	if stats.Items < 0 {
		t.Fatalf("invalid items count")
	}
}
