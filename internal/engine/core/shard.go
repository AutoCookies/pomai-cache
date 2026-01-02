// File: internal/engine/core/shard.go
package core

import (
	"container/list"
	"sync"
	"sync/atomic"
)

const (
	minCapacity = 4096
)

type Shard struct {
	mu    sync.RWMutex
	items map[string]*list.Element
	ll    *list.List
	bytes atomic.Int64

	lockfree    *LockFreeShard
	useLockfree bool

	syncmap    *SyncMapShard
	useSyncMap bool
}

func NewShard() *Shard {
	return NewShardWithCapacity(minCapacity)
}

func NewShardWithCapacity(capacity int) *Shard {
	if capacity < minCapacity {
		capacity = minCapacity
	}

	return &Shard{
		items:       make(map[string]*list.Element, capacity),
		ll:          list.New(),
		useLockfree: false,
		useSyncMap:  false,
	}
}

func NewLockFreeShardAdapter() *Shard {
	lfs := NewLockFreeShard(0) // 0 defaults to 256 stripes

	return &Shard{
		items:       make(map[string]*list.Element, 1),
		ll:          list.New(),
		lockfree:    lfs,
		useLockfree: true,
		useSyncMap:  false,
	}
}

func NewSyncMapShardAdapter() *Shard {
	sms := NewSyncMapShard()

	return &Shard{
		items:       make(map[string]*list.Element, 1),
		ll:          list.New(),
		syncmap:     sms,
		useSyncMap:  true,
		useLockfree: false,
	}
}

func (s *Shard) Get(key string) (*Entry, bool) {
	if s.useSyncMap {
		entry, ok := s.syncmap.Get(key)
		if ok {
			entry.Touch()
		}
		return entry, ok
	}

	if s.useLockfree {
		entry, ok := s.lockfree.Get(key)
		return entry, ok
	}

	s.mu.RLock()
	elem := s.items[key]
	s.mu.RUnlock()

	if elem == nil {
		return nil, false
	}

	entry := elem.Value.(*Entry)
	if entry.IsExpired() {
		return nil, false
	}

	entry.Touch()

	s.mu.Lock()
	if elem2, ok := s.items[key]; ok && elem2 == elem {
		s.ll.MoveToFront(elem)
	}
	s.mu.Unlock()

	return entry, true
}

func (s *Shard) Set(entry *Entry) (*Entry, int64) {
	if s.useSyncMap {
		old, delta := s.syncmap.Set(entry)
		entry.Touch()
		return old, delta
	}

	if s.useLockfree {
		return s.lockfree.Set(entry)
	}

	key := entry.Key()
	newSize := int64(entry.Size())

	s.mu.Lock()

	var oldEntry *Entry
	var deltaBytes int64

	if elem := s.items[key]; elem != nil {
		oldEntry = elem.Value.(*Entry)
		deltaBytes = newSize - int64(oldEntry.Size())
		elem.Value = entry
		s.ll.MoveToFront(elem)
	} else {
		elem := s.ll.PushFront(entry)
		s.items[key] = elem
		deltaBytes = newSize
	}

	s.mu.Unlock()
	s.bytes.Add(deltaBytes)

	entry.Touch()

	return oldEntry, deltaBytes
}

func (s *Shard) Delete(key string) (*Entry, bool) {
	if s.useSyncMap {
		return s.syncmap.Delete(key)
	}

	if s.useLockfree {
		return s.lockfree.Delete(key)
	}

	s.mu.Lock()

	elem := s.items[key]
	if elem == nil {
		s.mu.Unlock()
		return nil, false
	}

	entry := elem.Value.(*Entry)
	entrySize := int64(entry.Size())

	delete(s.items, key)
	s.ll.Remove(elem)

	s.mu.Unlock()
	s.bytes.Add(-entrySize)

	return entry, true
}

func (s *Shard) Len() int {
	// Atomic reads for optimized shards
	if s.useSyncMap {
		return s.syncmap.Len()
	}

	if s.useLockfree {
		return s.lockfree.Len()
	}

	s.mu.RLock()
	length := len(s.items)
	s.mu.RUnlock()
	return length
}

func (s *Shard) Bytes() int64 {
	// Atomic reads for optimized shards
	if s.useSyncMap {
		return s.syncmap.Bytes()
	}

	if s.useLockfree {
		return s.lockfree.Bytes()
	}

	return s.bytes.Load()
}

func (s *Shard) EvictExpired() []*Entry {
	if s.useSyncMap {
		return s.syncmap.EvictExpired()
	}

	if s.useLockfree {
		return s.lockfree.EvictExpired()
	}

	s.mu.Lock()

	expired := make([]*Entry, 0, 64)
	toDelete := make([]*list.Element, 0, 64)
	var totalSize int64

	count := 0
	// Limit scan to avoid holding lock too long
	for elem := s.ll.Back(); elem != nil && count < 100; elem = elem.Prev() {
		entry := elem.Value.(*Entry)
		if entry.IsExpired() {
			expired = append(expired, entry)
			toDelete = append(toDelete, elem)
			totalSize += int64(entry.Size())
		}
		count++
	}

	for _, elem := range toDelete {
		entry := elem.Value.(*Entry)
		delete(s.items, entry.Key())
		s.ll.Remove(elem)
	}

	s.mu.Unlock()

	if totalSize > 0 {
		s.bytes.Add(-totalSize)
	}

	return expired
}

func (s *Shard) Clear() {
	if s.useSyncMap {
		s.syncmap.Clear()
		return
	}

	if s.useLockfree {
		s.lockfree.Clear()
		return
	}

	s.mu.Lock()
	s.items = make(map[string]*list.Element, minCapacity)
	s.ll = list.New()
	s.mu.Unlock()
	s.bytes.Store(0)
}

func (s *Shard) GetAndTouch(key string) (*Entry, bool) {
	if s.useSyncMap {
		return s.syncmap.Get(key)
	}

	if s.useLockfree {
		return s.lockfree.GetAndTouch(key)
	}

	// For Standard Shard, GetAndTouch must perform LRU promotion
	// This requires a Write Lock
	s.mu.Lock()
	defer s.mu.Unlock()

	elem := s.items[key]
	if elem == nil {
		return nil, false
	}

	entry := elem.Value.(*Entry)
	if entry.IsExpired() {
		return nil, false
	}

	// Promote to front (LRU behavior)
	s.ll.MoveToFront(elem)

	// Update metadata (atomic operation)
	entry.Touch()

	return entry, true
}

func (s *Shard) GetFast(key string) (*Entry, bool) {
	if s.useLockfree {
		// Direct fast read
		return s.lockfree.GetFast(key)
	}
	return s.Get(key)
}

func (s *Shard) SetFast(entry *Entry) (*Entry, int64) {
	if s.useLockfree {
		return s.lockfree.SetFast(entry)
	}
	return s.Set(entry)
}

func (s *Shard) Lock() {
	// Skip locking if using optimized internal implementations
	if !s.useLockfree && !s.useSyncMap {
		s.mu.Lock()
	}
}

func (s *Shard) Unlock() {
	if !s.useLockfree && !s.useSyncMap {
		s.mu.Unlock()
	}
}

func (s *Shard) RLock() {
	if !s.useLockfree && !s.useSyncMap {
		s.mu.RLock()
	}
}

func (s *Shard) RUnlock() {
	if !s.useLockfree && !s.useSyncMap {
		s.mu.RUnlock()
	}
}

func (s *Shard) GetItems() map[string]interface{} {
	if s.useSyncMap {
		return s.syncmap.GetItems()
	}

	if s.useLockfree {
		return s.lockfree.GetItems()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make(map[string]interface{}, len(s.items))
	for k, v := range s.items {
		items[k] = v
	}
	return items
}

func (s *Shard) GetLRUBack() interface{} {
	if s.useSyncMap {
		return nil
	}

	if s.useLockfree {
		return s.lockfree.GetLRUBack()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ll.Back()
}

func (s *Shard) DeleteItem(key string) (size int, ok bool) {
	entry, ok := s.Delete(key)
	if !ok {
		return 0, false
	}
	return entry.Size(), true
}

func (s *Shard) GetBytes() int64 {
	return s.Bytes()
}

func (s *Shard) AddBytes(delta int64) {
	if s.useSyncMap {
		s.syncmap.AddBytes(delta)
		return
	}

	if s.useLockfree {
		s.lockfree.AddBytes(delta)
		return
	}

	s.bytes.Add(delta)
}

func (s *Shard) GetItemCount() int {
	return s.Len()
}

func (s *Shard) Compact() int {
	return len(s.EvictExpired())
}
