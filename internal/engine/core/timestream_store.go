package core

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/AutoCookies/pomai-cache/packages/al"
)

const (
	BlockCapacity = 1024
)

type Event struct {
	ID        string                 `json:"id"`
	Timestamp int64                  `json:"timestamp"`
	Type      string                 `json:"type"`
	Value     float64                `json:"value"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type StreamBlock struct {
	events    [BlockCapacity]Event
	count     int
	startTime int64
	endTime   int64
	next      *StreamBlock
}

var blockPool = sync.Pool{
	New: func() interface{} {
		return &StreamBlock{}
	},
}

func getBlock() *StreamBlock {
	b := blockPool.Get().(*StreamBlock)
	b.count = 0
	b.next = nil
	b.startTime = 0
	b.endTime = 0
	return b
}

func putBlock(b *StreamBlock) {
	for i := 0; i < b.count; i++ {
		b.events[i].Metadata = nil
		b.events[i].ID = ""
		b.events[i].Type = ""
	}
	blockPool.Put(b)
}

type Stream struct {
	mu        sync.RWMutex
	head      *StreamBlock
	tail      *StreamBlock
	totalSize int64

	sum       float64
	sqSum     float64
	retention time.Duration
}

type TimeStreamStore struct {
	streams sync.Map
	offsets sync.Map
}

func NewTimeStreamStore() *TimeStreamStore {
	return &TimeStreamStore{}
}

func (ts *TimeStreamStore) getOrCreateStream(name string) *Stream {
	v, ok := ts.streams.Load(name)
	if ok {
		return v.(*Stream)
	}

	firstBlock := getBlock()
	s := &Stream{
		head:      firstBlock,
		tail:      firstBlock,
		retention: 24 * time.Hour,
	}

	actual, loaded := ts.streams.LoadOrStore(name, s)
	if loaded {
		putBlock(firstBlock)
		return actual.(*Stream)
	}
	return s
}

func (ts *TimeStreamStore) Append(streamName string, event *Event) error {
	return ts.AppendBatch(streamName, []*Event{event})
}

func (ts *TimeStreamStore) AppendBatch(streamName string, events []*Event) error {
	if len(events) == 0 {
		return nil
	}

	s := ts.getOrCreateStream(streamName)

	now := time.Now().UnixNano()
	for _, e := range events {
		if e.Timestamp == 0 {
			e.Timestamp = now
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(now)

	for _, evt := range events {
		if s.tail.count >= BlockCapacity {
			newBlock := getBlock()
			s.tail.next = newBlock
			s.tail = newBlock
		}

		idx := s.tail.count
		s.tail.events[idx] = *evt

		if s.tail.count == 0 {
			s.tail.startTime = evt.Timestamp
		}
		s.tail.endTime = evt.Timestamp
		s.tail.count++

		s.totalSize++
		s.sum += evt.Value
		s.sqSum += evt.Value * evt.Value
	}

	return nil
}

func (s *Stream) pruneLocked(now int64) {
	cutoff := now - int64(s.retention)

	for s.head != s.tail && s.head.endTime < cutoff {
		oldHead := s.head

		for i := 0; i < oldHead.count; i++ {
			val := oldHead.events[i].Value
			s.sum -= val
			s.sqSum -= val * val
		}
		s.totalSize -= int64(oldHead.count)

		s.head = s.head.next
		putBlock(oldHead)
	}
}

func (ts *TimeStreamStore) Range(streamName string, start, end int64, filterFunc func(*Event) bool) ([]*Event, error) {
	v, ok := ts.streams.Load(streamName)
	if !ok {
		return nil, fmt.Errorf("stream not found")
	}
	s := v.(*Stream)

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*Event, 0, 128)

	current := s.head
	for current != nil {
		if current.endTime < start {
			current = current.next
			continue
		}
		if current.startTime > end {
			break
		}

		for i := 0; i < current.count; i++ {
			evt := &current.events[i]
			if evt.Timestamp >= start && evt.Timestamp <= end {
				if filterFunc == nil || filterFunc(evt) {
					eCopy := *evt
					results = append(results, &eCopy)
				}
			}
		}
		current = current.next
	}

	return results, nil
}

func (ts *TimeStreamStore) SetOffset(stream, group string, timestamp int64) {
	key := group + "|" + stream
	ts.offsets.Store(key, timestamp)
}

func (ts *TimeStreamStore) GetOffset(stream, group string) int64 {
	key := group + "|" + stream
	val, ok := ts.offsets.Load(key)
	if !ok {
		return 0
	}
	return val.(int64)
}

func (ts *TimeStreamStore) ReadGroup(streamName, groupName string, count int) ([]*Event, error) {
	v, ok := ts.streams.Load(streamName)
	if !ok {
		return nil, fmt.Errorf("stream not found")
	}
	s := v.(*Stream)

	s.mu.RLock()
	defer s.mu.RUnlock()

	lastReadTimestamp := ts.GetOffset(streamName, groupName)
	results := make([]*Event, 0, count)

	current := s.head
	for current != nil {
		if current.endTime <= lastReadTimestamp {
			current = current.next
			continue
		}

		idx := al.Search(current.count, func(i int) bool {
			return current.events[i].Timestamp > lastReadTimestamp
		})

		for i := idx; i < current.count; i++ {
			if len(results) >= count {
				goto Done
			}
			e := current.events[i]
			results = append(results, &e)
		}

		current = current.next
	}

Done:
	if len(results) > 0 {
		newOffset := results[len(results)-1].Timestamp
		ts.SetOffset(streamName, groupName, newOffset)
	}

	return results, nil
}

func (ts *TimeStreamStore) Window(streamName string, windowSize time.Duration, aggType string) (map[int64]float64, error) {
	v, ok := ts.streams.Load(streamName)
	if !ok {
		return nil, fmt.Errorf("stream not found")
	}
	s := v.(*Stream)

	s.mu.RLock()
	defer s.mu.RUnlock()

	buckets := make(map[int64]struct {
		sum   float64
		count int64
		min   float64
		max   float64
	})

	windowNano := windowSize.Nanoseconds()
	if windowNano == 0 {
		windowNano = 1
	}

	current := s.head
	for current != nil {
		for i := 0; i < current.count; i++ {
			val := current.events[i].Value
			ts := current.events[i].Timestamp

			bucketKey := (ts / windowNano) * windowNano

			b := buckets[bucketKey]
			if b.count == 0 {
				b.min = val
				b.max = val
			} else {
				if val < b.min {
					b.min = val
				}
				if val > b.max {
					b.max = val
				}
			}
			b.sum += val
			b.count++
			buckets[bucketKey] = b
		}
		current = current.next
	}

	result := make(map[int64]float64, len(buckets))
	for t, b := range buckets {
		var finalVal float64
		switch aggType {
		case "sum":
			finalVal = b.sum
		case "avg":
			if b.count > 0 {
				finalVal = b.sum / float64(b.count)
			}
		case "min":
			finalVal = b.min
		case "max":
			finalVal = b.max
		case "count":
			finalVal = float64(b.count)
		default:
			return nil, fmt.Errorf("unknown agg type: %s", aggType)
		}
		result[t] = finalVal
	}

	return result, nil
}

func (ts *TimeStreamStore) DetectAnomaly(streamName string, threshold float64) ([]*Event, error) {
	v, ok := ts.streams.Load(streamName)
	if !ok {
		return nil, fmt.Errorf("stream not found")
	}
	s := v.(*Stream)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.totalSize < 10 {
		return nil, nil
	}

	mean := s.sum / float64(s.totalSize)
	variance := (s.sqSum / float64(s.totalSize)) - (mean * mean)
	if variance < 0 {
		variance = 0
	}
	stdDev := math.Sqrt(variance)
	if stdDev == 0 {
		stdDev = 1e-9
	}

	anomalies := make([]*Event, 0)

	current := s.head
	for current != nil {
		for i := 0; i < current.count; i++ {
			val := current.events[i].Value
			zScore := math.Abs((val - mean) / stdDev)
			if zScore > threshold {
				eCopy := current.events[i]
				anomalies = append(anomalies, &eCopy)
			}
		}
		current = current.next
	}

	return anomalies, nil
}

func (ts *TimeStreamStore) Forecast(streamName string, horizon time.Duration) (float64, error) {
	v, ok := ts.streams.Load(streamName)
	if !ok {
		return 0, fmt.Errorf("stream not found")
	}
	s := v.(*Stream)

	s.mu.RLock()
	defer s.mu.RUnlock()

	n := float64(s.totalSize)
	if n < 2 {
		return 0, nil
	}

	var sumX, sumY, sumXY, sumXX float64

	baseTime := float64(s.head.startTime)
	var lastTime float64

	current := s.head
	for current != nil {
		for i := 0; i < current.count; i++ {
			x := float64(current.events[i].Timestamp) - baseTime
			y := current.events[i].Value

			sumX += x
			sumY += y
			sumXY += x * y
			sumXX += x * x
			lastTime = x
		}
		current = current.next
	}

	denom := n*sumXX - sumX*sumX
	if math.Abs(denom) < 1e-9 {
		return 0, nil
	}

	slope := (n*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / n

	futureTime := lastTime + float64(horizon.Nanoseconds())
	return slope*futureTime + intercept, nil
}

func (ts *TimeStreamStore) DetectPattern(streamName string, types []string, within time.Duration) ([][]*Event, error) {
	return nil, nil
}
