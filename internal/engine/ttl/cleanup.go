// File: internal/engine/ttl/cleanup.go
package ttl

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/AutoCookies/pomai-cache/packages/ds/bloom"
)

type Cleaner struct {
	manager          *Manager
	store            StoreInterface
	cleanupInterval  time.Duration
	rebuildThreshold int
	logger           *log.Logger
}

func NewCleaner(manager *Manager, store StoreInterface) *Cleaner {
	return &Cleaner{
		manager:          manager,
		store:            store,
		cleanupInterval:  time.Minute,
		rebuildThreshold: 1000,
	}
}

func (c *Cleaner) SetInterval(interval time.Duration) {
	c.cleanupInterval = interval
}

func (c *Cleaner) SetRebuildThreshold(threshold int) {
	c.rebuildThreshold = threshold
}

func (c *Cleaner) Start(ctx context.Context) {
	ticker := time.NewTicker(c.cleanupInterval)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				c.logInfo("Cleanup worker stopped")
				return

			case <-ticker.C:
				cleaned := c.CleanupExpired()
				if cleaned > 0 {
					c.logInfo("Cleaned %d expired keys", cleaned)
				}
			}
		}
	}()
}

func (c *Cleaner) CleanupExpired() int {
	now := time.Now().UnixNano()
	cleaned := int64(0)

	shards := c.store.GetShards()

	for _, shard := range shards {
		count := c.cleanupShard(shard, now)
		atomic.AddInt64(&cleaned, int64(count))
	}

	totalCleaned := int(atomic.LoadInt64(&cleaned))

	if totalCleaned > c.rebuildThreshold {
		c.logInfo("Triggering incremental bloom rebuild after %d deletions", totalCleaned)
		go c.IncrementalBloomRebuild()
	}

	return totalCleaned
}

func (c *Cleaner) cleanupShard(shard ShardInterface, now int64) int {
	shard.Lock()
	defer shard.Unlock()

	toDelete := make([]string, 0)
	items := shard.GetItems()

	for key, elem := range items {
		entry := extractEntry(elem)
		if entry == nil {
			continue
		}

		expireAt := entry.ExpireAt()
		if expireAt != 0 && now > expireAt {
			toDelete = append(toDelete, key)
		}
	}

	totalSize := 0
	for _, key := range toDelete {
		size, ok := shard.DeleteItem(key)
		if ok {
			totalSize += size
		}
	}

	if totalSize > 0 {
		shard.AddBytes(-int64(totalSize))
		c.store.AddTotalBytes(-int64(totalSize))

		if memCtrl := c.store.GetGlobalMemCtrl(); memCtrl != nil {
			memCtrl.Release(int64(totalSize))
		}
	}

	return len(toDelete)
}

func (c *Cleaner) IncrementalBloomRebuild() {
	bloomFilter := c.store.GetBloomFilter()
	if bloomFilter == nil {
		return
	}

	c.logInfo("Starting incremental bloom rebuild")

	totalItems := int64(0)
	shards := c.store.GetShards()

	for _, shard := range shards {
		totalItems += int64(shard.GetItemCount())
	}

	if totalItems == 0 {
		c.logInfo("No items - clearing bloom filter")
		bloomFilter.Clear()
		return
	}

	newBloom := bloom.NewOptimal(uint64(totalItems), 0.01)

	keysAdded := 0
	for _, shard := range shards {
		added := c.rebuildShardBloom(shard, newBloom)
		keysAdded += added
	}

	c.store.SetBloomFilter(newBloom)

	c.logInfo("Incremental bloom rebuild complete:  %d keys", keysAdded)
}

func (c *Cleaner) rebuildShardBloom(shard ShardInterface, bloomFilter *bloom.BloomFilter) int {
	shard.RLock()
	defer shard.RUnlock()

	items := shard.GetItems()
	added := 0

	for key := range items {
		bloomFilter.Add(key)
		added++

		if added%1000 == 0 {
			time.Sleep(time.Microsecond)
		}
	}

	return added
}

func (c *Cleaner) logInfo(format string, args ...interface{}) {
	if c.logger != nil {
		c.logger.Printf("[TTL-CLEANUP] "+format, args...)
	} else {
		log.Printf("[TTL-CLEANUP] "+format, args...)
	}
}
