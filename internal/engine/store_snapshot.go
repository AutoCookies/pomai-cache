package engine

import (
	"encoding/gob"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"github.com/AutoCookies/pomai-cache/packages/ds/skiplist"
)

// snapshotItem là struct trung gian dùng để encode/decode dữ liệu.
// Nó hỗ trợ cả Key-Value truyền thống và ZSet (Sorted Set).
type snapshotItem struct {
	Type uint8 // 0 = KV (Key-Value), 1 = ZSet (Sorted Set)

	// Fields cho KV
	Key        string
	Value      []byte
	ExpireAt   int64
	LastAccess int64
	Accesses   uint64
	CreatedAt  int64

	// Fields cho ZSet
	ZMembers []snapshotZMember
}

// snapshotZMember struct con để lưu từng phần tử trong ZSet
type snapshotZMember struct {
	Member string
	Score  float64
}

// SnapshotTo ghi toàn bộ dữ liệu cache (KV + ZSet) vào writer.
func (s *Store) SnapshotTo(w io.Writer) error {
	enc := gob.NewEncoder(w)

	// 1. Ghi Version. Sử dụng version 2 để đánh dấu hỗ trợ ZSet.
	if err := enc.Encode(int(2)); err != nil {
		return err
	}

	// 2. Snapshot KV Data (Shards)
	for _, sh := range s.shards {
		sh.mu.RLock()
		for _, elem := range sh.items {
			ent := elem.Value.(*entry)

			// Bỏ qua key hết hạn
			if ent.expireAt != 0 && time.Now().UnixNano() > ent.expireAt {
				continue
			}

			item := snapshotItem{
				Type:       0, // KV
				Key:        ent.key,
				Value:      ent.value,
				ExpireAt:   ent.expireAt,
				LastAccess: ent.lastAccess,
				Accesses:   atomic.LoadUint64(&ent.accesses),
				CreatedAt:  ent.createdAt,
			}

			if err := enc.Encode(&item); err != nil {
				sh.mu.RUnlock()
				return err
			}
		}
		sh.mu.RUnlock()
	}

	// 3. Snapshot ZSet Data
	// Cần Lock toàn bộ map ZSet để đọc
	s.zmu.RLock()
	defer s.zmu.RUnlock()

	for key, sl := range s.zsets {
		// Lấy toàn bộ dữ liệu từ Skiplist
		// LƯU Ý: Bạn cần đảm bảo pkg/ds/skiplist có method Dump() trả về danh sách node.
		// Nếu chưa có, xem phần chú thích bên dưới để thêm vào.
		nodes := sl.Dump()

		zMembers := make([]snapshotZMember, len(nodes))
		for i, n := range nodes {
			zMembers[i] = snapshotZMember{
				Member: n.Member,
				Score:  n.Score,
			}
		}

		item := snapshotItem{
			Type:     1, // ZSet
			Key:      key,
			ZMembers: zMembers,
		}

		if err := enc.Encode(&item); err != nil {
			return err
		}
	}

	return nil
}

// RestoreFrom khôi phục dữ liệu từ reader.
func (s *Store) RestoreFrom(r io.Reader) error {
	dec := gob.NewDecoder(r)

	// 1. Check Version
	var version int
	if err := dec.Decode(&version); err != nil {
		return err
	}
	// Hỗ trợ cả version 1 (cũ) và 2 (mới)
	if version != 1 && version != 2 {
		return errors.New("unsupported snapshot version")
	}

	// 2. Loop đọc từng item
	for {
		var item snapshotItem
		if err := dec.Decode(&item); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if item.Type == 0 {
			// --- RESTORE KV ---
			if item.ExpireAt != 0 && time.Now().UnixNano() > item.ExpireAt {
				continue
			}

			sh := s.getShard(item.Key)
			sh.mu.Lock()

			// Xóa cũ nếu đè
			if elem, ok := sh.items[item.Key]; ok {
				old := elem.Value.(*entry)
				delete(sh.items, item.Key)
				sh.ll.Remove(elem)
				sh.bytes -= int64(old.size)
				atomic.AddInt64(&s.totalBytesAtomic, -int64(old.size))
			}

			ent := &entry{
				key:        item.Key,
				value:      item.Value,
				size:       len(item.Value),
				expireAt:   item.ExpireAt,
				lastAccess: item.LastAccess,
				accesses:   item.Accesses,
				createdAt:  item.CreatedAt,
			}

			elem := sh.ll.PushFront(ent)
			sh.items[item.Key] = elem
			sh.bytes += int64(ent.size)
			atomic.AddInt64(&s.totalBytesAtomic, int64(ent.size))

			// Rebuild Bloom Filter
			if s.bloom != nil {
				s.bloom.Add(item.Key)
			}
			sh.mu.Unlock()

		} else if item.Type == 1 {
			// --- RESTORE ZSET ---
			// Vì ZSet ít khi bị expire hơn và cơ chế phức tạp hơn, ta restore thẳng
			s.zmu.Lock()

			// Lazy init map nếu chưa có
			if s.zsets == nil {
				s.zsets = make(map[string]*skiplist.Skiplist)
			}

			// Tạo mới Skiplist
			sl := skiplist.New()
			for _, m := range item.ZMembers {
				sl.Add(m.Member, m.Score)
			}
			s.zsets[item.Key] = sl

			s.zmu.Unlock()
		}
	}

	return nil
}
