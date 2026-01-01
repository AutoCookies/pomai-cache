package engine

import (
	"sync/atomic"
)

// Stats là struct chứa báo cáo tổng quan về Cache
// Được trả về qua API /stats
type Stats struct {
	Hits       uint64 `json:"hits"`
	Misses     uint64 `json:"misses"`
	Items      int64  `json:"items"`
	Bytes      int64  `json:"bytes"`
	Capacity   int64  `json:"capacity"`
	Evictions  uint64 `json:"evictions"`
	ShardCount int    `json:"shard_count"`
	FreqBoost  int64  `json:"freq_boost"`

	// Adaptive TTL Info
	AdaptiveTTL    bool   `json:"adaptive_ttl_enabled"`
	AdaptiveMinTTL string `json:"adaptive_min_ttl,omitempty"`
	AdaptiveMaxTTL string `json:"adaptive_max_ttl,omitempty"`

	// Bloom Filter Info
	BloomEnabled bool    `json:"bloom_enabled"`
	BloomFPRate  float64 `json:"bloom_false_positive_rate"`
	BloomAvoided uint64  `json:"bloom_avoided_lookups"`
}

// Stats thu thập dữ liệu từ tất cả các Shard và Atomic Counters
func (s *Store) Stats() Stats {
	var totalItems int64
	var totalBytes int64

	// 1. Duyệt qua từng shard để cộng dồn items và bytes
	// Cần RLock để đảm bảo số liệu chính xác tại thời điểm đọc
	for _, sh := range s.shards {
		sh.mu.RLock()
		totalItems += int64(len(sh.items))
		totalBytes += sh.bytes
		sh.mu.RUnlock()
	}

	// 2. Lấy số liệu Bloom Filter
	bloomStats := s.GetBloomStats()

	// 3. Tổng hợp báo cáo
	stats := Stats{
		Hits:         atomic.LoadUint64(&s.hits),
		Misses:       atomic.LoadUint64(&s.misses),
		Items:        totalItems,
		Bytes:        totalBytes,
		Capacity:     s.capacityBytes,
		Evictions:    atomic.LoadUint64(&s.evictions),
		ShardCount:   int(s.shardCount),
		FreqBoost:    atomic.LoadInt64(&s.freqBoost),
		AdaptiveTTL:  s.adaptiveTTL != nil,
		BloomEnabled: s.bloom != nil,
		BloomFPRate:  bloomStats.FalsePositiveRate,
		BloomAvoided: bloomStats.Avoided,
	}

	if s.adaptiveTTL != nil {
		stats.AdaptiveMinTTL = s.adaptiveTTL.minTTL.String()
		stats.AdaptiveMaxTTL = s.adaptiveTTL.maxTTL.String()
	}

	return stats
}

// GetBloomStats tính toán tỷ lệ dương tính giả (False Positive Rate)
func (s *Store) GetBloomStats() BloomStats {
	hits := atomic.LoadUint64(&s.bloomStats.Hits)
	misses := atomic.LoadUint64(&s.bloomStats.Misses)
	avoided := atomic.LoadUint64(&s.bloomStats.Avoided)

	stats := BloomStats{
		Hits:    hits,
		Misses:  misses,
		Avoided: avoided,
	}

	// Công thức FPR = Misses / (Hits + Misses)
	// Misses ở đây là: Bloom nói "Có" nhưng tìm trong Map lại "Không có"
	totalBloomPositives := hits + misses
	if totalBloomPositives > 0 {
		stats.FalsePositiveRate = float64(misses) / float64(totalBloomPositives) * 100
	}

	return stats
}
