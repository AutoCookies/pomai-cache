package bloom

import (
	"hash/fnv"
	"sync/atomic"
)

type BloomFilter struct {
	bits []uint64
	size uint64
	k    uint64 // number of hash functions
}

func New(size uint64, k uint64) *BloomFilter {
	numWords := (size + 63) / 64
	return &BloomFilter{
		bits: make([]uint64, numWords),
		size: size,
		k:    k,
	}
}

// Add thêm key vào Bloom Filter
func (b *BloomFilter) Add(key string) {
	h1, h2 := b.getHashes(key)

	for i := uint64(0); i < b.k; i++ {
		// Double Hashing: (h1 + i*h2) % size
		idx := (h1 + (i * h2)) % b.size

		wordIdx := idx / 64
		bitIdx := idx % 64

		mask := uint64(1 << bitIdx)
		atomic.OrUint64(&b.bits[wordIdx], mask)
	}
}

// MayContain kiểm tra sự tồn tại
func (b *BloomFilter) MayContain(key string) bool {
	h1, h2 := b.getHashes(key)

	for i := uint64(0); i < b.k; i++ {
		idx := (h1 + (i * h2)) % b.size

		wordIdx := idx / 64
		bitIdx := idx % 64

		if atomic.LoadUint64(&b.bits[wordIdx])&(1<<bitIdx) == 0 {
			return false
		}
	}
	return true
}

// getHashes tính toán 2 giá trị hash độc lập
func (b *BloomFilter) getHashes(key string) (uint64, uint64) {
	// Hash 1
	h := fnv.New64a()
	h.Write([]byte(key))
	h1 := h.Sum64()

	// Hash 2: Hash lại key với 1 byte salt để đảm bảo khác biệt với h1
	h.Reset()
	h.Write([]byte{59}) // salt tùy ý
	h.Write([]byte(key))
	h2 := h.Sum64()

	// [QUAN TRỌNG] Buộc h2 phải là số lẻ.
	// Nếu size là chẵn (ví dụ 10000) và h2 chẵn, ta chỉ probe được 50% số slot.
	// h2 | 1 đảm bảo h2 luôn lẻ, giảm thiểu xung đột chu kỳ.
	return h1, h2 | 1
}
