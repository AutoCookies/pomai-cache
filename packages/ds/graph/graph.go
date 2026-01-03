package graph

import (
	"container/heap"
	"fmt"
	"sync"
)

const (
	shardCount = 256
	shardMask  = shardCount - 1
	offset64   = 14695981039346656037
	prime64    = 1099511628211
)

type Graph struct {
	shards []*graphShard
}

type graphShard struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	edges map[string]map[string]*Edge
	pad   [64]byte
}

type Node struct {
	ID         string
	Properties map[string]interface{}
}

type Edge struct {
	From   string
	To     string
	Type   string
	Weight float64
	Props  map[string]interface{}
}

type PathResult struct {
	Path  []string
	Cost  float64
	Edges []*Edge
}

func NewGraph() *Graph {
	g := &Graph{
		shards: make([]*graphShard, shardCount),
	}
	for i := 0; i < shardCount; i++ {
		g.shards[i] = &graphShard{
			nodes: make(map[string]*Node),
			edges: make(map[string]map[string]*Edge),
		}
	}
	return g
}

func (g *Graph) getShard(key string) *graphShard {
	var hash uint64 = offset64
	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= prime64
	}
	return g.shards[hash&shardMask]
}

func (g *Graph) AddNode(id string, properties map[string]interface{}) error {
	if properties == nil {
		properties = make(map[string]interface{})
	}
	shard := g.getShard(id)
	shard.mu.Lock()
	shard.nodes[id] = &Node{
		ID:         id,
		Properties: properties,
	}
	if shard.edges[id] == nil {
		shard.edges[id] = make(map[string]*Edge)
	}
	shard.mu.Unlock()
	return nil
}

func (g *Graph) GetNode(id string) (*Node, bool) {
	shard := g.getShard(id)
	shard.mu.RLock()
	node, exists := shard.nodes[id]
	shard.mu.RUnlock()
	return node, exists
}

func (g *Graph) UpdateNode(id string, properties map[string]interface{}) error {
	shard := g.getShard(id)
	shard.mu.Lock()
	node, exists := shard.nodes[id]
	if !exists {
		shard.mu.Unlock()
		return fmt.Errorf("node not found: %s", id)
	}
	for k, v := range properties {
		node.Properties[k] = v
	}
	shard.mu.Unlock()
	return nil
}

func (g *Graph) DeleteNode(id string) error {
	shard := g.getShard(id)
	shard.mu.Lock()
	delete(shard.nodes, id)
	delete(shard.edges, id)
	shard.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(len(g.shards))
	for _, s := range g.shards {
		go func(s *graphShard) {
			defer wg.Done()
			s.mu.Lock()
			for _, edges := range s.edges {
				if _, ok := edges[id]; ok {
					delete(edges, id)
				}
			}
			s.mu.Unlock()
		}(s)
	}
	wg.Wait()
	return nil
}

func (g *Graph) AddEdge(from, to, edgeType string, weight float64, props map[string]interface{}) error {
	shardFrom := g.getShard(from)

	shardFrom.mu.RLock()
	_, existsFrom := shardFrom.nodes[from]
	shardFrom.mu.RUnlock()
	if !existsFrom {
		return fmt.Errorf("source node not found: %s", from)
	}

	shardTo := g.getShard(to)
	shardTo.mu.RLock()
	_, existsTo := shardTo.nodes[to]
	shardTo.mu.RUnlock()
	if !existsTo {
		return fmt.Errorf("target node not found: %s", to)
	}

	if props == nil {
		props = make(map[string]interface{})
	}

	shardFrom.mu.Lock()
	if shardFrom.edges[from] == nil {
		shardFrom.edges[from] = make(map[string]*Edge)
	}
	shardFrom.edges[from][to] = &Edge{
		From:   from,
		To:     to,
		Type:   edgeType,
		Weight: weight,
		Props:  props,
	}
	shardFrom.mu.Unlock()
	return nil
}

func (g *Graph) GetEdge(from, to string) (*Edge, bool) {
	shard := g.getShard(from)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	if edges, exists := shard.edges[from]; exists {
		edge, found := edges[to]
		return edge, found
	}
	return nil, false
}

func (g *Graph) DeleteEdge(from, to string) error {
	shard := g.getShard(from)
	shard.mu.Lock()
	if edges, exists := shard.edges[from]; exists {
		delete(edges, to)
	}
	shard.mu.Unlock()
	return nil
}

func (g *Graph) GetNeighbors(id string, depth int, edgeType string) []string {
	visited := make(map[string]bool)
	result := make([]string, 0)
	g.bfsNeighbors(id, depth, edgeType, visited, &result, 0)
	return result
}

func (g *Graph) bfsNeighbors(current string, maxDepth int, edgeType string, visited map[string]bool, result *[]string, currentDepth int) {
	if currentDepth >= maxDepth {
		return
	}
	visited[current] = true

	shard := g.getShard(current)
	shard.mu.RLock()

	var neighbors []string
	if edges, exists := shard.edges[current]; exists {
		neighbors = make([]string, 0, len(edges))
		for _, edge := range edges {
			if edgeType == "" || edge.Type == edgeType {
				neighbors = append(neighbors, edge.To)
			}
		}
	}
	shard.mu.RUnlock()

	for _, to := range neighbors {
		if !visited[to] {
			*result = append(*result, to)
			g.bfsNeighbors(to, maxDepth, edgeType, visited, result, currentDepth+1)
		}
	}
}

func (g *Graph) ShortestPath(from, to string, maxDepth int) *PathResult {
	if maxDepth <= 0 {
		maxDepth = 100
	}

	dist := make(map[string]float64)
	prev := make(map[string]string)
	edgeMap := make(map[string]*Edge)
	pq := &priorityQueue{}
	heap.Init(pq)

	dist[from] = 0
	heap.Push(pq, &item{id: from, priority: 0})

	visited := make(map[string]bool)

	for pq.Len() > 0 {
		current := heap.Pop(pq).(*item)
		if current.id == to {
			break
		}
		if current.priority > dist[current.id] {
			continue
		}
		visited[current.id] = true

		shard := g.getShard(current.id)
		shard.mu.RLock()

		var outgoing []*Edge
		if edges, ok := shard.edges[current.id]; ok {
			outgoing = make([]*Edge, 0, len(edges))
			for _, e := range edges {
				outgoing = append(outgoing, e)
			}
		}
		shard.mu.RUnlock()

		for _, edge := range outgoing {
			if visited[edge.To] {
				continue
			}
			newDist := dist[current.id] + edge.Weight
			if d, ok := dist[edge.To]; !ok || newDist < d {
				dist[edge.To] = newDist
				prev[edge.To] = current.id
				edgeMap[edge.To] = edge
				heap.Push(pq, &item{id: edge.To, priority: newDist})
			}
		}
	}

	if _, ok := dist[to]; !ok {
		return nil
	}

	path := make([]string, 0)
	edges := make([]*Edge, 0)
	for at := to; at != ""; at = prev[at] {
		path = append([]string{at}, path...)
		if edge, ok := edgeMap[at]; ok {
			edges = append([]*Edge{edge}, edges...)
		}
		if at == from {
			break
		}
	}

	return &PathResult{
		Path:  path,
		Cost:  dist[to],
		Edges: edges,
	}
}

func (g *Graph) PageRank(iterations int, dampingFactor float64) map[string]float64 {
	if iterations <= 0 {
		iterations = 20
	}
	if dampingFactor <= 0 || dampingFactor >= 1 {
		dampingFactor = 0.85
	}

	nodeCount := g.NodeCount()
	if nodeCount == 0 {
		return nil
	}

	ranks := make(map[string]float64)
	outDegree := make(map[string]int)
	allNodes := make([]string, 0, nodeCount)

	for _, shard := range g.shards {
		shard.mu.RLock()
		for id := range shard.nodes {
			allNodes = append(allNodes, id)
			ranks[id] = 1.0 / float64(nodeCount)
			if edges, ok := shard.edges[id]; ok {
				outDegree[id] = len(edges)
			} else {
				outDegree[id] = 0
			}
		}
		shard.mu.RUnlock()
	}

	for iter := 0; iter < iterations; iter++ {
		newRanks := make(map[string]float64)
		baseRank := (1.0 - dampingFactor) / float64(nodeCount)

		for _, id := range allNodes {
			newRanks[id] = baseRank
		}

		for _, shard := range g.shards {
			shard.mu.RLock()
			for from, edges := range shard.edges {
				degree := outDegree[from]
				if degree == 0 {
					continue
				}
				contribution := dampingFactor * ranks[from] / float64(degree)
				for to := range edges {
					newRanks[to] += contribution
				}
			}
			shard.mu.RUnlock()
		}
		ranks = newRanks
	}
	return ranks
}

func (g *Graph) GetSubgraph(nodeIDs []string) *Graph {
	subgraph := NewGraph()
	nodeSet := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		nodeSet[id] = true
	}

	for _, id := range nodeIDs {
		shard := g.getShard(id)
		shard.mu.RLock()
		if node, exists := shard.nodes[id]; exists {
			propsCopy := make(map[string]interface{})
			for k, v := range node.Properties {
				propsCopy[k] = v
			}
			subgraph.AddNode(id, propsCopy)
		}
		shard.mu.RUnlock()
	}

	for from := range nodeSet {
		shard := g.getShard(from)
		shard.mu.RLock()
		if edges, exists := shard.edges[from]; exists {
			for to, edge := range edges {
				if nodeSet[to] {
					propsCopy := make(map[string]interface{})
					for k, v := range edge.Props {
						propsCopy[k] = v
					}
					subgraph.AddEdge(from, to, edge.Type, edge.Weight, propsCopy)
				}
			}
		}
		shard.mu.RUnlock()
	}
	return subgraph
}

func (g *Graph) DetectCommunities(algorithm string) map[string]int {
	switch algorithm {
	case "louvain":
		return g.louvainCommunities()
	case "label_propagation":
		return g.labelPropagation()
	default:
		return g.louvainCommunities()
	}
}

func (g *Graph) louvainCommunities() map[string]int {
	communities := make(map[string]int)
	nodes := make([]string, 0)
	nodeIndex := 0

	for _, shard := range g.shards {
		shard.mu.RLock()
		for id := range shard.nodes {
			communities[id] = nodeIndex
			nodes = append(nodes, id)
			nodeIndex++
		}
		shard.mu.RUnlock()
	}

	improved := true
	for improved {
		improved = false
		for _, nodeID := range nodes {
			currentCommunity := communities[nodeID]
			bestCommunity := currentCommunity
			bestGain := 0.0
			neighborCommunities := make(map[int]float64)

			shard := g.getShard(nodeID)
			shard.mu.RLock()
			if edges, exists := shard.edges[nodeID]; exists {
				for neighbor, edge := range edges {
					comm := communities[neighbor]
					neighborCommunities[comm] += edge.Weight
				}
			}
			shard.mu.RUnlock()

			for comm, weight := range neighborCommunities {
				if comm != currentCommunity && weight > bestGain {
					bestGain = weight
					bestCommunity = comm
				}
			}

			if bestCommunity != currentCommunity {
				communities[nodeID] = bestCommunity
				improved = true
			}
		}
	}
	return g.normalizeCommunities(communities)
}

func (g *Graph) labelPropagation() map[string]int {
	labels := make(map[string]int)
	nodes := make([]string, 0)
	nodeIndex := 0

	for _, shard := range g.shards {
		shard.mu.RLock()
		for id := range shard.nodes {
			labels[id] = nodeIndex
			nodes = append(nodes, id)
			nodeIndex++
		}
		shard.mu.RUnlock()
	}

	for iter := 0; iter < 50; iter++ {
		changed := false
		for _, nodeID := range nodes {
			labelCount := make(map[int]float64)
			shard := g.getShard(nodeID)
			shard.mu.RLock()
			if edges, exists := shard.edges[nodeID]; exists {
				for neighbor, edge := range edges {
					label := labels[neighbor]
					labelCount[label] += edge.Weight
				}
			}
			shard.mu.RUnlock()

			if len(labelCount) == 0 {
				continue
			}

			maxLabel := labels[nodeID]
			maxCount := 0.0
			for label, count := range labelCount {
				if count > maxCount {
					maxCount = count
					maxLabel = label
				}
			}

			if maxLabel != labels[nodeID] {
				labels[nodeID] = maxLabel
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return g.normalizeCommunities(labels)
}

func (g *Graph) normalizeCommunities(communities map[string]int) map[string]int {
	mapping := make(map[int]int)
	nextID := 0
	normalized := make(map[string]int)
	for node, comm := range communities {
		if _, exists := mapping[comm]; !exists {
			mapping[comm] = nextID
			nextID++
		}
		normalized[node] = mapping[comm]
	}
	return normalized
}

func (g *Graph) NodeCount() int {
	count := 0
	for _, shard := range g.shards {
		shard.mu.RLock()
		count += len(shard.nodes)
		shard.mu.RUnlock()
	}
	return count
}

func (g *Graph) EdgeCount() int {
	count := 0
	for _, shard := range g.shards {
		shard.mu.RLock()
		for _, edges := range shard.edges {
			count += len(edges)
		}
		shard.mu.RUnlock()
	}
	return count
}

func (g *Graph) Clear() {
	for _, shard := range g.shards {
		shard.mu.Lock()
		shard.nodes = make(map[string]*Node)
		shard.edges = make(map[string]map[string]*Edge)
		shard.mu.Unlock()
	}
}

type item struct {
	id       string
	priority float64
	index    int
}

type priorityQueue []*item

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].priority < pq[j].priority }
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*item)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*pq = old[0 : n-1]
	return item
}
