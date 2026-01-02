// File: packages/ds/bloom/bloom.go
package bloom

import (
	"fmt"
	"hash"
	"hash/fnv"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	cacheLineSize = 64
	bitsPerWord   = 64
)

type BloomFilter struct {
	bits []uint64
	size uint64
	k    uint64

	_ [cacheLineSize - unsafe.Sizeof(uint64(0))*3]byte

	count atomic.Uint64

	hashPool sync.Pool
}

func New(size uint64, k uint64) *BloomFilter {
	numWords := (size + bitsPerWord - 1) / bitsPerWord

	bf := &BloomFilter{
		bits: make([]uint64, numWords),
		size: size,
		k:    k,
	}

	bf.hashPool = sync.Pool{
		New: func() interface{} {
			return fnv.New64a()
		},
	}

	return bf
}

func NewOptimal(expectedItems uint64, falsePositiveRate float64) *BloomFilter {
	size := optimalSize(expectedItems, falsePositiveRate)
	k := optimalK(size, expectedItems)
	return New(size, k)
}

func optimalSize(n uint64, p float64) uint64 {
	m := -float64(n) * math.Log(p) / (math.Log(2) * math.Log(2))
	return uint64(math.Ceil(m))
}

func optimalK(m, n uint64) uint64 {
	k := float64(m) / float64(n) * math.Log(2)
	return uint64(math.Ceil(k))
}

func (b *BloomFilter) Add(key string) {
	h1, h2 := b.getHashesFast(key)

	for i := uint64(0); i < b.k; i++ {
		idx := (h1 + i*h2) % b.size

		wordIdx := idx >> 6
		bitIdx := idx & 63

		mask := uint64(1) << bitIdx
		atomic.OrUint64(&b.bits[wordIdx], mask)
	}

	b.count.Add(1)
}

func (b *BloomFilter) AddBatch(keys []string) {
	for _, key := range keys {
		b.Add(key)
	}
}

func (b *BloomFilter) MayContain(key string) bool {
	h1, h2 := b.getHashesFast(key)

	for i := uint64(0); i < b.k; i++ {
		idx := (h1 + i*h2) % b.size

		wordIdx := idx >> 6
		bitIdx := idx & 63

		if atomic.LoadUint64(&b.bits[wordIdx])&(uint64(1)<<bitIdx) == 0 {
			return false
		}
	}

	return true
}

func (b *BloomFilter) MustContain(key string) bool {
	return b.MayContain(key)
}

func (b *BloomFilter) MayContainBatch(keys []string) []bool {
	results := make([]bool, len(keys))
	for i, key := range keys {
		results[i] = b.MayContain(key)
	}
	return results
}

func (b *BloomFilter) Remove(key string) {
}

func (b *BloomFilter) getHashesFast(key string) (uint64, uint64) {
	h := b.hashPool.Get().(hash.Hash64)
	defer b.hashPool.Put(h)

	h.Reset()
	h.Write(unsafeStringToBytes(key))
	h1 := h.Sum64()

	h.Reset()
	h.Write([]byte{59})
	h.Write(unsafeStringToBytes(key))
	h2 := h.Sum64()

	return h1, h2 | 1
}

func (b *BloomFilter) getHashes(key string) (uint64, uint64) {
	h := fnv.New64a()
	h.Write([]byte(key))
	h1 := h.Sum64()

	h.Reset()
	h.Write([]byte{59})
	h.Write([]byte(key))
	h2 := h.Sum64()

	return h1, h2 | 1
}

func (b *BloomFilter) Clear() {
	for i := range b.bits {
		atomic.StoreUint64(&b.bits[i], 0)
	}
	b.count.Store(0)
}

func (b *BloomFilter) Count() uint64 {
	return b.count.Load()
}

func (b *BloomFilter) FillRatio() float64 {
	var setBits uint64
	for i := range b.bits {
		word := atomic.LoadUint64(&b.bits[i])
		setBits += uint64(popcount(word))
	}
	return float64(setBits) / float64(b.size)
}

func (b *BloomFilter) EstimatedFalsePositiveRate() float64 {
	fillRatio := b.FillRatio()
	return math.Pow(fillRatio, float64(b.k))
}

func (b *BloomFilter) Merge(other *BloomFilter) error {
	if b.size != other.size || b.k != other.k {
		return fmt.Errorf("bloom filters must have same size and k")
	}

	for i := range b.bits {
		for {
			oldVal := atomic.LoadUint64(&b.bits[i])
			otherVal := atomic.LoadUint64(&other.bits[i])
			newVal := oldVal | otherVal

			if atomic.CompareAndSwapUint64(&b.bits[i], oldVal, newVal) {
				break
			}
		}
	}

	b.count.Add(other.count.Load())
	return nil
}

func (b *BloomFilter) Intersect(other *BloomFilter) (*BloomFilter, error) {
	if b.size != other.size || b.k != other.k {
		return nil, fmt.Errorf("bloom filters must have same size and k")
	}

	result := New(b.size, b.k)

	for i := range b.bits {
		val1 := atomic.LoadUint64(&b.bits[i])
		val2 := atomic.LoadUint64(&other.bits[i])
		result.bits[i] = val1 & val2
	}

	return result, nil
}

func (b *BloomFilter) Clone() *BloomFilter {
	clone := &BloomFilter{
		bits: make([]uint64, len(b.bits)),
		size: b.size,
		k:    b.k,
	}

	for i := range b.bits {
		clone.bits[i] = atomic.LoadUint64(&b.bits[i])
	}

	clone.count.Store(b.count.Load())

	clone.hashPool = sync.Pool{
		New: func() interface{} {
			return fnv.New64a()
		},
	}

	return clone
}

func (b *BloomFilter) Export() []byte {
	data := make([]byte, len(b.bits)*8)
	for i := range b.bits {
		val := atomic.LoadUint64(&b.bits[i])
		offset := i * 8
		data[offset+0] = byte(val)
		data[offset+1] = byte(val >> 8)
		data[offset+2] = byte(val >> 16)
		data[offset+3] = byte(val >> 24)
		data[offset+4] = byte(val >> 32)
		data[offset+5] = byte(val >> 40)
		data[offset+6] = byte(val >> 48)
		data[offset+7] = byte(val >> 56)
	}
	return data
}

func (b *BloomFilter) Import(data []byte) error {
	if len(data) != len(b.bits)*8 {
		return fmt.Errorf("invalid data size")
	}

	for i := range b.bits {
		offset := i * 8
		val := uint64(data[offset+0]) |
			uint64(data[offset+1])<<8 |
			uint64(data[offset+2])<<16 |
			uint64(data[offset+3])<<24 |
			uint64(data[offset+4])<<32 |
			uint64(data[offset+5])<<40 |
			uint64(data[offset+6])<<48 |
			uint64(data[offset+7])<<56
		atomic.StoreUint64(&b.bits[i], val)
	}

	return nil
}

func (b *BloomFilter) Stats() map[string]interface{} {
	fillRatio := b.FillRatio()

	return map[string]interface{}{
		"size":          b.size,
		"k":             b.k,
		"count":         b.count.Load(),
		"fill_ratio":    fillRatio,
		"estimated_fpr": b.EstimatedFalsePositiveRate(),
		"memory_bytes":  len(b.bits) * 8,
		"bits_per_item": float64(b.size) / float64(b.count.Load()+1),
	}
}

func unsafeStringToBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func popcount(x uint64) int {
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f
	return int((x * 0x0101010101010101) >> 56)
}
