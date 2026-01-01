package skiplist

import (
	"math/rand"
	"time"
)

const maxLevel = 32
const probability = 0.25

func init() {
	rand.Seed(time.Now().UnixNano())
}

// NodePublic dùng để export dữ liệu ra ngoài package
type NodePublic struct {
	Member string
	Score  float64
}

type znode struct {
	member string
	score  float64
	next   []*znode
}

type Skiplist struct {
	head  *znode
	level int
	dict  map[string]*znode
}

func New() *Skiplist {
	return &Skiplist{
		head:  &znode{next: make([]*znode, maxLevel)},
		level: 1,
		dict:  make(map[string]*znode),
	}
}

func (z *Skiplist) Add(member string, score float64) {
	if _, ok := z.dict[member]; ok {
		z.Remove(member)
	}

	update := make([]*znode, maxLevel)
	current := z.head

	for i := z.level - 1; i >= 0; i-- {
		for current.next[i] != nil && current.next[i].score < score {
			current = current.next[i]
		}
		update[i] = current
	}

	lvl := randomLevel()
	if lvl > z.level {
		for i := z.level; i < lvl; i++ {
			update[i] = z.head
		}
		z.level = lvl
	}

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
}

// Range trả về danh sách member trong khoảng rank [start, stop].
// Nếu stop < 0, nó sẽ lấy đến hết danh sách.
func (z *Skiplist) Range(start, stop int) []string {
	result := []string{}
	current := z.head.next[0]
	rank := 0

	for current != nil {
		// [FIX] Thêm điều kiện (stop < 0) để lấy tất cả nếu stop là số âm (ví dụ -1)
		if rank >= start && (stop < 0 || rank <= stop) {
			result = append(result, current.member)
		}
		// [FIX] Chỉ break nếu stop không âm và đã vượt quá stop
		if stop >= 0 && rank > stop {
			break
		}
		current = current.next[0]
		rank++
	}
	return result
}

func (z *Skiplist) Remove(member string) {
	node, ok := z.dict[member]
	if !ok {
		return
	}

	update := make([]*znode, maxLevel)
	current := z.head

	for i := z.level - 1; i >= 0; i-- {
		for current.next[i] != nil && current.next[i].score < node.score {
			current = current.next[i]
		}
		update[i] = current
	}

	for i := 0; i < z.level; i++ {
		if update[i].next[i] == node {
			update[i].next[i] = node.next[i]
		}
	}

	for z.level > 1 && z.head.next[z.level-1] == nil {
		z.level--
	}
	delete(z.dict, member)
}

func randomLevel() int {
	lvl := 1
	for rand.Float64() < probability && lvl < maxLevel {
		lvl++
	}
	return lvl
}

// Dump trả về toàn bộ dữ liệu trong skiplist (dùng cho snapshot)
func (z *Skiplist) Dump() []NodePublic {
	var result []NodePublic
	// Duyệt qua level 0 (level chứa đầy đủ phần tử nhất)
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
