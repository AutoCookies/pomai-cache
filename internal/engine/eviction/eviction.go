// File: internal/engine/eviction/eviction.go
package eviction

import (
	"container/list"
	"log"
	"sort"
	"sync/atomic"
	"time"
)

var (
	evictionLogEnabled atomic.Bool
	lastLogTime        int64
)

const minLogInterval = int64(time.Second)

func init() {
	evictionLogEnabled.Store(true)
}

func SetEvictionLogging(enabled bool) {
	evictionLogEnabled.Store(enabled)
}

func logEviction(format string, args ...interface{}) {
	if !evictionLogEnabled.Load() {
		return
	}

	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&lastLogTime)

	if now-last < minLogInterval {
		return
	}

	if atomic.CompareAndSwapInt64(&lastLogTime, last, now) {
		log.Printf("[EVICTION] "+format, args...)
	}
}

type EvictionMode int

const (
	EvictionModeNormal EvictionMode = iota
	EvictionModeOverwrite
)

type Manager struct {
	store           StoreInterface
	mode            EvictionMode
	overwriteEnable bool
	metrics         *EvictionMetrics
}

type StoreInterface interface {
	GetShard(key string) ShardInterface
	GetShardByIndex(idx int) ShardInterface
	GetShardCount() int
	GetCapacityBytes() int64
	GetTotalBytes() int64
	AddTotalBytes(delta int64)
	GetTenantID() string
	GetFreqEstimator() FreqEstimator
	GetGlobalMemCtrl() MemoryController
	GetBloomFilter() BloomFilterInterface
	AddEviction()
}

type ShardInterface interface {
	Lock()
	Unlock()
	RLock()
	RUnlock()
	GetItems() map[string]interface{}
	GetLRUBack() interface{}
	DeleteItem(key string) (size int, ok bool)
	GetBytes() int64
}

type FreqEstimator interface {
	Estimate(key string) uint32
	Increment(key string)
}

type MemoryController interface {
	Release(bytes int64)
}

type BloomFilterInterface interface {
	Add(key string)
	Remove(key string)
	MayContain(key string) bool
	Clear()
	Count() uint64
	FillRatio() float64
}

func NewManager(store StoreInterface) *Manager {
	return &Manager{
		store:           store,
		mode:            EvictionModeNormal,
		overwriteEnable: false,
		metrics:         &EvictionMetrics{},
	}
}

func (m *Manager) SetOverwriteMode(enable bool) {
	m.overwriteEnable = enable
	if enable {
		m.mode = EvictionModeOverwrite
	} else {
		m.mode = EvictionModeNormal
	}
}

func (m *Manager) GetMetrics() *EvictionMetrics {
	return m.metrics
}

func (m *Manager) ForceEvictBytes(targetBytes int64) int64 {
	if m.overwriteEnable {
		return m.forceEvictBytesOverwrite(targetBytes)
	}
	return m.forceEvictBytesNormal(targetBytes)
}

func (m *Manager) forceEvictBytesOverwrite(targetBytes int64) int64 {
	startTime := time.Now()
	totalOverwritten := int64(0)
	totalCount := 0

	shardCount := m.store.GetShardCount()

	for i := 0; i < shardCount && totalOverwritten < targetBytes; i++ {
		shard := m.store.GetShardByIndex(i)
		if shard == nil {
			continue
		}

		overwritten, count := m.overwriteTailEntries(shard, targetBytes-totalOverwritten)
		totalOverwritten += overwritten
		totalCount += count
	}

	duration := time.Since(startTime)

	m.metrics.TotalEvictions.Add(uint64(totalCount))
	m.metrics.EmergencyEvictions.Add(uint64(totalCount))
	m.metrics.BytesFreed.Add(totalOverwritten)
	m.metrics.LastEvictionDuration.Store(int64(duration))

	if totalCount > 0 {
		logEviction("OVERWRITE freed=%s count=%d", FormatBytes(totalOverwritten), totalCount)
	}

	return totalOverwritten
}

func (m *Manager) overwriteTailEntries(shard ShardInterface, needed int64) (int64, int) {
	shard.Lock()
	defer shard.Unlock()

	lruBack := shard.GetLRUBack()
	if lruBack == nil {
		return 0, 0
	}

	elem, ok := lruBack.(*list.Element)
	if !ok {
		return 0, 0
	}

	freed := int64(0)
	count := 0

	for elem != nil && freed < needed {
		entry := extractEntry(elem)
		if entry.Key == "" {
			elem = getPrevElementFromList(elem)
			continue
		}

		key := entry.Key
		size := entry.Size

		prev := getPrevElementFromList(elem)

		if deletedSize, ok := shard.DeleteItem(key); ok {
			freed += int64(deletedSize)
			count++

			m.store.AddTotalBytes(-int64(size))

			if memCtrl := m.store.GetGlobalMemCtrl(); memCtrl != nil {
				memCtrl.Release(int64(size))
			}

			if bloom := m.store.GetBloomFilter(); bloom != nil {
				bloom.Remove(key)
			}
		}

		elem = prev
	}

	return freed, count
}

func (m *Manager) forceEvictBytesNormal(targetBytes int64) int64 {
	startTime := time.Now()
	totalFreed := int64(0)
	totalEvicted := 0

	shardCount := m.store.GetShardCount()
	maxShardsToScan := shardCount / 2
	if maxShardsToScan == 0 {
		maxShardsToScan = shardCount
	}

	for i := 0; i < maxShardsToScan && totalFreed < targetBytes; i++ {
		shard := m.store.GetShardByIndex(i)
		if shard == nil {
			continue
		}

		freed, evicted := m.evictFromShard(shard, targetBytes-totalFreed)
		totalFreed += freed
		totalEvicted += evicted
	}

	duration := time.Since(startTime)

	m.metrics.TotalEvictions.Add(uint64(totalEvicted))
	m.metrics.EmergencyEvictions.Add(uint64(totalEvicted))
	m.metrics.BytesFreed.Add(totalFreed)
	m.metrics.LastEvictionDuration.Store(int64(duration))

	if totalEvicted > 0 {
		avgTime := duration.Milliseconds() / int64(totalEvicted)
		m.metrics.AvgEvictionTimeMs.Store(avgTime)
	}

	return totalFreed
}

func (m *Manager) evictFromShard(shard ShardInterface, needed int64) (int64, int) {
	shard.Lock()
	defer shard.Unlock()

	lruBack := shard.GetLRUBack()
	if lruBack == nil {
		return 0, 0
	}

	elem, ok := lruBack.(*list.Element)
	if !ok {
		return 0, 0
	}

	freed := int64(0)
	evicted := 0
	maxEvict := 100

	for elem != nil && freed < needed && evicted < maxEvict {
		entry := extractEntry(elem)
		if entry.Key == "" {
			elem = getPrevElementFromList(elem)
			continue
		}

		key := entry.Key
		size := entry.Size

		prev := getPrevElementFromList(elem)

		if deletedSize, ok := shard.DeleteItem(key); ok {
			freed += int64(deletedSize)
			evicted++

			m.store.AddTotalBytes(-int64(size))

			if memCtrl := m.store.GetGlobalMemCtrl(); memCtrl != nil {
				memCtrl.Release(int64(size))
			}
		}

		elem = prev
	}

	return freed, evicted
}

func (m *Manager) EvictIfNeeded(threshold float64) error {
	capacityBytes := m.store.GetCapacityBytes()
	if capacityBytes <= 0 {
		return nil
	}

	currentBytes := m.store.GetTotalBytes()
	usageRatio := float64(currentBytes) / float64(capacityBytes)

	if usageRatio < threshold {
		return nil
	}

	if usageRatio >= 1.0 {
		return m.EmergencyEvict(currentBytes - capacityBytes)
	}

	return nil
}

func (m *Manager) BatchEvict(targetBytes int64) int64 {
	if targetBytes <= 0 {
		return 0
	}

	estimatedCount := int(targetBytes / 128)
	if estimatedCount < 10 {
		estimatedCount = 10
	}
	if estimatedCount > 1000 {
		estimatedCount = 1000
	}

	victims := m.CollectVictimsBatch(estimatedCount)

	if len(victims) == 0 {
		return 0
	}

	sort.Slice(victims, func(i, j int) bool {
		return victims[i].Freq < victims[j].Freq
	})

	freed := int64(0)
	evictedCount := 0

	for _, v := range victims {
		if freed >= targetBytes {
			break
		}

		size := m.EvictKey(v.Key)
		if size > 0 {
			freed += int64(size)
			evictedCount++
		}
	}

	if evictedCount > 0 {
		logEviction("Batch evicted %d items, freed %s", evictedCount, FormatBytes(freed))
	}

	return freed
}

func (m *Manager) CollectVictimsBatch(estimatedCount int) []VictimCandidate {
	victims := make([]VictimCandidate, 0, estimatedCount)
	shardCount := m.store.GetShardCount()

	samplesPerShard := max(1, estimatedCount/shardCount)
	if samplesPerShard > 5 {
		samplesPerShard = 5
	}

	freqEstimator := m.store.GetFreqEstimator()

	for i := 0; i < shardCount; i++ {
		sh := m.store.GetShardByIndex(i)

		sh.RLock()

		count := 0
		elem := sh.GetLRUBack()

		for elem != nil && count < samplesPerShard {
			ent := extractEntry(elem)

			if ent.ExpireAt != 0 && time.Now().UnixNano() > ent.ExpireAt {
				elem = getPrevElement(elem)
				continue
			}

			freq := uint32(0)
			if freqEstimator != nil {
				freq = freqEstimator.Estimate(ent.Key)
			}

			victims = append(victims, VictimCandidate{
				Key:  ent.Key,
				Size: ent.Size,
				Freq: freq,
			})

			elem = getPrevElement(elem)
			count++
		}

		sh.RUnlock()
	}

	return victims
}

func (m *Manager) EmergencyEvict(targetBytes int64) error {
	const maxAttempts = 3

	for attempt := 0; attempt < maxAttempts; attempt++ {
		freed := m.BatchEvict(targetBytes)

		if freed >= targetBytes {
			logEviction("EMERGENCY tenant=%s freed=%s (attempt %d)",
				m.store.GetTenantID(), FormatBytes(freed), attempt+1)
			return nil
		}

		targetBytes -= freed

		if attempt == maxAttempts-1 {
			freed = m.ForceEvictBytes(targetBytes)
			if freed > 0 {
				logEviction("EMERGENCY FORCE tenant=%s freed=%s",
					m.store.GetTenantID(), FormatBytes(freed))
				return nil
			}
		}
	}

	return ErrInsufficientStorage
}

func (m *Manager) EvictKey(key string) int {
	if key == "" {
		return 0
	}

	sh := m.store.GetShard(key)
	sh.Lock()
	defer sh.Unlock()

	size, ok := sh.DeleteItem(key)
	if !ok {
		return 0
	}

	m.store.AddEviction()

	if memCtrl := m.store.GetGlobalMemCtrl(); memCtrl != nil {
		memCtrl.Release(int64(size))
	}

	return size
}

type Entry struct {
	Key      string
	Size     int
	ExpireAt int64
}

func extractEntry(elem interface{}) Entry {
	if elem == nil {
		return Entry{}
	}

	if listElem, ok := elem.(*list.Element); ok {
		if listElem.Value == nil {
			return Entry{}
		}

		if entryInterface, ok := listElem.Value.(interface {
			Key() string
			Size() int
			ExpireAt() int64
		}); ok {
			return Entry{
				Key:      entryInterface.Key(),
				Size:     entryInterface.Size(),
				ExpireAt: entryInterface.ExpireAt(),
			}
		}
	}

	return Entry{}
}

func getPrevElement(elem interface{}) interface{} {
	if listElem, ok := elem.(*list.Element); ok {
		return listElem.Prev()
	}
	return nil
}

func getPrevElementFromList(elem *list.Element) *list.Element {
	if elem == nil {
		return nil
	}
	return elem.Prev()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
