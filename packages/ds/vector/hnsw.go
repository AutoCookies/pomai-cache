// File: packages/ds/vector/hnsw.go
package vector

import (
	"container/heap"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
)

const (
	DefaultM              = 16
	DefaultEfConstruction = 200
	DefaultEfSearch       = 50
	DefaultMaxLevel       = 16
	DefaultMLConstant     = 1.0 / math.Ln2
)

type HNSW struct {
	m              int
	maxM           int
	maxM0          int
	efConstruction int
	efSearch       atomic.Int32
	ml             float64
	maxLevel       int
	entryPoint     atomic.Pointer[HNSWNode]
	nodes          sync.Map
	levelGenerator *rand.Rand
	distanceFunc   DistanceFunc
	nodeCount      atomic.Int64
	visitedPool    *sync.Pool
	resultPool     *sync.Pool
	mu             sync.RWMutex
}

type HNSWNode struct {
	id          string
	vector      []float32
	level       int
	connections []sync.Map
	deleted     atomic.Bool
}

type DistanceFunc func(a, b []float32) float32

func NewHNSW(m, efConstruction int, distFunc DistanceFunc) *HNSW {
	if m <= 0 {
		m = DefaultM
	}
	if efConstruction <= 0 {
		efConstruction = DefaultEfConstruction
	}
	if distFunc == nil {
		distFunc = CosineSIMD
	}

	h := &HNSW{
		m:              m,
		maxM:           m,
		maxM0:          m * 2,
		efConstruction: efConstruction,
		ml:             DefaultMLConstant,
		maxLevel:       DefaultMaxLevel,
		levelGenerator: rand.New(rand.NewSource(rand.Int63())),
		distanceFunc:   distFunc,
		visitedPool: &sync.Pool{
			New: func() interface{} {
				return make(map[string]struct{}, 256)
			},
		},
		resultPool: &sync.Pool{
			New: func() interface{} {
				return &resultHeap{data: make([]SearchResult, 0, 64)}
			},
		},
	}

	h.efSearch.Store(int32(DefaultEfSearch))
	return h
}

func (h *HNSW) SetEfSearch(ef int) {
	if ef < 1 {
		ef = 1
	}
	if ef > 1000 {
		ef = 1000
	}
	h.efSearch.Store(int32(ef))
}

func (h *HNSW) GetEfSearch() int {
	return int(h.efSearch.Load())
}

func (h *HNSW) Insert(id string, vector []float32) error {
	if _, exists := h.nodes.Load(id); exists {
		return nil
	}

	level := h.randomLevel()
	node := &HNSWNode{
		id:          id,
		vector:      vector,
		level:       level,
		connections: make([]sync.Map, level+1),
	}

	h.nodes.Store(id, node)
	h.nodeCount.Add(1)

	entryPointPtr := h.entryPoint.Load()
	if entryPointPtr == nil {
		h.entryPoint.Store(node)
		return nil
	}

	entryPoint := entryPointPtr

	nearest := h.searchLayer(vector, entryPoint, 1, level+1, h.efConstruction)

	for lc := level; lc >= 0; lc-- {
		candidates := h.searchLayer(vector, nearest[0].node, h.efConstruction, lc, h.efConstruction)

		m := h.maxM
		if lc == 0 {
			m = h.maxM0
		}

		neighbors := h.selectNeighbors(candidates, m)

		for _, neighbor := range neighbors {
			node.connections[lc].Store(neighbor.node.id, neighbor.node)
		}

		for _, neighbor := range neighbors {
			neighbor.node.connections[lc].Store(node.id, node)

			maxConn := h.maxM
			if lc == 0 {
				maxConn = h.maxM0
			}

			connCount := 0
			neighbor.node.connections[lc].Range(func(_, _ interface{}) bool {
				connCount++
				return true
			})

			if connCount > maxConn {
				neighborVec := neighbor.node.vector
				candidates := make([]SearchResult, 0, connCount)

				neighbor.node.connections[lc].Range(func(key, value interface{}) bool {
					conn := value.(*HNSWNode)
					if !conn.deleted.Load() {
						dist := h.distanceFunc(neighborVec, conn.vector)
						candidates = append(candidates, SearchResult{
							node:     conn,
							distance: dist,
						})
					}
					return true
				})

				pruned := h.selectNeighbors(candidates, maxConn)

				neighbor.node.connections[lc].Range(func(key, _ interface{}) bool {
					neighbor.node.connections[lc].Delete(key)
					return true
				})

				for _, p := range pruned {
					neighbor.node.connections[lc].Store(p.node.id, p.node)
				}
			}
		}

		if len(nearest) > 0 {
			nearest = []SearchResult{nearest[0]}
		}
	}

	currentEntry := h.entryPoint.Load()
	if currentEntry == nil || level > currentEntry.level {
		h.entryPoint.Store(node)
	}

	return nil
}

func (h *HNSW) Search(vector []float32, k int) []SearchResult {
	ef := h.GetEfSearch()
	if k > ef {
		ef = k
	}
	return h.SearchEf(vector, k, ef)
}

func (h *HNSW) SearchEf(vector []float32, k, ef int) []SearchResult {
	entryPoint := h.entryPoint.Load()
	if entryPoint == nil {
		return nil
	}

	if entryPoint.deleted.Load() {
		var newEntry *HNSWNode
		h.nodes.Range(func(key, value interface{}) bool {
			node := value.(*HNSWNode)
			if !node.deleted.Load() {
				if newEntry == nil || node.level > newEntry.level {
					newEntry = node
				}
			}
			return true
		})
		if newEntry != nil {
			h.entryPoint.Store(newEntry)
			entryPoint = newEntry
		} else {
			return nil
		}
	}

	nearest := h.searchLayer(vector, entryPoint, 1, entryPoint.level+1, ef)

	for level := entryPoint.level; level >= 0; level-- {
		nearest = h.searchLayer(vector, nearest[0].node, ef, level, ef)
	}

	if len(nearest) > k {
		nearest = nearest[:k]
	}

	return nearest
}

func (h *HNSW) searchLayer(target []float32, entryPoint *HNSWNode, num, level, ef int) []SearchResult {
	visited := h.visitedPool.Get().(map[string]struct{})
	defer func() {
		for k := range visited {
			delete(visited, k)
		}
		h.visitedPool.Put(visited)
	}()

	candidatesHeap := h.resultPool.Get().(*resultHeap)
	defer func() {
		candidatesHeap.data = candidatesHeap.data[:0]
		h.resultPool.Put(candidatesHeap)
	}()

	wHeap := h.resultPool.Get().(*resultHeap)
	defer func() {
		wHeap.data = wHeap.data[:0]
		h.resultPool.Put(wHeap)
	}()

	heap.Init(candidatesHeap)
	heap.Init(wHeap)

	dist := h.distanceFunc(target, entryPoint.vector)
	heap.Push(candidatesHeap, SearchResult{node: entryPoint, distance: dist})
	heap.Push(wHeap, SearchResult{node: entryPoint, distance: dist})
	visited[entryPoint.id] = struct{}{}

	for candidatesHeap.Len() > 0 {
		current := heap.Pop(candidatesHeap).(SearchResult)

		if wHeap.Len() > 0 && current.distance > wHeap.data[0].distance {
			break
		}

		if current.node.deleted.Load() {
			continue
		}

		if level >= len(current.node.connections) {
			continue
		}

		current.node.connections[level].Range(func(key, value interface{}) bool {
			neighbor := value.(*HNSWNode)

			if _, ok := visited[neighbor.id]; ok {
				return true
			}
			visited[neighbor.id] = struct{}{}

			if neighbor.deleted.Load() {
				return true
			}

			dist := h.distanceFunc(target, neighbor.vector)

			if wHeap.Len() < ef || dist < wHeap.data[0].distance {
				heap.Push(candidatesHeap, SearchResult{node: neighbor, distance: dist})
				heap.Push(wHeap, SearchResult{node: neighbor, distance: dist})

				if wHeap.Len() > ef {
					heap.Pop(wHeap)
				}
			}

			return true
		})
	}

	results := make([]SearchResult, wHeap.Len())
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(wHeap).(SearchResult)
	}

	return results
}

func (h *HNSW) selectNeighbors(candidates []SearchResult, m int) []SearchResult {
	if len(candidates) <= m {
		return candidates
	}

	pq := h.resultPool.Get().(*resultHeap)
	defer func() {
		pq.data = pq.data[:0]
		h.resultPool.Put(pq)
	}()

	heap.Init(pq)

	for _, c := range candidates {
		if !c.node.deleted.Load() {
			heap.Push(pq, c)
		}
	}

	result := make([]SearchResult, 0, m)
	for pq.Len() > 0 && len(result) < m {
		result = append(result, heap.Pop(pq).(SearchResult))
	}

	return result
}

func (h *HNSW) randomLevel() int {
	h.mu.RLock()
	level := 0
	for level < h.maxLevel && h.levelGenerator.Float64() < h.ml {
		level++
	}
	h.mu.RUnlock()
	return level
}

func (h *HNSW) Delete(id string) bool {
	value, exists := h.nodes.Load(id)
	if !exists {
		return false
	}

	node := value.(*HNSWNode)
	node.deleted.Store(true)

	h.nodeCount.Add(-1)

	for level := 0; level < len(node.connections); level++ {
		node.connections[level].Range(func(key, value interface{}) bool {
			neighbor := value.(*HNSWNode)
			neighbor.connections[level].Delete(id)
			return true
		})
	}

	h.nodes.Delete(id)

	return true
}

func (h *HNSW) Size() int {
	return int(h.nodeCount.Load())
}

type SearchResult struct {
	node     *HNSWNode
	distance float32
}

func (r SearchResult) ID() string {
	if r.node == nil {
		return ""
	}
	return r.node.id
}

func (r SearchResult) Distance() float32 {
	return r.distance
}

type resultHeap struct {
	data []SearchResult
}

func (h *resultHeap) Len() int           { return len(h.data) }
func (h *resultHeap) Less(i, j int) bool { return h.data[i].distance > h.data[j].distance }
func (h *resultHeap) Swap(i, j int)      { h.data[i], h.data[j] = h.data[j], h.data[i] }

func (h *resultHeap) Push(x interface{}) {
	h.data = append(h.data, x.(SearchResult))
}

func (h *resultHeap) Pop() interface{} {
	old := h.data
	n := len(old)
	x := old[n-1]
	h.data = old[0 : n-1]
	return x
}
