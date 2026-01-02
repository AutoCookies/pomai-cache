// File: internal/engine/datastructures/zset.go
package datastructures

import (
	"sync"

	"github.com/AutoCookies/pomai-cache/packages/ds/skiplist"
)

// ZSetManager manages sorted sets
type ZSetManager struct {
	zsets map[string]*skiplist.Skiplist
	mu    sync.RWMutex
}

// NewZSetManager creates ZSet manager
func NewZSetManager() *ZSetManager {
	return &ZSetManager{
		zsets: make(map[string]*skiplist.Skiplist),
	}
}

// ZAdd adds member to sorted set
func (zm *ZSetManager) ZAdd(key, member string, score float64) {
	if key == "" || member == "" {
		return
	}

	zm.mu.Lock()
	defer zm.mu.Unlock()

	zset, ok := zm.zsets[key]
	if !ok {
		zset = skiplist.New()
		zm.zsets[key] = zset
	}

	zset.Add(member, score)
}

// ZRange returns members in range [start, stop]
func (zm *ZSetManager) ZRange(key string, start, stop int) []string {
	if key == "" {
		return nil
	}

	zm.mu.RLock()
	zset := zm.zsets[key]
	zm.mu.RUnlock()

	if zset == nil {
		return []string{}
	}

	return zset.Range(start, stop)
}

// ZRem removes member from sorted set
func (zm *ZSetManager) ZRem(key, member string) bool {
	if key == "" || member == "" {
		return false
	}

	zm.mu.Lock()
	defer zm.mu.Unlock()

	zset, ok := zm.zsets[key]
	if !ok {
		return false
	}

	return zset.Remove(member)
}

// ZScore returns score of member
func (zm *ZSetManager) ZScore(key, member string) (float64, bool) {
	if key == "" || member == "" {
		return 0, false
	}

	zm.mu.RLock()
	zset := zm.zsets[key]
	zm.mu.RUnlock()

	if zset == nil {
		return 0, false
	}

	return zset.Score(member)
}

// ZCard returns cardinality (size) of sorted set
func (zm *ZSetManager) ZCard(key string) int {
	if key == "" {
		return 0
	}

	zm.mu.RLock()
	zset := zm.zsets[key]
	zm.mu.RUnlock()

	if zset == nil {
		return 0
	}

	return zset.Len()
}

// ZRangeByScore returns members with scores in [min, max]
func (zm *ZSetManager) ZRangeByScore(key string, min, max float64, limit int) []skiplist.NodePublic {
	if key == "" {
		return nil
	}

	zm.mu.RLock()
	zset := zm.zsets[key]
	zm.mu.RUnlock()

	if zset == nil {
		return []skiplist.NodePublic{}
	}

	return zset.RangeByScore(min, max, limit)
}

// ZDel deletes entire sorted set
func (zm *ZSetManager) ZDel(key string) bool {
	if key == "" {
		return false
	}

	zm.mu.Lock()
	defer zm.mu.Unlock()

	_, ok := zm.zsets[key]
	if !ok {
		return false
	}

	delete(zm.zsets, key)
	return true
}

// GetAll returns all ZSets (for snapshot)
func (zm *ZSetManager) GetAll() map[string]*skiplist.Skiplist {
	zm.mu.RLock()
	defer zm.mu.RUnlock()

	result := make(map[string]*skiplist.Skiplist, len(zm.zsets))
	for k, v := range zm.zsets {
		result[k] = v
	}
	return result
}

// Clear removes all ZSets
func (zm *ZSetManager) Clear() {
	zm.mu.Lock()
	defer zm.mu.Unlock()
	zm.zsets = make(map[string]*skiplist.Skiplist)
}

// Keys returns all ZSet keys
func (zm *ZSetManager) Keys() []string {
	zm.mu.RLock()
	defer zm.mu.RUnlock()

	keys := make([]string, 0, len(zm.zsets))
	for k := range zm.zsets {
		keys = append(keys, k)
	}
	return keys
}

// Exists checks if ZSet exists
func (zm *ZSetManager) Exists(key string) bool {
	zm.mu.RLock()
	defer zm.mu.RUnlock()

	_, ok := zm.zsets[key]
	return ok
}
