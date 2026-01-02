// File: internal/adapter/tcp/server.go
package tcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/gnet/v2"

	"github.com/AutoCookies/pomai-cache/internal/engine/core"
	"github.com/AutoCookies/pomai-cache/internal/engine/tenants"
)

const (
	MaxValueSize = 10 * 1024 * 1024
)

type ServerMetrics struct {
	ActiveConnections atomic.Int64
	TotalRequests     atomic.Uint64
	TotalErrors       atomic.Uint64
	TotalBytes        atomic.Uint64
	StartTime         time.Time
}

func (m *ServerMetrics) GetStats() map[string]interface{} {
	uptime := time.Since(m.StartTime)
	totalReqs := m.TotalRequests.Load()
	totalBytes := m.TotalBytes.Load()
	totalErrors := m.TotalErrors.Load()

	var errorRate float64
	if totalReqs > 0 {
		errorRate = float64(totalErrors) / float64(totalReqs)
	}

	return map[string]interface{}{
		"active_connections": m.ActiveConnections.Load(),
		"total_requests":     totalReqs,
		"total_errors":       totalErrors,
		"total_bytes":        totalBytes,
		"uptime_seconds":     uptime.Seconds(),
		"requests_per_sec":   float64(totalReqs) / uptime.Seconds(),
		"bytes_per_sec":      float64(totalBytes) / uptime.Seconds(),
		"error_rate":         errorRate,
	}
}

type PomaiServer struct {
	gnet.BuiltinEventEngine

	tenants *tenants.Manager
	metrics *ServerMetrics
	addr    string
	eng     gnet.Engine

	connections   atomic.Int64
	totalRequests atomic.Uint64
	totalErrors   atomic.Uint64
	totalBytes    atomic.Uint64

	started   atomic.Bool
	startTime time.Time

	multicore    bool
	numEventLoop int
	reusePort    bool

	autoTuner *core.AutoTuner

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

type connCtx struct {
	tenantID string
	reqCount uint64
	created  int64
}

func NewPomaiServer(tm *tenants.Manager) *PomaiServer {
	numLoops := runtime.NumCPU()
	if numLoops < 2 {
		numLoops = 2
	}
	if numLoops > 16 {
		numLoops = 16
	}

	ctx, cancel := context.WithCancel(context.Background())

	defaultStore := tm.GetStore("default")
	autoTuner := core.NewAutoTuner(defaultStore)

	s := &PomaiServer{
		tenants:      tm,
		metrics:      &ServerMetrics{StartTime: time.Now()},
		multicore:    true,
		numEventLoop: numLoops,
		reusePort:    true,
		ctx:          ctx,
		cancel:       cancel,
		autoTuner:    autoTuner,
	}

	autoTuner.Start()

	return s
}

func (s *PomaiServer) ListenAndServe(addr string) error {
	s.addr = addr
	s.startTime = time.Now()
	s.started.Store(true)

	log.Printf("[TCP] Starting Gnet Server on %s", addr)
	log.Printf("[TCP] Event Loops: %d (CPU: %d)", s.numEventLoop, runtime.NumCPU())
	log.Printf("[TCP] Multicore: %v", s.multicore)
	log.Printf("[TCP] Auto-tuner:  ENABLED")

	return gnet.Run(s, "tcp://"+addr,
		gnet.WithMulticore(s.multicore),
		gnet.WithReusePort(s.reusePort),
		gnet.WithTCPKeepAlive(time.Minute),
		gnet.WithTCPNoDelay(gnet.TCPNoDelay),
		gnet.WithReadBufferCap(256*1024),
		gnet.WithWriteBufferCap(256*1024),
		gnet.WithNumEventLoop(s.numEventLoop),
		gnet.WithTicker(true),
		gnet.WithSocketRecvBuffer(512*1024),
		gnet.WithSocketSendBuffer(512*1024),
		gnet.WithLoadBalancing(gnet.LeastConnections),
	)
}

func (s *PomaiServer) OnBoot(eng gnet.Engine) gnet.Action {
	s.eng = eng
	log.Printf("[TCP] Server booted successfully")
	return gnet.None
}

func (s *PomaiServer) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	s.connections.Add(1)

	ctx := &connCtx{
		tenantID: "default",
		created:  time.Now().UnixNano(),
	}
	c.SetContext(ctx)

	return nil, gnet.None
}

func (s *PomaiServer) OnClose(c gnet.Conn, err error) gnet.Action {
	s.connections.Add(-1)

	if err != nil {
		s.totalErrors.Add(1)
	}

	return gnet.None
}

func (s *PomaiServer) OnTraffic(c gnet.Conn) gnet.Action {
	buf, err := c.Peek(-1)
	if err != nil {
		s.totalErrors.Add(1)
		return gnet.Close
	}

	if len(buf) < HeaderSize {
		return gnet.None
	}

	processed := 0

	for len(buf) >= HeaderSize {
		if buf[0] != MagicByte {
			s.totalErrors.Add(1)
			s.sendError(c, StatusInvalidRequest, "invalid magic byte")
			return gnet.Close
		}

		opcode := buf[1]
		keyLen := binary.BigEndian.Uint16(buf[2:4])
		valLen := binary.BigEndian.Uint32(buf[4:8])

		packetSize := HeaderSize + int(keyLen) + int(valLen)

		if len(buf) < packetSize {
			break
		}

		packet := buf[:packetSize]

		s.handlePacketFast(c, packet, opcode, keyLen, valLen)

		buf = buf[packetSize:]
		processed += packetSize
	}

	if processed > 0 {
		c.Discard(processed)
	}

	return gnet.None
}

func (s *PomaiServer) handlePacketFast(
	c gnet.Conn,
	packet []byte,
	opcode uint8,
	keyLen uint16,
	valLen uint32,
) {
	if keyLen > 65535 {
		s.sendError(c, StatusInvalidRequest, "key too large")
		return
	}

	if valLen > MaxValueSize {
		s.sendError(c, StatusInvalidRequest, "value too large")
		return
	}

	s.totalRequests.Add(1)
	s.totalBytes.Add(uint64(len(packet)))

	s.autoTuner.RecordRequest()

	ctx := c.Context().(*connCtx)
	ctx.reqCount++

	store := s.tenants.GetStore(ctx.tenantID)

	keyStart := HeaderSize
	keyEnd := keyStart + int(keyLen)
	valueStart := keyEnd
	valueEnd := valueStart + int(valLen)

	key := string(packet[keyStart:keyEnd])
	var value []byte
	if valLen > 0 {
		value = make([]byte, valLen)
		copy(value, packet[valueStart:valueEnd])
	}

	switch opcode {
	case OpGet:
		s.handleGetFast(c, store, key)
	case OpSet:
		s.handleSetFast(c, store, key, value)
	case OpDel:
		s.handleDelFast(c, store, key)
	case OpExists:
		s.handleExistsFast(c, store, key)
	case OpIncr:
		s.handleIncrFast(c, store, key, value)
	case OpStats:
		s.handleStatsFast(c, store)
	case OpMGet:
		s.handleMGetFast(c, store, value)
	case OpMSet:
		s.handleMSetFast(c, store, value)
	default:
		s.sendError(c, StatusInvalidRequest, "unknown command")
	}
}

func (s *PomaiServer) handleGetFast(c gnet.Conn, store *core.Store, key string) {
	value, ok := store.Get(key)
	if !ok {
		s.sendResponse(c, StatusKeyNotFound, nil)
		return
	}
	s.sendResponse(c, StatusOK, value)
}

func (s *PomaiServer) handleSetFast(c gnet.Conn, store *core.Store, key string, value []byte) {
	if key == "" {
		s.sendError(c, StatusInvalidRequest, "empty key")
		return
	}

	if err := store.Put(key, value, 0); err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	s.sendResponse(c, StatusOK, nil)
}

func (s *PomaiServer) handleDelFast(c gnet.Conn, store *core.Store, key string) {
	if key != "" {
		store.Delete(key)
	}
	s.sendResponse(c, StatusOK, nil)
}

func (s *PomaiServer) handleExistsFast(c gnet.Conn, store *core.Store, key string) {
	exists := store.Exists(key)
	if exists {
		s.sendResponse(c, StatusOK, []byte("1"))
	} else {
		s.sendResponse(c, StatusOK, []byte("0"))
	}
}

func (s *PomaiServer) handleIncrFast(c gnet.Conn, store *core.Store, key string, deltaBytes []byte) {
	delta := int64(1)
	if len(deltaBytes) > 0 {
		var err error
		delta, err = strconv.ParseInt(string(deltaBytes), 10, 64)
		if err != nil {
			s.sendError(c, StatusInvalidRequest, "invalid delta")
			return
		}
	}

	newVal, err := store.Incr(key, delta)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	result := []byte(strconv.FormatInt(newVal, 10))
	s.sendResponse(c, StatusOK, result)
}

func (s *PomaiServer) handleStatsFast(c gnet.Conn, store *core.Store) {
	stats := store.Stats()

	statsStr := fmt.Sprintf(
		"items:%d\nbytes:%d\n",
		stats.Items,
		stats.Bytes,
	)

	s.sendResponse(c, StatusOK, []byte(statsStr))
}

func (s *PomaiServer) handleMGetFast(c gnet.Conn, store *core.Store, data []byte) {
	keys := parseKeysFast(data)
	if len(keys) == 0 {
		s.sendError(c, StatusInvalidRequest, "no keys")
		return
	}

	results := store.MGet(keys)
	responseData := encodeMGetResponseFast(results)
	s.sendResponse(c, StatusOK, responseData)
}

func (s *PomaiServer) handleMSetFast(c gnet.Conn, store *core.Store, data []byte) {
	items, err := parseMSetRequestFast(data)
	if err != nil {
		s.sendError(c, StatusInvalidRequest, err.Error())
		return
	}

	if len(items) == 0 {
		s.sendError(c, StatusInvalidRequest, "no items")
		return
	}

	if err := store.MSet(items, 0); err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	s.sendResponse(c, StatusOK, nil)
}

func (s *PomaiServer) sendResponse(c gnet.Conn, status uint8, value []byte) {
	keyLen := uint16(0)
	valLen := uint32(len(value))

	packetLen := HeaderSize + len(value)
	buf := make([]byte, packetLen)

	buf[0] = MagicByte
	buf[1] = status
	binary.BigEndian.PutUint16(buf[2:4], keyLen)
	binary.BigEndian.PutUint32(buf[4:8], valLen)

	if len(value) > 0 {
		copy(buf[HeaderSize:], value)
	}

	c.AsyncWrite(buf, nil)
}

func (s *PomaiServer) sendError(c gnet.Conn, status uint8, message string) {
	s.totalErrors.Add(1)
	s.sendResponse(c, status, []byte(message))
}

func (s *PomaiServer) OnTick() (time.Duration, gnet.Action) {
	select {
	case <-s.ctx.Done():
		return 0, gnet.Shutdown
	default:
	}

	if !s.started.Load() {
		return time.Minute, gnet.None
	}

	uptime := time.Since(s.startTime)
	conns := s.connections.Load()
	reqs := s.totalRequests.Load()
	errs := s.totalErrors.Load()
	bytes := s.totalBytes.Load()

	rps := float64(0)
	bps := float64(0)
	errorRate := float64(0)

	if uptime.Seconds() > 0 {
		rps = float64(reqs) / uptime.Seconds()
		bps = float64(bytes) / uptime.Seconds() / 1024 / 1024
	}

	if reqs > 0 {
		errorRate = float64(errs) / float64(reqs) * 100
	}

	log.Printf("[TCP] Conns: %d | Reqs: %d | RPS: %.0f | BW: %.2f MB/s | Errors: %. 2f%%",
		conns, reqs, rps, bps, errorRate)

	return 30 * time.Second, gnet.None
}

func (s *PomaiServer) Shutdown(timeout time.Duration) error {
	log.Println("[TCP] Shutting down...")

	if !s.started.Swap(false) {
		log.Println("[TCP] Server is not running or already stopped.")
		return nil
	}

	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	s.mu.Lock()
	eng := s.eng
	s.mu.Unlock()

	log.Println("[TCP] Stopping gnet engine...")
	if err := eng.Stop(ctx); err != nil {
		log.Printf("[TCP] Error stopping engine: %v", err)
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		if s.connections.Load() == 0 {
			log.Println("[TCP] All connections closed")
			break
		}
		<-ticker.C
	}

	active := s.connections.Load()
	if active > 0 {
		log.Printf("[TCP] Warning: %d connections still active", active)
	}

	log.Println("[TCP] Shutdown complete")
	return nil
}

func (s *PomaiServer) Stats() map[string]interface{} {
	uptime := time.Since(s.startTime)
	reqs := s.totalRequests.Load()
	bytes := s.totalBytes.Load()
	errs := s.totalErrors.Load()

	rps := float64(0)
	bps := float64(0)
	errorRate := float64(0)

	if uptime.Seconds() > 0 {
		rps = float64(reqs) / uptime.Seconds()
		bps = float64(bytes) / uptime.Seconds()
	}

	if reqs > 0 {
		errorRate = float64(errs) / float64(reqs) * 100
	}

	return map[string]interface{}{
		"server_type":        "gnet",
		"connections":        s.connections.Load(),
		"total_requests":     reqs,
		"total_errors":       errs,
		"total_bytes":        bytes,
		"uptime_seconds":     uptime.Seconds(),
		"requests_per_sec":   rps,
		"bytes_per_sec":      bps,
		"error_rate_percent": errorRate,
		"multicore":          s.multicore,
		"event_loops":        s.numEventLoop,
	}
}

func (s *PomaiServer) GetMetrics() map[string]interface{} {
	return s.Stats()
}

func parseKeysFast(data []byte) []string {
	if len(data) < 2 {
		return nil
	}

	keys := make([]string, 0, 16)
	offset := 0
	dataLen := len(data)

	for offset+2 <= dataLen {
		keyLen := int(data[offset])<<8 | int(data[offset+1])
		offset += 2

		if offset+keyLen > dataLen {
			break
		}

		keys = append(keys, string(data[offset:offset+keyLen]))
		offset += keyLen
	}

	return keys
}

func encodeMGetResponseFast(results map[string][]byte) []byte {
	totalSize := 2
	for key, val := range results {
		totalSize += 2 + len(key) + 4 + len(val)
	}

	buf := make([]byte, totalSize)

	offset := 0
	count := len(results)
	buf[offset] = byte(count >> 8)
	buf[offset+1] = byte(count)
	offset += 2

	for key, val := range results {
		keyLen := len(key)
		buf[offset] = byte(keyLen >> 8)
		buf[offset+1] = byte(keyLen)
		offset += 2
		copy(buf[offset:], key)
		offset += keyLen

		valLen := len(val)
		buf[offset] = byte(valLen >> 24)
		buf[offset+1] = byte(valLen >> 16)
		buf[offset+2] = byte(valLen >> 8)
		buf[offset+3] = byte(valLen)
		offset += 4
		copy(buf[offset:], val)
		offset += valLen
	}

	return buf
}

func parseMSetRequestFast(data []byte) (map[string][]byte, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("invalid data: too short")
	}

	count := int(data[0])<<8 | int(data[1])
	if count <= 0 {
		return nil, fmt.Errorf("invalid count: %d", count)
	}

	items := make(map[string][]byte, count)
	offset := 2
	dataLen := len(data)

	for i := 0; i < count; i++ {
		if offset+2 > dataLen {
			return nil, fmt.Errorf("truncated at key %d length", i)
		}

		keyLen := int(data[offset])<<8 | int(data[offset+1])
		offset += 2

		if offset+keyLen > dataLen {
			return nil, fmt.Errorf("truncated at key %d data", i)
		}

		key := string(data[offset : offset+keyLen])
		offset += keyLen

		if offset+4 > dataLen {
			return nil, fmt.Errorf("truncated at value %d length", i)
		}

		valLen := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		offset += 4

		if offset+valLen > dataLen {
			return nil, fmt.Errorf("truncated at value %d data", i)
		}

		val := make([]byte, valLen)
		copy(val, data[offset:offset+valLen])
		items[key] = val
		offset += valLen
	}

	return items, nil
}
