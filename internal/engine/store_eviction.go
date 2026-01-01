package engine

import (
	"math/rand"
	"sync/atomic"
)

// candidate được dùng tạm thời trong thuật toán lấy mẫu (sampling)
// để so sánh độ ưu tiên giữa các item.
type candidate struct {
	shardIdx int
	key      string
	priority int64
	size     int
}

// evictIfNeeded kiểm tra dung lượng và kích hoạt vòng lặp xóa (eviction loop) nếu cần.
// Nó sử dụng thuật toán xấp xỉ LRU (Approximated LRU) bằng cách lấy mẫu ngẫu nhiên
// để tránh việc phải lock toàn bộ cache hoặc duy trì một danh sách LRU khổng lồ.
func (s *Store) evictIfNeeded(startShard int) {
	// 1. Kiểm tra điều kiện nhanh (không cần lock)
	if s.capacityBytes <= 0 {
		return
	}
	if atomic.LoadInt64(&s.totalBytesAtomic) <= s.capacityBytes {
		return
	}

	shardCount := int(s.shardCount)
	start := startShard % shardCount
	if start < 0 {
		start = 0
	}

	// Cấu hình lấy mẫu
	const sampleShardLimit = 16 // Chỉ quét tối đa 16 shard mỗi lần để tránh lag
	const perShardSamples = 2   // Lấy 2 phần tử cuối mỗi shard
	const maxCandidates = 128   // Dung lượng pool ứng viên tối đa

	// Vòng lặp xóa cho đến khi dung lượng xuống dưới mức cho phép
	for atomic.LoadInt64(&s.totalBytesAtomic) > s.capacityBytes {
		cands := make([]candidate, 0, 32)

		// A. Giai đoạn thu thập ứng viên (Candidates Collection)
		for i := 0; i < sampleShardLimit; i++ {
			idx := (start + i) % shardCount
			sh := s.shards[idx]

			// Dùng Read Lock để lấy mẫu nhanh mà không chặn ghi
			sh.mu.RLock()
			elem := sh.ll.Back() // Lấy từ đuôi (LRU)
			count := 0
			for elem != nil && count < perShardSamples {
				ent := elem.Value.(*entry)

				// Tính điểm ưu tiên (Priority Score)
				// Công thức: Thời gian truy cập cuối + (Số lần truy cập * Trọng số)
				// Item nào có điểm thấp nhất => Ít quan trọng nhất => Sẽ bị xóa
				priority := ent.lastAccess + int64(atomic.LoadUint64(&ent.accesses))*atomic.LoadInt64(&s.freqBoost)

				cands = append(cands, candidate{
					shardIdx: idx,
					key:      ent.key,
					priority: priority,
					size:     ent.size,
				})

				elem = elem.Prev()
				count++
				if len(cands) >= maxCandidates {
					break
				}
			}
			sh.mu.RUnlock()

			if len(cands) >= maxCandidates {
				break
			}
		}

		if len(cands) == 0 {
			// Không tìm thấy gì để xóa (Cache rỗng?), break để tránh lặp vô tận
			break
		}

		// B. Giai đoạn chọn nạn nhân (Victim Selection)
		// Tìm item có priority thấp nhất trong danh sách ứng viên
		best := cands[0]
		for _, c := range cands[1:] {
			if c.priority < best.priority {
				best = c
			}
		}

		// C. Giai đoạn hành quyết (Execution)
		// Gọi hàm evictKey để xóa an toàn (có Write Lock)
		freed := s.evictKey(best.key)

		// Nếu không xóa được (do key đã bị xóa bởi luồng khác), thử shard tiếp theo
		if freed == 0 {
			start = (start + 1) % shardCount
			continue
		}

		// Xoay vòng shard bắt đầu cho lần quét tới để công bằng
		start = (start + 1) % shardCount
	}
}

// evictKey xóa một key cụ thể và cập nhật lại bộ đếm bộ nhớ.
// Trả về số bytes đã giải phóng (0 nếu không tìm thấy).
func (s *Store) evictKey(key string) int {
	sh := s.getShard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if elem, ok := sh.items[key]; ok {
		ent := elem.Value.(*entry)

		// Xóa khỏi map và list
		delete(sh.items, key)
		sh.ll.Remove(elem)

		// Cập nhật metrics
		size := ent.size
		sh.bytes -= int64(size)
		atomic.AddInt64(&s.totalBytesAtomic, -int64(size))
		atomic.AddUint64(&s.evictions, 1)

		// Trả bộ nhớ về Global Controller (nếu có)
		if GlobalMemCtrl != nil {
			GlobalMemCtrl.Release(int64(size))
		}
		return size
	}
	return 0
}

// admitOrRejectTinyLFU thực hiện logic "Admission Control".
// Nó so sánh tần suất (Frequency) của item mới đang muốn vào
// với tần suất của các item đang có trong cache.
// Trả về: true (Cho vào), false (Từ chối/Reject).
func (s *Store) admitOrRejectTinyLFU(key string, newSize int) bool {
	// Nếu không giới hạn dung lượng hoặc chưa đầy => Luôn cho vào
	if s.capacityBytes == 0 {
		return true
	}
	if atomic.LoadInt64(&s.totalBytesAtomic)+int64(newSize) <= s.capacityBytes {
		return true
	}

	// Nếu không có bộ đếm tần suất (Sketch), fallback về behavior cũ (cho vào rồi evict sau)
	if GlobalFreq == nil {
		return true
	}

	// 1. Ước tính tần suất của Key mới
	// +1 vì đây là lần truy cập hiện tại
	newFreq := GlobalFreq.Estimate(key) + 1

	// 2. Lấy mẫu một "nạn nhân" tiềm năng trong cache để so sánh
	// Thử tối đa 3 lần để tìm một nạn nhân yếu thế hơn
	attempts := 3
	for i := 0; i < attempts; i++ {
		victimKey, _, victimFreq := s.sampleVictim()

		if victimKey == "" {
			// Cache rỗng hoặc lỗi lấy mẫu => Cho vào
			return true
		}

		// LOGIC CỐT LÕI CỦA TINY-LFU:
		// Nếu thằng mới (newFreq) "hot" hơn thằng cũ (victimFreq)
		// => Đuổi thằng cũ đi để nhường chỗ.
		if newFreq >= victimFreq {
			freed := s.evictKey(victimKey)
			if freed > 0 {
				// Nếu xóa xong mà đủ chỗ => Return true ngay
				if atomic.LoadInt64(&s.totalBytesAtomic)+int64(newSize) <= s.capacityBytes {
					return true
				}
				// Nếu chưa đủ chỗ, lặp tiếp để tìm nạn nhân khác
				continue
			}
			// Key biến mất giữa chừng (race condition), thử lại
			continue
		}

		// Nếu thằng mới "kém fame" hơn thằng cũ => REJECT ngay lập tức.
		// Bảo vệ cache khỏi bị "nhiễm độc" bởi dữ liệu dùng 1 lần (One-hit wonders).
		return false
	}

	// Nếu sau khi nỗ lực xóa mà vẫn không đủ chỗ => Reject
	if atomic.LoadInt64(&s.totalBytesAtomic)+int64(newSize) > s.capacityBytes {
		return false
	}

	return true
}

// sampleVictim lấy ngẫu nhiên một item trong cache và trả về tần suất của nó.
// Trả về: (key, size, frequency)
func (s *Store) sampleVictim() (string, int, uint32) {
	shardCount := int(s.shardCount)
	if shardCount == 0 {
		return "", 0, 0
	}

	const sampleShardLimit = 8
	const perShardSamples = 1

	var bestKey string
	var bestSize int
	var bestFreq uint32 = ^uint32(0) // Max Uint32

	// Lấy mẫu ngẫu nhiên vài shard
	for i := 0; i < sampleShardLimit; i++ {
		idx := rand.Intn(shardCount)
		sh := s.shards[idx]

		sh.mu.RLock()
		elem := sh.ll.Back() // Ưu tiên lấy từ đuôi (LRU)
		count := 0
		for elem != nil && count < perShardSamples {
			ent := elem.Value.(*entry)
			var freq uint32
			if GlobalFreq != nil {
				freq = GlobalFreq.Estimate(ent.key)
			}
			// Tìm thằng có tần suất thấp nhất trong nhóm mẫu
			if freq < bestFreq {
				bestFreq = freq
				bestKey = ent.key
				bestSize = ent.size
			}
			elem = elem.Prev()
			count++
		}
		sh.mu.RUnlock()
	}

	if bestKey == "" {
		return "", 0, 0
	}
	return bestKey, bestSize, bestFreq
}

// ForceEvictBytes bắt buộc giải phóng một lượng bộ nhớ cụ thể (targetBytes).
// Hàm này thường được gọi bởi MemoryController khi hệ thống thiếu RAM toàn cục.
func (s *Store) ForceEvictBytes(targetBytes int64) int64 {
	if targetBytes <= 0 {
		return 0
	}
	freed := int64(0)
	shardCount := int(s.shardCount)

	// Duyệt qua các shard và xóa thằng cũ nhất (Back of List)
	// cho đến khi đủ chỉ tiêu.
	for i := 0; i < shardCount && freed < targetBytes; i++ {
		sh := s.shards[i]
		sh.mu.Lock()

		for elem := sh.ll.Back(); elem != nil && freed < targetBytes; {
			ent := elem.Value.(*entry)
			prev := elem.Prev() // Lưu con trỏ trước khi xóa

			delete(sh.items, ent.key)
			sh.ll.Remove(elem)

			size := int64(ent.size)
			sh.bytes -= size
			atomic.AddInt64(&s.totalBytesAtomic, -size)
			atomic.AddUint64(&s.evictions, 1)

			freed += size
			elem = prev
		}
		sh.mu.Unlock()
	}

	// Báo cáo lại cho Global Controller số lượng thực tế đã xóa được
	if freed > 0 && GlobalMemCtrl != nil {
		GlobalMemCtrl.Release(freed)
	}
	return freed
}
