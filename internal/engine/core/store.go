// File: internal/engine/core/store.go
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/snappy"
	"golang.org/x/sync/singleflight"

	"github.com/AutoCookies/pomai-cache/internal/engine/eviction"
	"github.com/AutoCookies/pomai-cache/packages/ds/bloom"
	"github.com/AutoCookies/pomai-cache/packages/ds/sketch"
	"github.com/AutoCookies/pomai-cache/packages/ds/skiplist"
)

var (
	ErrEmptyKey            = errors.New("empty key")
	ErrInsufficientStorage = errors.New("insufficient storage")
	ErrValueNotInteger     = errors.New("value is not an integer")
	ErrKeyNotFound         = errors.New("key not found")
)

var GlobalMemCtrl MemoryController

var hashPool = sync.Pool{
	New: func() any {
		return fnv.New32a()
	},
}

type Store struct {
	config *StoreConfig

	shards     []*Shard
	shardCount uint32
	shardMask  uint32

	totalBytesAtomic int64

	bloom *bloom.BloomFilter
	g     singleflight.Group

	zsets map[string]*skiplist.Skiplist
	zmu   sync.RWMutex

	evictionManager *eviction.Manager
	evictionMetrics *eviction.EvictionMetrics
	evictionCtx     context.Context
	evictionCancel  context.CancelFunc
	freqSketch      *sketch.Sketch
}

func NewStore(shardCount int) *Store {
	config := DefaultStoreConfig()
	config.ShardCount = shardCount
	return NewStoreWithConfig(config)
}

func NewStoreWithOptions(shardCount int, capacityBytes int64) *Store {
	config := DefaultStoreConfig()
	config.ShardCount = shardCount
	config.CapacityBytes = capacityBytes
	return NewStoreWithConfig(config)
}

func NewStoreWithConfig(config *StoreConfig) *Store {
	if config.ShardCount <= 0 {
		config.ShardCount = 256
	}

	shardCount := nextPowerOf2(config.ShardCount)
	config.ShardCount = shardCount

	ctx, cancel := context.WithCancel(context.Background())

	s := &Store{
		config:          config,
		shards:          make([]*Shard, shardCount),
		shardCount:      uint32(shardCount),
		shardMask:       uint32(shardCount - 1),
		zsets:           make(map[string]*skiplist.Skiplist),
		evictionMetrics: &eviction.EvictionMetrics{},
		evictionCtx:     ctx,
		evictionCancel:  cancel,
	}

	s.freqSketch = sketch.New(1<<16, 4)

	for i := 0; i < shardCount; i++ {
		s.shards[i] = NewLockFreeShardAdapter()
	}

	s.evictionManager = eviction.NewManager(s)

	return s
}

func (s *Store) Shutdown() {
	if s.evictionCancel != nil {
		s.evictionCancel()
	}
}

func (s *Store) getShardFast(key string) *Shard {
	h := hashPool.Get().(hash.Hash32)
	h.Reset()
	h.Write([]byte(key))
	idx := h.Sum32() & s.shardMask
	hashPool.Put(h)
	return s.shards[idx]
}

func (s *Store) getShard(key string) *Shard {
	return s.getShardFast(key)
}

func (s *Store) hashToShardIndex(key string) int {
	h := hashPool.Get().(hash.Hash32)
	h.Reset()
	h.Write([]byte(key))
	idx := int(h.Sum32() & s.shardMask)
	hashPool.Put(h)
	return idx
}

func (s *Store) GetShards() []*Shard {
	return s.shards
}

func (s *Store) Get(key string) ([]byte, bool) {
	if key == "" {
		return nil, false
	}

	if s.bloom != nil {
		if !s.bloom.MayContain(key) {
			return nil, false
		}
	}

	shard := s.getShardFast(key)
	entry, ok := shard.Get(key)

	if !ok {
		return nil, false
	}

	if s.freqSketch != nil {
		s.freqSketch.Increment(key)
	}

	return entry.Value(), true
}

func (s *Store) GetFast(key string) ([]byte, bool) {
	if key == "" {
		return nil, false
	}

	if s.bloom != nil {
		if !s.bloom.MayContain(key) {
			return nil, false
		}
	}

	shard := s.getShardFast(key)
	entry, ok := shard.Get(key)

	if !ok {
		return nil, false
	}

	return entry.Value(), true
}

func (s *Store) Put(key string, value []byte, ttl time.Duration) error {
	if key == "" {
		return ErrEmptyKey
	}

	entry := NewEntry(key, value, ttl)
	shard := s.getShardFast(key)
	_, deltaBytes := shard.Set(entry)

	atomic.AddInt64(&s.totalBytesAtomic, deltaBytes)

	if s.bloom != nil {
		s.bloom.Add(key)
	}

	if s.freqSketch != nil {
		s.freqSketch.Increment(key)
	}

	return nil
}

func (s *Store) PutFast(key string, value []byte, ttl time.Duration) error {
	if key == "" {
		return ErrEmptyKey
	}

	entry := NewEntry(key, value, ttl)
	shard := s.getShardFast(key)
	_, deltaBytes := shard.Set(entry)

	atomic.AddInt64(&s.totalBytesAtomic, deltaBytes)

	if s.bloom != nil {
		s.bloom.Add(key)
	}

	if s.freqSketch != nil {
		s.freqSketch.Increment(key)
	}

	return nil
}

func (s *Store) Delete(key string) {
	if key == "" {
		return
	}

	shard := s.getShardFast(key)
	entry, ok := shard.Delete(key)

	if !ok {
		return
	}

	atomic.AddInt64(&s.totalBytesAtomic, -int64(entry.Size()))

	if GlobalMemCtrl != nil {
		GlobalMemCtrl.Release(int64(entry.Size()))
	}
}

func (s *Store) Exists(key string) bool {
	if key == "" {
		return false
	}

	if s.bloom != nil {
		if !s.bloom.MayContain(key) {
			return false
		}
	}

	shard := s.getShardFast(key)
	_, ok := shard.Get(key)
	return ok
}

func (s *Store) Incr(key string, delta int64) (int64, error) {
	if key == "" {
		return 0, ErrEmptyKey
	}

	shard := s.getShardFast(key)

	if shard.useLockfree {
		return s.incrLockFree(shard, key, delta)
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	var currentVal int64 = 0

	elem, ok := shard.items[key]
	if ok {
		ent := elem.Value.(*Entry)
		raw := ent.value

		var valStr string
		if len(raw) > 0 {
			magic := raw[0]
			payload := raw[1:]
			if magic == 1 {
				decoded, err := snappy.Decode(nil, payload)
				if err != nil {
					return 0, fmt.Errorf("corrupted data: %w", err)
				}
				valStr = string(decoded)
			} else {
				valStr = string(payload)
			}
		}

		val, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			return 0, ErrValueNotInteger
		}
		currentVal = val
	}

	newVal := currentVal + delta
	newValBytes := []byte(strconv.FormatInt(newVal, 10))
	finalData := make([]byte, len(newValBytes)+1)
	finalData[0] = 0
	copy(finalData[1:], newValBytes)

	newEntry := NewEntry(key, finalData, 0)

	var deltaBytes int64
	if elem, ok := shard.items[key]; ok {
		oldEntry := elem.Value.(*Entry)
		deltaBytes = int64(newEntry.Size() - oldEntry.Size())
		elem.Value = newEntry
		shard.ll.MoveToFront(elem)
	} else {
		elem := shard.ll.PushFront(newEntry)
		shard.items[key] = elem
		deltaBytes = int64(newEntry.Size())
	}

	shard.bytes.Add(deltaBytes)
	atomic.AddInt64(&s.totalBytesAtomic, deltaBytes)

	return newVal, nil
}

func (s *Store) incrLockFree(shard *Shard, key string, delta int64) (int64, error) {
	entry, ok := shard.lockfree.Get(key)

	var currentVal int64 = 0

	if ok {
		raw := entry.value
		var valStr string
		if len(raw) > 0 {
			magic := raw[0]
			payload := raw[1:]
			if magic == 1 {
				decoded, err := snappy.Decode(nil, payload)
				if err != nil {
					return 0, fmt.Errorf("corrupted data: %w", err)
				}
				valStr = string(decoded)
			} else {
				valStr = string(payload)
			}
		}

		val, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			return 0, ErrValueNotInteger
		}
		currentVal = val
	}

	newVal := currentVal + delta
	newValBytes := []byte(strconv.FormatInt(newVal, 10))
	finalData := make([]byte, len(newValBytes)+1)
	finalData[0] = 0
	copy(finalData[1:], newValBytes)

	newEntry := NewEntry(key, finalData, 0)
	_, deltaBytes := shard.lockfree.Set(newEntry)

	atomic.AddInt64(&s.totalBytesAtomic, deltaBytes)

	return newVal, nil
}

func (s *Store) MGet(keys []string) map[string][]byte {
	if len(keys) == 0 {
		return nil
	}

	results := make(map[string][]byte, len(keys))

	for _, key := range keys {
		if val, ok := s.Get(key); ok {
			results[key] = val
		}
	}

	return results
}

func (s *Store) MSet(items map[string][]byte, ttl time.Duration) error {
	if len(items) == 0 {
		return nil
	}

	for key, val := range items {
		if err := s.Put(key, val, ttl); err != nil {
			return fmt.Errorf("mset failed at key %s: %w", key, err)
		}
	}

	return nil
}

func (s *Store) Clear() {
	for _, shard := range s.shards {
		shard.Clear()
	}
	atomic.StoreInt64(&s.totalBytesAtomic, 0)
}

func (s *Store) SetTenantID(tenantID string) {
	if tenantID == "" {
		tenantID = "default"
	}
	s.config.TenantID = tenantID
}

func (s *Store) SetBloomFilter(bf interface{}) {
	if bloomFilter, ok := bf.(*bloom.BloomFilter); ok {
		s.bloom = bloomFilter
	}
}

func (s *Store) GetBloomFilter() eviction.BloomFilterInterface {
	return s.bloom
}

func (s *Store) GetConfig() *StoreConfig {
	return s.config
}

func (s *Store) GetShard(key string) eviction.ShardInterface {
	return s.getShardFast(key)
}

func (s *Store) GetShardByIndex(idx int) eviction.ShardInterface {
	if idx < 0 || idx >= len(s.shards) {
		return nil
	}
	return s.shards[idx]
}

func (s *Store) GetShardCount() int {
	return int(s.shardCount)
}

func (s *Store) GetCapacityBytes() int64 {
	return s.config.CapacityBytes
}

func (s *Store) GetTotalBytes() int64 {
	return atomic.LoadInt64(&s.totalBytesAtomic)
}

func (s *Store) AddTotalBytes(delta int64) {
	atomic.AddInt64(&s.totalBytesAtomic, delta)
}

func (s *Store) GetTenantID() string {
	return s.config.TenantID
}

func (s *Store) GetFreqEstimator() eviction.FreqEstimator {
	if s.freqSketch == nil {
		return nil
	}
	return &freqEstimatorWrapper{sketch: s.freqSketch}
}

type freqEstimatorWrapper struct {
	sketch *sketch.Sketch
}

func (w *freqEstimatorWrapper) Estimate(key string) uint32 {
	if w.sketch == nil {
		return 0
	}
	return w.sketch.Estimate(key)
}

func (w *freqEstimatorWrapper) Increment(key string) {
	if w.sketch != nil {
		w.sketch.Increment(key)
	}
}

func (s *Store) GetGlobalMemCtrl() eviction.MemoryController {
	if GlobalMemCtrl == nil {
		return nil
	}
	return &memoryControllerWrapper{mc: GlobalMemCtrl}
}

func (s *Store) AddEviction() {}

func (s *Store) ForceEvictBytes(targetBytes int64) int64 {
	if s.evictionManager == nil {
		return 0
	}
	return s.evictionManager.ForceEvictBytes(targetBytes)
}

type memoryControllerWrapper struct {
	mc MemoryController
}

func (w *memoryControllerWrapper) Release(bytes int64) {
	if w.mc != nil {
		w.mc.Release(bytes)
	}
}

func (w *memoryControllerWrapper) Reserve(bytes int64) bool {
	if w.mc != nil {
		return w.mc.Reserve(bytes)
	}
	return true
}

func (w *memoryControllerWrapper) Used() int64 {
	if w.mc != nil {
		return w.mc.Used()
	}
	return 0
}

func (w *memoryControllerWrapper) Capacity() int64 {
	if w.mc != nil {
		return w.mc.Capacity()
	}
	return 0
}

func (s *Store) EvictionStats() EvictionMetrics {
	return EvictionMetrics{}
}

func (s *Store) Stats() Stats {
	var totalItems int64
	var totalBytes int64

	for _, shard := range s.shards {
		totalItems += int64(shard.Len())
		totalBytes += shard.Bytes()
	}

	return Stats{
		Items:      totalItems,
		Bytes:      totalBytes,
		Capacity:   s.config.CapacityBytes,
		ShardCount: int(s.shardCount),
		TenantID:   s.config.TenantID,
	}
}

func (s *Store) GetHits() uint64 {
	return 0
}

func (s *Store) GetMisses() uint64 {
	return 0
}

func (s *Store) GetEvictions() uint64 {
	return 0
}

func (s *Store) ResetStats() {}

func (s *Store) Serialize() (io.Reader, error) {
	allEntries := make(map[string][]byte)

	for _, shard := range s.shards {
		items := shard.GetItems()
		for key, val := range items {
			if elem, ok := val.(*Entry); ok {
				if !elem.IsExpired() {
					allEntries[key] = elem.Value()
				}
			}
		}
	}

	data, err := json.Marshal(allEntries)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize:  %w", err)
	}

	return bytes.NewReader(data), nil
}

func (s *Store) RestoreFrom(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("failed to read restore data: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var entries map[string][]byte
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to deserialize store data: %w", err)
	}

	restoredCount := 0
	for key, value := range entries {
		if err := s.Put(key, value, 0); err != nil {
			log.Printf("Failed to restore key %s: %v", key, err)
		} else {
			restoredCount++
		}
	}

	log.Printf("Restored %d/%d entries to tenant '%s'",
		restoredCount, len(entries), s.config.TenantID)

	return nil
}

func (s *Store) SnapshotTo(w io.Writer) error {
	reader, err := s.Serialize()
	if err != nil {
		return err
	}
	_, err = io.Copy(w, reader)
	return err
}

func nextPowerOf2(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n++
	return n
}

func (s *Store) EvictExpired() int {
	totalEvicted := 0

	for i := 0; i < int(s.shardCount); i++ {
		shard := s.shards[i]
		expired := shard.EvictExpired()
		totalEvicted += len(expired)
	}

	return totalEvicted
}
