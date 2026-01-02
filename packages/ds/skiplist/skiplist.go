// File: packages/ds/skiplist/skiplist.go
package skiplist

import (
	"math/rand"
	"sync"
	"time"
)

const maxLevel = 32
const probability = 0.25

func init() {
	rand.Seed(time.Now().UnixNano())
}

// NodePublic represents public node data
type NodePublic struct {
	Member string
	Score  float64
}

// znode represents internal skiplist node
type znode struct {
	member string
	score  float64
	next   []*znode
}

// Skiplist implements a thread-safe sorted set
type Skiplist struct {
	mu    sync.RWMutex
	head  *znode
	level int
	dict  map[string]*znode
	size  int64
}

// New creates new skiplist
func New() *Skiplist {
	return &Skiplist{
		head:  &znode{next: make([]*znode, maxLevel)},
		level: 1,
		dict:  make(map[string]*znode),
		size:  0,
	}
}

// Add adds or updates member with score
func (z *Skiplist) Add(member string, score float64) {
	z.mu.Lock()
	defer z.mu.Unlock()

	// Remove existing if present
	if _, ok := z.dict[member]; ok {
		z.removeLocked(member)
	}

	// Find insertion point
	update := make([]*znode, maxLevel)
	current := z.head

	for i := z.level - 1; i >= 0; i-- {
		for current.next[i] != nil && current.next[i].score < score {
			current = current.next[i]
		}
		update[i] = current
	}

	// Determine level for new node
	lvl := randomLevel()
	if lvl > z.level {
		for i := z.level; i < lvl; i++ {
			update[i] = z.head
		}
		z.level = lvl
	}

	// Create and insert new node
	newNode := &znode{
		member: member,
		score:  score,
		next:   make([]*znode, lvl),
	}

	for i := 0; i < lvl; i++ {
		newNode.next[i] = update[i].next[i]
		update[i].next[i] = newNode
	}

	z.dict[member] = newNode
	z.size++
}

// Range returns members in rank range [start, stop]
// Negative stop means all elements
func (z *Skiplist) Range(start, stop int) []string {
	z.mu.RLock()
	defer z.mu.RUnlock()

	result := make([]string, 0, z.size)
	current := z.head.next[0]
	rank := 0

	for current != nil {
		if rank >= start && (stop < 0 || rank <= stop) {
			result = append(result, current.member)
		}
		if stop >= 0 && rank > stop {
			break
		}
		current = current.next[0]
		rank++
	}

	return result
}

// Remove removes member and returns true if found
func (z *Skiplist) Remove(member string) bool {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.removeLocked(member)
}

// removeLocked removes member (internal, must hold lock)
func (z *Skiplist) removeLocked(member string) bool {
	node, ok := z.dict[member]
	if !ok {
		return false
	}

	// Find update nodes
	update := make([]*znode, maxLevel)
	current := z.head

	for i := z.level - 1; i >= 0; i-- {
		for current.next[i] != nil && current.next[i].score < node.score {
			current = current.next[i]
		}
		update[i] = current
	}

	// Remove node from all levels
	for i := 0; i < z.level; i++ {
		if update[i].next[i] == node {
			update[i].next[i] = node.next[i]
		}
	}

	// Adjust level if needed
	for z.level > 1 && z.head.next[z.level-1] == nil {
		z.level--
	}

	delete(z.dict, member)
	z.size--
	return true
}

// Score returns score of member
func (z *Skiplist) Score(member string) (float64, bool) {
	z.mu.RLock()
	defer z.mu.RUnlock()

	node, ok := z.dict[member]
	if !ok {
		return 0, false
	}
	return node.score, true
}

// Len returns number of members
func (z *Skiplist) Len() int {
	z.mu.RLock()
	defer z.mu.RUnlock()
	return int(z.size)
}

// Dump returns all members as public nodes
func (z *Skiplist) Dump() []NodePublic {
	z.mu.RLock()
	defer z.mu.RUnlock()

	result := make([]NodePublic, 0, z.size)
	current := z.head.next[0]

	for current != nil {
		result = append(result, NodePublic{
			Member: current.member,
			Score:  current.score,
		})
		current = current.next[0]
	}

	return result
}

// Size returns number of members (alias for Len)
func (z *Skiplist) Size() int64 {
	z.mu.RLock()
	defer z.mu.RUnlock()
	return z.size
}

// Get returns score of member (alias for Score)
func (z *Skiplist) Get(member string) (float64, bool) {
	return z.Score(member)
}

// RangeByScore returns members with scores in [minScore, maxScore]
func (z *Skiplist) RangeByScore(minScore, maxScore float64, limit int) []NodePublic {
	z.mu.RLock()
	defer z.mu.RUnlock()

	result := make([]NodePublic, 0)
	current := z.head

	// Find first node >= minScore
	for i := z.level - 1; i >= 0; i-- {
		for current.next[i] != nil && current.next[i].score < minScore {
			current = current.next[i]
		}
	}

	current = current.next[0]
	count := 0

	// Collect nodes in range
	for current != nil && current.score <= maxScore {
		if limit <= 0 || count < limit {
			result = append(result, NodePublic{
				Member: current.member,
				Score:  current.score,
			})
			count++
		} else {
			break
		}
		current = current.next[0]
	}

	return result
}

// Clear removes all members
func (z *Skiplist) Clear() {
	z.mu.Lock()
	defer z.mu.Unlock()

	z.head = &znode{next: make([]*znode, maxLevel)}
	z.level = 1
	z.dict = make(map[string]*znode)
	z.size = 0
}

// randomLevel generates random level for new node
func randomLevel() int {
	lvl := 1
	for rand.Float64() < probability && lvl < maxLevel {
		lvl++
	}
	return lvl
}
