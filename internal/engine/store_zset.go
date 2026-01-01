package engine

import (
	"github.com/AutoCookies/pomai-cache/packages/ds/skiplist"
)

func (s *Store) ZAdd(key, member string, score float64) {
	if key == "" || member == "" {
		return
	}
	s.zmu.Lock()
	defer s.zmu.Unlock()

	// Defensive check
	if s.zsets == nil {
		s.zsets = make(map[string]*skiplist.Skiplist)
	}

	ss, ok := s.zsets[key]
	if !ok {
		ss = skiplist.New()
		s.zsets[key] = ss
	}
	ss.Add(member, score)
}

func (s *Store) ZRange(key string, start, stop int) []string {
	if key == "" {
		return nil
	}
	s.zmu.RLock()
	ss := s.zsets[key]
	s.zmu.RUnlock()

	if ss == nil {
		return []string{}
	}
	return ss.Range(start, stop)
}
