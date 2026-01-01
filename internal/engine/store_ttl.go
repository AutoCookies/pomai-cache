package engine

import (
	"container/list"
	"context"
	"sync/atomic"
	"time"

	"github.com/AutoCookies/pomai-cache/packages/ds/bloom"
)

// TTLRemaining kiểm tra thời gian sống còn lại của một key.
// Trả về: (thời gian còn lại, key có tồn tại hay không).
// Nếu key đã hết hạn, nó sẽ kích hoạt Lazy Delete và trả về (0, false).
func (s *Store) TTLRemaining(key string) (time.Duration, bool) {
	if key == "" {
		return 0, false
	}

	// 1. Fast fail với Bloom Filter
	if s.bloom != nil && !s.bloom.MayContain(key) {
		return 0, false
	}

	sh := s.getShard(key)

	// 2. Kiểm tra nhanh bằng Read Lock
	sh.mu.RLock()
	elem, ok := sh.items[key]
	if !ok {
		sh.mu.RUnlock()
		return 0, false
	}

	ent := elem.Value.(*entry)

	// Nếu không có TTL (vĩnh viễn)
	if ent.expireAt == 0 {
		sh.mu.RUnlock()
		return 0, true
	}

	remain := time.Until(time.Unix(0, ent.expireAt))
	sh.mu.RUnlock() // Giải phóng Read Lock ngay

	// 3. Nếu đã hết hạn -> Lazy Delete (Cần Write Lock)
	if remain <= 0 {
		s.deleteExpired(key)
		return 0, false
	}

	return remain, true
}

// deleteExpired là hàm helper xóa key nếu nó thực sự đã hết hạn.
// Hàm này tự quản lý Lock, an toàn để gọi từ các luồng khác.
func (s *Store) deleteExpired(key string) {
	sh := s.getShard(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()

	// Double-check: Kiểm tra lại xem key còn đó và còn hết hạn không
	// (tránh trường hợp luồng khác đã update key này giữa lúc ta chuyển lock)
	if elem, ok := sh.items[key]; ok {
		ent := elem.Value.(*entry)
		if ent.expireAt != 0 && time.Now().UnixNano() > ent.expireAt {
			// Xóa khỏi Map và List
			delete(sh.items, key)
			sh.ll.Remove(elem)

			// Cập nhật bộ nhớ
			sh.bytes -= int64(ent.size)
			atomic.AddInt64(&s.totalBytesAtomic, -int64(ent.size))

			// Giải phóng Global Memory nếu có
			if GlobalMemCtrl != nil {
				GlobalMemCtrl.Release(int64(ent.size))
			}
		}
	}
}

// StartCleanup bắt đầu một Goroutine chạy ngầm để dọn dẹp định kỳ.
func (s *Store) StartCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CleanupExpired()
			}
		}
	}()
}

// CleanupExpired quét toàn bộ cache và xóa các key hết hạn (Active Expiration).
// Trả về số lượng key đã xóa.
func (s *Store) CleanupExpired() int {
	now := time.Now().UnixNano()
	cleaned := 0

	// Duyệt qua từng shard để dọn dẹp
	// Chúng ta lock từng shard một để không chặn toàn bộ hệ thống
	for _, sh := range s.shards {
		sh.mu.Lock()

		// 1. Tìm các key hết hạn trong shard này
		// (Lưu vào slice tạm để tránh lỗi khi vừa duyệt vừa xóa map)
		toDelete := make([]*list.Element, 0)

		for _, elem := range sh.items {
			ent := elem.Value.(*entry)
			if ent.expireAt != 0 && now > ent.expireAt {
				toDelete = append(toDelete, elem)
			}
		}

		// 2. Xóa chúng
		for _, elem := range toDelete {
			ent := elem.Value.(*entry)
			if _, exists := sh.items[ent.key]; exists {
				delete(sh.items, ent.key)
				sh.ll.Remove(elem)

				sh.bytes -= int64(ent.size)
				atomic.AddInt64(&s.totalBytesAtomic, -int64(ent.size))
				if GlobalMemCtrl != nil {
					GlobalMemCtrl.Release(int64(ent.size))
				}
				cleaned++
			}
		}

		sh.mu.Unlock()
	}

	// 3. Tái tạo Bloom Filter nếu xóa quá nhiều
	// Bloom Filter không hỗ trợ xóa, nên nếu xóa nhiều key, nó sẽ bị "bẩn" (tỷ lệ dương tính giả cao).
	// Ta cần xây lại nó để đảm bảo hiệu suất.
	if cleaned > 1000 && s.bloom != nil {
		go s.RebuildBloomFilter()
	}

	return cleaned
}

// RebuildBloomFilter xây dựng lại Bloom Filter từ dữ liệu hiện có.
// Chạy trong background để tránh block.
func (s *Store) RebuildBloomFilter() {
	// Giả định dung lượng Bloom Filter dựa trên số item hiện tại + 10% buffer
	// (Hoặc giữ nguyên cấu hình cũ nếu lưu lại config size/k)

	// Để đơn giản, ta tạo mới bloom filter với kích thước mặc định hoặc config đã lưu
	// Ở đây ta cần access vào struct Bloom cũ để lấy config size/k,
	// nhưng struct bloom.BloomFilter trong pkg/ds ẩn field size/k.
	// -> Cách tốt nhất: Tạo mới dựa trên estimation items hiện tại.

	totalItems := int64(0)
	for _, sh := range s.shards {
		sh.mu.RLock()
		totalItems += int64(len(sh.items))
		sh.mu.RUnlock()
	}

	if totalItems == 0 {
		return
	}

	// Ước tính: size = n * 10 (cho sai số ~1%), k = 7
	newSize := uint64(totalItems * 10)
	if newSize < 1024 {
		newSize = 1024
	}
	newBloom := bloom.New(newSize, 7)

	// Populate dữ liệu vào bloom mới
	for _, sh := range s.shards {
		sh.mu.RLock()
		for key := range sh.items {
			newBloom.Add(key)
		}
		sh.mu.RUnlock()
	}

	// Swap con trỏ (Atomic swap an toàn hơn, nhưng gán trực tiếp cũng ổn vì pointer assignment là atomic trên x64)
	s.bloom = newBloom
}
