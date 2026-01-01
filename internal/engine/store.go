package engine

import (
	"container/list"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/snappy"
	"golang.org/x/sync/singleflight"

	"github.com/AutoCookies/pomai-cache/packages/ds/bloom"
	"github.com/AutoCookies/pomai-cache/packages/ds/sketch"
	"github.com/AutoCookies/pomai-cache/packages/ds/skiplist"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Global controllers (Dependency Injection is better, but keeping for compatibility)
var GlobalMemCtrl *MemoryController
var GlobalFreq *sketch.Sketch

func InitGlobalFreq(width, depth uint32) {
	GlobalFreq = sketch.New(width, depth)
}

// entry là đơn vị lưu trữ cơ bản trong RAM
type entry struct {
	key        string
	value      []byte
	size       int
	expireAt   int64
	lastAccess int64
	accesses   uint64
	createdAt  int64
}

// shard giúp chia nhỏ lock để tăng performance (giảm contention)
type shard struct {
	mu    sync.RWMutex
	items map[string]*list.Element
	ll    *list.List
	bytes int64
}

// Store là struct trung tâm quản lý Cache
type Store struct {
	shards        []*shard
	shardCount    uint32
	capacityBytes int64

	// Metrics & Counters
	totalBytesAtomic int64
	freqBoost        int64
	hits             uint64
	misses           uint64
	evictions        uint64

	// Feature modules
	adaptiveTTL *AdaptiveTTL

	// [PKG] Sử dụng BloomFilter từ pkg/ds/bloom
	bloom      *bloom.BloomFilter
	bloomStats BloomStats

	// Singleflight để chống Thundering Herd (gộp request)
	g singleflight.Group

	// [PKG] Sử dụng Skiplist từ pkg/ds/skiplist cho ZSET
	zsets map[string]*skiplist.Skiplist
	zmu   sync.RWMutex
}

// BloomStats struct giữ metric của Bloom Filter
type BloomStats struct {
	Hits              uint64
	Misses            uint64
	Avoided           uint64
	FalsePositiveRate float64
}

// NewStore tạo store mặc định
func NewStore(shardCount int) *Store {
	return NewStoreWithOptions(shardCount, 0)
}

// NewStoreWithOptions tạo store với cấu hình chi tiết
func NewStoreWithOptions(shardCount int, capacityBytes int64) *Store {
	if shardCount <= 0 {
		shardCount = 256
	}

	s := &Store{
		shards:        make([]*shard, shardCount),
		shardCount:    uint32(shardCount),
		capacityBytes: capacityBytes,
		freqBoost:     1_000_000,
		zsets:         make(map[string]*skiplist.Skiplist),
	}
	for i := 0; i < shardCount; i++ {
		s.shards[i] = &shard{
			items: make(map[string]*list.Element),
			ll:    list.New(),
		}
	}
	return s
}

// --- Helper Functions ---

func (s *Store) getShard(key string) *shard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	idx := h.Sum32() % s.shardCount
	return s.shards[int(idx)]
}

func (s *Store) hashToShardIndex(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % s.shardCount)
}

// --- Core Operations (Put, Get, Delete, Incr) ---

// Put lưu key-value vào cache
func (s *Store) Put(key string, value []byte, ttl time.Duration) error {
	if key == "" {
		return errors.New("missing key")
	}

	// Copy value để an toàn bộ nhớ (tránh race condition nếu caller sửa buffer ngoài)
	vcopy := make([]byte, len(value))
	copy(vcopy, value)
	newSize := len(vcopy)

	// Tính toán TTL
	now := time.Now().UnixNano()
	var expireAt int64
	if ttl > 0 {
		expireAt = time.Now().Add(ttl).UnixNano()
	}

	// [LOGIC TINY-LFU] Kiểm tra Admission Control (Code nằm ở store_eviction.go)
	if s.capacityBytes > 0 && GlobalFreq != nil {
		if !s.admitOrRejectTinyLFU(key, newSize) {
			return fmt.Errorf("insufficient storage (TinyLFU rejected)")
		}
	}

	sh := s.getShard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if elem, ok := sh.items[key]; ok {
		// --- UPDATE EXISTING ---
		ent := elem.Value.(*entry)
		oldSize := ent.size
		delta := int64(newSize - oldSize)

		// Kiểm tra OOM Guard toàn cục
		if delta > 0 && GlobalMemCtrl != nil && !GlobalMemCtrl.Reserve(delta) {
			return fmt.Errorf("insufficient storage (OOM Guard)")
		}

		// Update entry data
		ent.value = vcopy
		ent.size = newSize
		ent.expireAt = expireAt // Reset TTL on update
		ent.lastAccess = now
		atomic.AddUint64(&ent.accesses, 1)

		// Update metrics
		sh.bytes += delta
		sh.ll.MoveToFront(elem) // LRU update
		atomic.AddInt64(&s.totalBytesAtomic, delta)

		if delta < 0 && GlobalMemCtrl != nil {
			GlobalMemCtrl.Release(-delta)
		}
	} else {
		// --- INSERT NEW ---

		// [LOGIC EVICTION] Xả bớt nếu đầy (Code nằm ở store_eviction.go)
		if s.capacityBytes > 0 && atomic.LoadInt64(&s.totalBytesAtomic)+int64(newSize) > s.capacityBytes {
			// Thử evict tối đa 3 lần
			for i := 0; i < 3; i++ {
				s.evictIfNeeded(s.hashToShardIndex(key))
				if atomic.LoadInt64(&s.totalBytesAtomic)+int64(newSize) <= s.capacityBytes {
					break
				}
			}
			// Check lại lần cuối
			if atomic.LoadInt64(&s.totalBytesAtomic)+int64(newSize) > s.capacityBytes {
				return fmt.Errorf("insufficient storage")
			}
		}

		if GlobalMemCtrl != nil && !GlobalMemCtrl.Reserve(int64(newSize)) {
			return fmt.Errorf("insufficient storage (OOM Guard)")
		}

		ent := &entry{
			key:        key,
			value:      vcopy,
			size:       newSize,
			expireAt:   expireAt,
			lastAccess: now,
			accesses:   1,
			createdAt:  now,
		}

		elem := sh.ll.PushFront(ent)
		sh.items[key] = elem
		sh.bytes += int64(newSize)
		atomic.AddInt64(&s.totalBytesAtomic, int64(newSize))

		// Add to Bloom Filter
		if s.bloom != nil {
			s.bloom.Add(key)
		}
	}

	// Update Frequency Sketch (Count-Min Sketch)
	if GlobalFreq != nil {
		GlobalFreq.Increment(key)
	}

	return nil
}

// Get lấy value từ cache
func (s *Store) Get(key string) ([]byte, bool) {
	if key == "" {
		atomic.AddUint64(&s.misses, 1)
		return nil, false
	}

	// 1. Bloom Filter Check (Fast negative path)
	if s.bloom != nil && !s.bloom.MayContain(key) {
		atomic.AddUint64(&s.misses, 1)
		atomic.AddUint64(&s.bloomStats.Avoided, 1)
		return nil, false
	}

	sh := s.getShard(key)

	// Dùng Read Lock để kiểm tra nhanh
	sh.mu.RLock()
	elem, ok := sh.items[key]
	if !ok {
		sh.mu.RUnlock()
		atomic.AddUint64(&s.misses, 1)
		if s.bloom != nil {
			atomic.AddUint64(&s.bloomStats.Misses, 1) // False positive
		}
		return nil, false
	}
	if s.bloom != nil {
		atomic.AddUint64(&s.bloomStats.Hits, 1)
	}

	ent := elem.Value.(*entry)

	// 2. Check Expiration
	if ent.expireAt != 0 && time.Now().UnixNano() > ent.expireAt {
		sh.mu.RUnlock()
		// Lazy Delete: Chuyển sang Write Lock để xóa (Hàm ở store_ttl.go)
		s.deleteExpired(key)
		atomic.AddUint64(&s.misses, 1)
		return nil, false
	}

	// Copy value để trả về (tránh race condition)
	val := make([]byte, len(ent.value))
	copy(val, ent.value)
	sh.mu.RUnlock()

	// 3. Update Metadata (LRU & Access Count)
	// Cần Write Lock để move list element.
	// Để tối ưu, ta lock lại shard một lần nữa.
	sh.mu.Lock()
	if elem, ok := sh.items[key]; ok { // Check lại vì có thể bị xóa giữa chừng
		ent := elem.Value.(*entry)
		ent.lastAccess = time.Now().UnixNano()
		atomic.AddUint64(&ent.accesses, 1)
		sh.ll.MoveToFront(elem) // Đẩy lên đầu LRU
	}
	sh.mu.Unlock()

	// Update Frequency Sketch (Async safe - không cần lock)
	if GlobalFreq != nil {
		GlobalFreq.Increment(key)
	}

	atomic.AddUint64(&s.hits, 1)
	return val, true
}

// Delete xóa key
func (s *Store) Delete(key string) {
	if key == "" {
		return
	}
	sh := s.getShard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if elem, ok := sh.items[key]; ok {
		ent := elem.Value.(*entry)
		delete(sh.items, key)
		sh.ll.Remove(elem)

		// Update Memory
		sh.bytes -= int64(ent.size)
		atomic.AddInt64(&s.totalBytesAtomic, -int64(ent.size))
		if GlobalMemCtrl != nil {
			GlobalMemCtrl.Release(int64(ent.size))
		}
	}
}

// Incr tăng giảm giá trị atomic (Hỗ trợ Snappy Magic Byte)
func (s *Store) Incr(key string, delta int64) (int64, error) {
	if key == "" {
		return 0, errors.New("missing key")
	}

	sh := s.getShard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	var currentVal int64 = 0

	// 1. Get current value
	if elem, ok := sh.items[key]; ok {
		ent := elem.Value.(*entry)

		// Decode Value logic (Giống Get nhưng Inline)
		raw := ent.value
		var valStr string
		if len(raw) > 0 {
			magic := raw[0]
			payload := raw[1:]
			if magic == 1 { // Compressed
				decoded, err := snappy.Decode(nil, payload)
				if err != nil {
					return 0, fmt.Errorf("corrupted data")
				}
				valStr = string(decoded)
			} else { // Raw
				valStr = string(payload)
			}
		}

		val, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("value is not an integer")
		}
		currentVal = val
	}

	// 2. Calculate new value
	newVal := currentVal + delta

	// 3. Encode (Luôn dùng Uncompressed cho Int để nhanh)
	newValBytes := []byte(strconv.FormatInt(newVal, 10))
	finalData := make([]byte, len(newValBytes)+1)
	finalData[0] = 0 // Magic byte 0 = Uncompressed
	copy(finalData[1:], newValBytes)

	newSize := len(finalData)

	// 4. Update Store (Logic giống Put nhưng Inline để giữ Lock)
	if elem, ok := sh.items[key]; ok {
		ent := elem.Value.(*entry)
		oldSize := ent.size
		deltaSize := int64(newSize - oldSize)

		if deltaSize > 0 && GlobalMemCtrl != nil && !GlobalMemCtrl.Reserve(deltaSize) {
			return 0, fmt.Errorf("insufficient storage")
		}

		ent.value = finalData
		ent.size = newSize
		sh.bytes += deltaSize
		atomic.AddInt64(&s.totalBytesAtomic, deltaSize)
		sh.ll.MoveToFront(elem)

		if deltaSize < 0 && GlobalMemCtrl != nil {
			GlobalMemCtrl.Release(-deltaSize)
		}
	} else {
		// New Insert for Incr
		ent := &entry{
			key: key, value: finalData, size: newSize,
			expireAt: 0, lastAccess: time.Now().UnixNano(), accesses: 1, createdAt: time.Now().UnixNano(),
		}

		if GlobalMemCtrl != nil && !GlobalMemCtrl.Reserve(int64(newSize)) {
			return 0, fmt.Errorf("insufficient storage")
		}

		elem := sh.ll.PushFront(ent)
		sh.items[key] = elem
		sh.bytes += int64(newSize)
		atomic.AddInt64(&s.totalBytesAtomic, int64(newSize))

		if s.bloom != nil {
			s.bloom.Add(key)
		}
	}

	return newVal, nil
}
