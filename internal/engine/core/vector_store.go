// File: internal/engine/core/vector_store.go
package core

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/AutoCookies/pomai-cache/packages/ds/vector"
)

const (
	// Số lượng Shard: 64 là con số "vàng" để cân bằng giữa
	// lock contention và CPU cache locality trên các máy 2-8 cores.
	ShardCount = 64

	// Giới hạn an toàn để tránh tràn RAM trên laptop 8GB-16GB
	// 500,000 vectors x 256 dims x 4 bytes ~ 500MB RAM (chưa tính overhead HNSW)
	DefaultMaxVectors = 500_000
)

type VectorStore struct {
	shards []*VectorShard

	// Cấu hình toàn cục (Adaptive Settings)
	globalEfSearch int
	maxVectors     int64

	// Metrics
	totalVectors atomic.Int64
}

type VectorShard struct {
	mu      sync.RWMutex
	indices map[string]*VectorIndex
}

type VectorIndex struct {
	name      string
	dimension int
	distType  string
	hnsw      *vector.HNSW

	// Metadata được tối ưu: dùng map thường + Shard Lock
	// (tiết kiệm RAM hơn sync.Map rất nhiều)
	metadata map[string]map[string]interface{}
}

func NewVectorStore() *VectorStore {
	// Tự động điều chỉnh EfSearch dựa trên số CPU
	// CPU yếu -> Ef thấp để nhanh (chấp nhận giảm độ chính xác 1 xíu)
	ef := 64
	if runtime.NumCPU() > 4 {
		ef = 100
	}

	vs := &VectorStore{
		shards:         make([]*VectorShard, ShardCount),
		globalEfSearch: ef,
		maxVectors:     DefaultMaxVectors,
	}

	for i := 0; i < ShardCount; i++ {
		vs.shards[i] = &VectorShard{
			indices: make(map[string]*VectorIndex),
		}
	}
	return vs
}

// Adaptive Tuning: Cho phép chỉnh giới hạn RAM nóng
func (vs *VectorStore) SetMaxVectors(max int64) {
	atomic.StoreInt64(&vs.maxVectors, max)
}

func (vs *VectorStore) SetGlobalEfSearch(ef int) {
	vs.globalEfSearch = ef
	// Update hot cho các index đang chạy
	for _, shard := range vs.shards {
		shard.mu.RLock()
		for _, idx := range shard.indices {
			idx.hnsw.SetEfSearch(ef)
		}
		shard.mu.RUnlock()
	}
}

// Hash function cực nhanh để định tuyến Shard
func (vs *VectorStore) getShard(indexName string) *VectorShard {
	h := fnv.New32a()
	h.Write([]byte(indexName))
	return vs.shards[h.Sum32()%uint32(ShardCount)]
}

func (vs *VectorStore) HasIndex(name string) bool {
	shard := vs.getShard(name)
	shard.mu.RLock()
	_, exists := shard.indices[name]
	shard.mu.RUnlock()
	return exists
}

func (vs *VectorStore) CreateIndex(name string, dimension int, distanceType string) error {
	shard := vs.getShard(name)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, exists := shard.indices[name]; exists {
		return fmt.Errorf("index already exists: %s", name)
	}

	var distFunc vector.DistanceFunc
	switch distanceType {
	case "cosine":
		distFunc = vector.CosineSIMD
	case "euclidean":
		distFunc = vector.EuclideanSIMD
	case "dot":
		distFunc = vector.DotProductSIMD
	default:
		distFunc = vector.CosineSIMD
	}

	// ADAPTIVE CONFIG:
	// Với máy yếu (2 core), ta giảm M xuống 10 (mặc định 16-32)
	// Giúp insert nhanh hơn và tốn ít RAM hơn cho các kết nối
	m := 16
	efConstruction := 100
	if runtime.NumCPU() <= 2 {
		m = 10
		efConstruction = 60
	}

	index := &VectorIndex{
		name:      name,
		dimension: dimension,
		distType:  distanceType,
		hnsw:      vector.NewHNSW(m, efConstruction, distFunc),
		metadata:  make(map[string]map[string]interface{}),
	}
	index.hnsw.SetEfSearch(vs.globalEfSearch)

	shard.indices[name] = index
	return nil
}

func (vs *VectorStore) Insert(indexName, id string, vec []float32, metadata map[string]interface{}) error {
	// 1. Kiểm tra giới hạn RAM (Adaptive Protection)
	currentTotal := vs.totalVectors.Load()
	limit := atomic.LoadInt64(&vs.maxVectors)
	if currentTotal >= limit {
		// Ở bản Big Tech xịn, ta sẽ kích hoạt Eviction ở đây.
		// Tạm thời trả lỗi để bảo vệ server khỏi Crash.
		return fmt.Errorf("vector store full (max %d)", limit)
	}

	shard := vs.getShard(indexName)

	// Dùng RLock để tìm Index (Nhanh)
	shard.mu.RLock()
	index, exists := shard.indices[indexName]
	shard.mu.RUnlock()

	if !exists {
		return fmt.Errorf("index not found: %s", indexName)
	}

	if len(vec) != index.dimension {
		return fmt.Errorf("dim mismatch: want %d, got %d", index.dimension, len(vec))
	}

	// 2. Insert vào HNSW
	// HNSW tự handle concurrency nội bộ, nên không cần Lock to ở đây
	if err := index.hnsw.Insert(id, vec); err != nil {
		return err
	}

	// 3. Insert Metadata (Chỉ khi cần thiết)
	if metadata != nil && len(metadata) > 0 {
		// Ta cần Lock Shard để ghi metadata an toàn.
		// Để tối ưu, ta có thể dùng sync.Map bên trong Index,
		// nhưng map thường + Shard Lock tiết kiệm RAM hơn.
		// Vì metadata write ít hơn read vector, ta lock shard 1 xíu không sao.
		shard.mu.Lock()
		index.metadata[id] = metadata
		shard.mu.Unlock()
	}

	vs.totalVectors.Add(1)
	return nil
}

func (vs *VectorStore) Search(indexName string, query []float32, k int) ([]VectorSearchResult, error) {
	shard := vs.getShard(indexName)

	shard.mu.RLock()
	index, exists := shard.indices[indexName]
	// Giữ RLock trong khi copy metadata để đảm bảo an toàn tuyệt đối
	// (Tuy nhiên để tối ưu search HNSW - vốn lâu - ta nên unlock sớm)

	// Chiến thuật: Lấy index ra, unlock shard, search HNSW, sau đó RLock lại để lấy metadata
	// Điều này giúp CPU không bị chặn khi tính toán vector.
	shard.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("index not found: %s", indexName)
	}

	if len(query) != index.dimension {
		return nil, fmt.Errorf("dim mismatch")
	}

	// HEAVY CALCULATION (Không giữ lock ở đây -> Adaptive Parallelism)
	results := index.hnsw.Search(query, k)

	// Lấy Metadata (Nhanh)
	output := make([]VectorSearchResult, len(results))

	shard.mu.RLock() // Lock lại để đọc map
	defer shard.mu.RUnlock()

	for i, r := range results {
		var meta map[string]interface{}
		// Copy metadata để tránh race condition nếu caller sửa map
		if m, ok := index.metadata[r.ID()]; ok {
			// Shallow copy map
			meta = make(map[string]interface{}, len(m))
			for k, v := range m {
				meta[k] = v
			}
		}

		output[i] = VectorSearchResult{
			ID:       r.ID(),
			Distance: r.Distance(),
			Metadata: meta,
		}
	}

	return output, nil
}

func (vs *VectorStore) Delete(indexName, id string) error {
	shard := vs.getShard(indexName)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	index, exists := shard.indices[indexName]
	if !exists {
		return fmt.Errorf("index not found")
	}

	index.hnsw.Delete(id)
	delete(index.metadata, id)

	vs.totalVectors.Add(-1)
	return nil
}

func (vs *VectorStore) DropIndex(name string) error {
	shard := vs.getShard(name)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if index, exists := shard.indices[name]; exists {
		count := index.hnsw.Size()
		delete(shard.indices, name)
		vs.totalVectors.Add(int64(-count))

		// Giúp GC dọn dẹp nhanh
		index.hnsw = nil
		index.metadata = nil
	}
	return nil
}

func (vs *VectorStore) ListIndices() []string {
	var names []string
	for _, shard := range vs.shards {
		shard.mu.RLock()
		for name := range shard.indices {
			names = append(names, name)
		}
		shard.mu.RUnlock()
	}
	return names
}

func (vs *VectorStore) IndexStats(name string) (map[string]interface{}, error) {
	shard := vs.getShard(name)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	index, exists := shard.indices[name]
	if !exists {
		return nil, fmt.Errorf("index not found")
	}

	return map[string]interface{}{
		"name":           index.name,
		"dimension":      index.dimension,
		"distance":       index.distType,
		"size":           index.hnsw.Size(),
		"metadata_count": len(index.metadata),
		"ef_search":      index.hnsw.GetEfSearch(),
	}, nil
}

type VectorSearchResult struct {
	ID       string
	Distance float32
	Metadata map[string]interface{}
}

func (r VectorSearchResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"id":       r.ID,
		"distance": r.Distance,
		"metadata": r.Metadata,
	})
}
