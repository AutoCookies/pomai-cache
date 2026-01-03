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

	"github.com/goccy/go-json"

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

	timestreams sync.Map // tenantID -> *core.TimeStreamStore
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
	case OpStreamAppend:
		s.handleStreamAppendFast(c, ctx.tenantID, key, value)
	case OpStreamRange:
		s.handleStreamRangeFast(c, ctx.tenantID, key, value)
	case OpStreamWindow:
		s.handleStreamWindowFast(c, ctx.tenantID, key, value)
	case OpStreamAnomaly:
		s.handleStreamAnomalyFast(c, ctx.tenantID, key, value)
	case OpStreamForecast:
		s.handleStreamForecastFast(c, ctx.tenantID, key, value)
	case OpStreamPattern:
		s.handleStreamPatternFast(c, ctx.tenantID, key, value)
	case OpVectorPut:
		s.handleVectorPutFast(c, store, key, value)
	case OpVectorSearch:
		s.handleVectorSearchFast(c, store, value)
	case OpBitSet:
		s.handleBitSetFast(c, store, key, value)
	case OpBitGet:
		s.handleBitGetFast(c, store, key, value)
	case OpBitCount:
		s.handleBitCountFast(c, store, key, value)
	case OpStreamReadGroup:
		s.handleStreamReadGroupFast(c, ctx.tenantID, value)
	case OpCDCEnable:
		s.handleCDCEnableFast(c, store, value)
	case OpCDCGet:
		s.handleCDCGetFast(c, store, value)
	case OpZAdd:
		s.handleZAddFast(c, store, key, value)
	case OpZRem:
		s.handleZRemFast(c, store, key, value)
	case OpZScore:
		s.handleZScoreFast(c, store, key, value)
	case OpZRank:
		s.handleZRankFast(c, store, key, value)
	case OpZRange:
		s.handleZRangeFast(c, store, key, value)
	case OpZCard:
		s.handleZCardFast(c, store, key)
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

func (s *PomaiServer) getTimeStore(tenantID string) *core.TimeStreamStore {
	v, ok := s.timestreams.Load(tenantID)
	if ok {
		return v.(*core.TimeStreamStore)
	}
	ts := core.NewTimeStreamStore()
	actual, _ := s.timestreams.LoadOrStore(tenantID, ts)
	return actual.(*core.TimeStreamStore)
}

func (s *PomaiServer) handleStreamAppendFast(c gnet.Conn, tenantID, streamName string, payload []byte) {
	if streamName == "" {
		s.sendError(c, StatusInvalidRequest, "empty stream name")
		return
	}

	ts := s.getTimeStore(tenantID)

	if len(payload) == 0 {
		s.sendError(c, StatusInvalidRequest, "empty event payload")
		return
	}

	first := payload[0]
	if first == '[' {
		var events []*core.Event
		if err := json.Unmarshal(payload, &events); err != nil {
			s.sendError(c, StatusInvalidRequest, "invalid batch event payload")
			return
		}
		if len(events) == 0 {
			s.sendError(c, StatusInvalidRequest, "empty event batch")
			return
		}
		if err := ts.AppendBatch(streamName, events); err != nil {
			s.sendError(c, StatusServerError, err.Error())
			return
		}
		s.sendResponse(c, StatusOK, nil)
		return
	}

	var evt core.Event
	if err := json.Unmarshal(payload, &evt); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid event payload")
		return
	}

	if err := ts.Append(streamName, &evt); err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	s.sendResponse(c, StatusOK, nil)
}

func (s *PomaiServer) handleStreamRangeFast(c gnet.Conn, tenantID, streamName string, payload []byte) {
	if streamName == "" {
		s.sendError(c, StatusInvalidRequest, "empty stream name")
		return
	}

	req := struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	}{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			s.sendError(c, StatusInvalidRequest, "invalid range payload")
			return
		}
	} else {
		now := time.Now().UnixNano()
		req.End = now
		req.Start = now - int64(time.Hour)
	}

	ts := s.getTimeStore(tenantID)
	events, err := ts.Range(streamName, req.Start, req.End, nil)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	response, err := json.Marshal(events)
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}
	s.sendResponse(c, StatusOK, response)
}

func (s *PomaiServer) handleStreamWindowFast(c gnet.Conn, tenantID, streamName string, payload []byte) {
	if streamName == "" {
		s.sendError(c, StatusInvalidRequest, "empty stream name")
		return
	}

	req := struct {
		WindowMs int64  `json:"window_ms"`
		Agg      string `json:"agg"`
	}{}
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid window payload")
		return
	}
	if req.WindowMs <= 0 || req.Agg == "" {
		s.sendError(c, StatusInvalidRequest, "window_ms and agg required")
		return
	}

	ts := s.getTimeStore(tenantID)
	result, err := ts.Window(streamName, time.Duration(req.WindowMs)*time.Millisecond, req.Agg)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	resp, err := json.Marshal(result)
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}

	s.sendResponse(c, StatusOK, resp)
}

func (s *PomaiServer) handleStreamAnomalyFast(c gnet.Conn, tenantID, streamName string, payload []byte) {
	if streamName == "" {
		s.sendError(c, StatusInvalidRequest, "empty stream name")
		return
	}

	req := struct {
		Threshold float64 `json:"threshold"`
	}{Threshold: 3.0}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			s.sendError(c, StatusInvalidRequest, "invalid anomaly payload")
			return
		}
	}

	ts := s.getTimeStore(tenantID)
	events, err := ts.DetectAnomaly(streamName, req.Threshold)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	resp, err := json.Marshal(events)
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}

	s.sendResponse(c, StatusOK, resp)
}

func (s *PomaiServer) handleStreamForecastFast(c gnet.Conn, tenantID, streamName string, payload []byte) {
	if streamName == "" {
		s.sendError(c, StatusInvalidRequest, "empty stream name")
		return
	}

	req := struct {
		HorizonMs int64 `json:"horizon_ms"`
	}{HorizonMs: int64(time.Minute.Milliseconds())}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			s.sendError(c, StatusInvalidRequest, "invalid forecast payload")
			return
		}
	}

	ts := s.getTimeStore(tenantID)
	pred, err := ts.Forecast(streamName, time.Duration(req.HorizonMs)*time.Millisecond)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	resp, err := json.Marshal(map[string]float64{"predicted": pred})
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}

	s.sendResponse(c, StatusOK, resp)
}

func (s *PomaiServer) handleStreamPatternFast(c gnet.Conn, tenantID, streamName string, payload []byte) {
	if streamName == "" {
		s.sendError(c, StatusInvalidRequest, "empty stream name")
		return
	}

	req := struct {
		Pattern  []string `json:"pattern"`
		WithinMs int64    `json:"within_ms"`
	}{}
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid pattern payload")
		return
	}
	if len(req.Pattern) < 2 {
		s.sendError(c, StatusInvalidRequest, "pattern must have at least two types")
		return
	}
	if req.WithinMs <= 0 {
		req.WithinMs = int64(time.Minute.Milliseconds())
	}

	ts := s.getTimeStore(tenantID)
	matches, err := ts.DetectPattern(streamName, req.Pattern, time.Duration(req.WithinMs)*time.Millisecond)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	resp, err := json.Marshal(matches)
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}

	s.sendResponse(c, StatusOK, resp)
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

	log.Printf("[TCP] Conns: %d | Reqs: %d | RPS: %.0f | BW: %.2f MB/s | Errors: %.2f%%",
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

func (s *PomaiServer) handleVectorPutFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	if key == "" {
		s.sendError(c, StatusInvalidRequest, "empty key")
		return
	}

	var req struct {
		Data   []byte    `json:"data"`
		Vector []float32 `json:"vector"`
		TTL    string    `json:"ttl"`
	}

	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid json payload")
		return
	}

	if len(req.Vector) == 0 {
		s.sendError(c, StatusInvalidRequest, "empty vector")
		return
	}

	var ttl time.Duration
	if req.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(req.TTL)
		if err != nil {
			s.sendError(c, StatusInvalidRequest, "invalid ttl format")
			return
		}
	}

	if err := store.PutWithEmbedding(key, req.Data, req.Vector, ttl); err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	s.sendResponse(c, StatusOK, nil)
}

func (s *PomaiServer) handleVectorSearchFast(c gnet.Conn, store *core.Store, payload []byte) {
	var req struct {
		Vector []float32 `json:"vector"`
		K      int       `json:"k"`
	}

	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid json payload")
		return
	}

	if req.K <= 0 {
		req.K = 5
	}

	results, err := store.SemanticSearch(req.Vector, req.K)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	respData, err := json.Marshal(results)
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}

	s.sendResponse(c, StatusOK, respData)
}

func (s *PomaiServer) handleBitSetFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	if key == "" {
		s.sendError(c, StatusInvalidRequest, "empty key")
		return
	}

	var req struct {
		Offset uint64 `json:"offset"`
		Value  int    `json:"value"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid json")
		return
	}

	orig, err := store.SetBit(key, req.Offset, req.Value)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	s.sendResponse(c, StatusOK, []byte(strconv.Itoa(orig)))
}

func (s *PomaiServer) handleBitGetFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	if key == "" {
		s.sendError(c, StatusInvalidRequest, "empty key")
		return
	}

	var req struct {
		Offset uint64 `json:"offset"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid json")
		return
	}

	val, err := store.GetBit(key, req.Offset)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	s.sendResponse(c, StatusOK, []byte(strconv.Itoa(val)))
}

func (s *PomaiServer) handleBitCountFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	if key == "" {
		s.sendError(c, StatusInvalidRequest, "empty key")
		return
	}

	start := int64(0)
	end := int64(-1)

	if len(payload) > 0 {
		var req struct {
			Start int64 `json:"start"`
			End   int64 `json:"end"`
		}
		if err := json.Unmarshal(payload, &req); err == nil {
			start = req.Start
			end = req.End
		}
	}

	count, err := store.BitCount(key, start, end)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	s.sendResponse(c, StatusOK, []byte(strconv.FormatInt(count, 10)))
}

func (s *PomaiServer) handleStreamReadGroupFast(c gnet.Conn, tenantID string, payload []byte) {
	var req struct {
		Stream string `json:"stream"`
		Group  string `json:"group"`
		Count  int    `json:"count"`
	}

	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid json payload")
		return
	}

	if req.Stream == "" || req.Group == "" {
		s.sendError(c, StatusInvalidRequest, "stream and group are required")
		return
	}

	if req.Count <= 0 {
		req.Count = 10
	}

	ts := s.getTimeStore(tenantID)

	events, err := ts.ReadGroup(req.Stream, req.Group, req.Count)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	resp, err := json.Marshal(events)
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}

	s.sendResponse(c, StatusOK, resp)
}

func (s *PomaiServer) handleCDCEnableFast(c gnet.Conn, store *core.Store, payload []byte) {
	val := string(payload)
	enabled := val == "1" || val == "true"

	store.EnableCDC(enabled)
	s.sendResponse(c, StatusOK, []byte("OK"))
}

func (s *PomaiServer) handleCDCGetFast(c gnet.Conn, store *core.Store, payload []byte) {
	group := string(payload)
	if group == "" {
		group = "default_cdc_reader"
	}

	events, err := store.GetChanges(group, 100)
	if err != nil {
		s.sendError(c, StatusServerError, err.Error())
		return
	}

	resp, err := json.Marshal(events)
	if err != nil {
		s.sendError(c, StatusServerError, "marshal error")
		return
	}

	s.sendResponse(c, StatusOK, resp)
}

func (s *PomaiServer) handleZAddFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	if key == "" {
		s.sendError(c, StatusInvalidRequest, "empty key")
		return
	}

	var req struct {
		Score  float64 `json:"score"`
		Member string  `json:"member"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid json")
		return
	}

	store.ZAdd(key, req.Score, req.Member)
	s.sendResponse(c, StatusOK, []byte("OK"))
}

func (s *PomaiServer) handleZRemFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	member := string(payload)
	removed := store.ZRem(key, member)
	if removed {
		s.sendResponse(c, StatusOK, []byte("1"))
	} else {
		s.sendResponse(c, StatusOK, []byte("0"))
	}
}

func (s *PomaiServer) handleZScoreFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	member := string(payload)
	score, ok := store.ZScore(key, member)
	if !ok {
		s.sendResponse(c, StatusKeyNotFound, nil)
		return
	}
	s.sendResponse(c, StatusOK, []byte(fmt.Sprintf("%f", score)))
}

func (s *PomaiServer) handleZRankFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	member := string(payload)
	rank := store.ZRank(key, member)
	if rank == -1 {
		s.sendResponse(c, StatusKeyNotFound, nil)
		return
	}
	s.sendResponse(c, StatusOK, []byte(strconv.Itoa(rank)))
}

func (s *PomaiServer) handleZRangeFast(c gnet.Conn, store *core.Store, key string, payload []byte) {
	var req struct {
		Start int `json:"start"`
		Stop  int `json:"stop"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(c, StatusInvalidRequest, "invalid json")
		return
	}

	items := store.ZRange(key, req.Start, req.Stop)
	resp, _ := json.Marshal(items)
	s.sendResponse(c, StatusOK, resp)
}

func (s *PomaiServer) handleZCardFast(c gnet.Conn, store *core.Store, key string) {
	count := store.ZCard(key)
	s.sendResponse(c, StatusOK, []byte(strconv.Itoa(count)))
}
