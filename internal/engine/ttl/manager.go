// File: internal/engine/ttl/manager.go
package ttl

import (
	"time"
)

// Manager handles TTL operations
type Manager struct {
	store StoreInterface
}

// NewManager creates TTL manager
func NewManager(store StoreInterface) *Manager {
	return &Manager{store: store}
}

// TTLRemaining checks remaining TTL for a key
func (m *Manager) TTLRemaining(key string) (time.Duration, bool) {
	if key == "" {
		return 0, false
	}

	// Fast-path:  Check bloom filter
	bloom := m.store.GetBloomFilter()
	if bloom != nil && !bloom.MayContain(key) {
		return 0, false
	}

	shard := m.store.GetShard(key)

	// Quick check with read lock
	shard.RLock()
	items := shard.GetItems()
	elem, ok := items[key]
	if !ok {
		shard.RUnlock()
		return 0, false
	}

	entry := extractEntry(elem)
	if entry == nil {
		shard.RUnlock()
		return 0, false
	}

	// No TTL (permanent)
	if entry.ExpireAt() == 0 {
		shard.RUnlock()
		return 0, true
	}

	remain := time.Until(time.Unix(0, entry.ExpireAt()))
	shard.RUnlock()

	// Lazy delete if expired
	if remain <= 0 {
		m.DeleteExpired(key)
		return 0, false
	}

	return remain, true
}

// DeleteExpired deletes key if expired (with double-check locking)
func (m *Manager) DeleteExpired(key string) bool {
	shard := m.store.GetShard(key)
	shard.Lock()
	defer shard.Unlock()

	// Double-check:  verify key still exists and is expired
	items := shard.GetItems()
	elem, ok := items[key]
	if !ok {
		return false
	}

	entry := extractEntry(elem)
	if entry == nil {
		return false
	}

	if entry.ExpireAt() == 0 {
		return false // No expiration
	}

	if time.Now().UnixNano() <= entry.ExpireAt() {
		return false // Not expired yet
	}

	// Delete
	size, deleted := shard.DeleteItem(key)
	if !deleted {
		return false
	}

	// Update metrics
	shard.AddBytes(-int64(size))
	m.store.AddTotalBytes(-int64(size))

	// Release memory
	if memCtrl := m.store.GetGlobalMemCtrl(); memCtrl != nil {
		memCtrl.Release(int64(size))
	}

	return true
}

// Helper:   Extract entry from interface
func extractEntry(elem interface{}) EntryInterface {
	if entry, ok := elem.(EntryInterface); ok {
		return entry
	}
	// Handle container/list. Element wrapping
	// This assumes elem might be *list.Element containing EntryInterface
	return nil
}
