package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/adapter/tcp"
)

var (
	addr          = flag.String("addr", "localhost:7600", "Server address")
	clients       = flag.Int("clients", 50, "Number of concurrent clients")
	requests      = flag.Int("requests", 1000000, "Total requests")
	pipeline      = flag.Int("pipeline", 128, "Pipeline batch size")
	dataSize      = flag.Int("data-size", 128, "Value size in bytes")
	workloadRatio = flag.Float64("ratio", 0.5, "Read ratio (0.0-1.0)")
	runs          = flag.Int("runs", 3, "Number of benchmark runs")

	aiMode        = flag.Bool("ai-mode", false, "Enable AI semantic workload")
	vectorDim     = flag.Int("vector-dim", 128, "Vector dimension for AI mode")
	semanticNoise = flag.Float64("semantic-noise", 0.1, "Perturbation noise for semantic queries")

	graphMode   = flag.Bool("graph-mode", false, "Enable graph workload")
	graphNodes  = flag.Int("graph-nodes", 100000, "Number of graph nodes")
	graphDegree = flag.Int("graph-degree", 10, "Average edges per node")

	timeMode        = flag.Bool("time-mode", false, "Enable TimeStream workload")
	timeStreams     = flag.Int("time-streams", 100, "Number of time streams")
	eventsPerStream = flag.Int("events-per-stream", 1000, "Initial events per stream")

	bitmapMode      = flag.Bool("bitmap-mode", false, "Enable Bitmap workload")
	bitmapMaxOffset = flag.Uint64("bitmap-size", 100000000, "Max bit offset (default 100M)")

	zsetMode    = flag.Bool("zset-mode", false, "Enable Sorted Set (ZSet) workload")
	zsetKeys    = flag.Int("zset-keys", 1000, "Number of ZSet keys")
	zsetMembers = flag.Int("zset-members", 10000, "Number of members per ZSet")
)

type BenchmarkResult struct {
	Duration   time.Duration
	TotalOps   uint64
	Throughput float64
	Bandwidth  float64
	AvgLatency float64
	MinLatency float64
	MaxLatency float64
	Latencies  []time.Duration
	Errors     uint64
}

type Config struct {
	addr            string
	clients         int
	requests        int
	pipeline        int
	dataSize        int
	ratio           float64
	runs            int
	aiMode          bool
	vectorDim       int
	semanticNoise   float64
	graphMode       bool
	graphNodes      int
	graphDegree     int
	timeMode        bool
	timeStreams     int
	eventsPerStream int
	bitmapMode      bool
	bitmapMaxOffset uint64
	zsetMode        bool
	zsetKeys        int
	zsetMembers     int
}

type AIWorkload struct {
	keys       []string
	embeddings map[string][]float64
	mu         sync.RWMutex
}

type GraphWorkload struct {
	nodeIDs []string
	edges   map[string][]string
	mu      sync.RWMutex
}

type TimeWorkload struct {
	streamKeys []string
}

type ZSetWorkload struct {
	keys []string
}

// Payloads
type VectorPutReq struct {
	Data   []byte    `json:"data"`
	Vector []float32 `json:"vector"`
	TTL    string    `json:"ttl"`
}

type VectorSearchReq struct {
	Vector []float32 `json:"vector"`
	K      int       `json:"k"`
}

type BitSetReq struct {
	Offset uint64 `json:"offset"`
	Value  int    `json:"value"`
}

type BitGetReq struct {
	Offset uint64 `json:"offset"`
}

type BitCountReq struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type ZAddReq struct {
	Score  float64 `json:"score"`
	Member string  `json:"member"`
}

type ZRangeReq struct {
	Start int `json:"start"`
	Stop  int `json:"stop"`
}

func main() {
	flag.Parse()

	cfg := &Config{
		addr:            *addr,
		clients:         *clients,
		requests:        *requests,
		pipeline:        *pipeline,
		dataSize:        *dataSize,
		ratio:           *workloadRatio,
		runs:            *runs,
		aiMode:          *aiMode,
		vectorDim:       *vectorDim,
		semanticNoise:   *semanticNoise,
		graphMode:       *graphMode,
		graphNodes:      *graphNodes,
		graphDegree:     *graphDegree,
		timeMode:        *timeMode,
		timeStreams:     *timeStreams,
		eventsPerStream: *eventsPerStream,
		bitmapMode:      *bitmapMode,
		bitmapMaxOffset: *bitmapMaxOffset,
		zsetMode:        *zsetMode,
		zsetKeys:        *zsetKeys,
		zsetMembers:     *zsetMembers,
	}

	printBanner(cfg)

	var aiWorkload *AIWorkload
	if cfg.aiMode {
		aiWorkload = initAIWorkload(cfg)
	}

	var graphWorkload *GraphWorkload
	if cfg.graphMode {
		graphWorkload = initGraphWorkload(cfg)
	}

	var timeWorkload *TimeWorkload
	if cfg.timeMode {
		timeWorkload = initTimeWorkload(cfg)
	}

	var zsetWorkload *ZSetWorkload
	if cfg.zsetMode {
		zsetWorkload = initZSetWorkload(cfg)
	}

	log.Printf("Warming up (%d requests)...", cfg.requests/10)
	runBenchmarkPhase(cfg, cfg.requests/10, aiWorkload, graphWorkload, timeWorkload, zsetWorkload, true)

	var results []BenchmarkResult
	for i := 0; i < cfg.runs; i++ {
		fmt.Printf("\nRun %d/%d:\n", i+1, cfg.runs)
		res := runBenchmarkPhase(cfg, cfg.requests, aiWorkload, graphWorkload, timeWorkload, zsetWorkload, false)
		printResult(i+1, res)
		results = append(results, res)
		time.Sleep(1 * time.Second)
	}

	printAverageResults(results)
}

func initAIWorkload(cfg *Config) *AIWorkload {
	log.Println("Initializing AI workload...")
	count := 100000
	keys := make([]string, count)
	embeddings := make(map[string][]float64)

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("vec:%d", i)
		keys[i] = key
		embeddings[key] = generateRandomVector(cfg.vectorDim)
	}

	log.Printf("Generated %d embeddings (%d dims)", count, cfg.vectorDim)
	return &AIWorkload{keys: keys, embeddings: embeddings}
}

func initGraphWorkload(cfg *Config) *GraphWorkload {
	log.Println("Initializing Graph workload...")
	nodeIDs := make([]string, cfg.graphNodes)
	for i := 0; i < cfg.graphNodes; i++ {
		nodeIDs[i] = fmt.Sprintf("node:%d", i)
	}

	log.Printf("Generated graph: %d nodes, ~%d edges", cfg.graphNodes, cfg.graphNodes*cfg.graphDegree)
	return &GraphWorkload{nodeIDs: nodeIDs}
}

func initTimeWorkload(cfg *Config) *TimeWorkload {
	log.Println("Initializing Time workload...")
	keys := make([]string, cfg.timeStreams)
	for i := 0; i < cfg.timeStreams; i++ {
		keys[i] = fmt.Sprintf("stream:%d", i)
	}
	return &TimeWorkload{streamKeys: keys}
}

func initZSetWorkload(cfg *Config) *ZSetWorkload {
	log.Println("Initializing ZSet workload...")
	keys := make([]string, cfg.zsetKeys)
	for i := 0; i < cfg.zsetKeys; i++ {
		keys[i] = fmt.Sprintf("leaderboard:%d", i)
	}
	return &ZSetWorkload{keys: keys}
}

func runBenchmarkPhase(cfg *Config, totalReqs int, aiWorkload *AIWorkload, graphWorkload *GraphWorkload, timeWorkload *TimeWorkload, zsetWorkload *ZSetWorkload, warmup bool) BenchmarkResult {
	var wg sync.WaitGroup
	wg.Add(cfg.clients)

	start := time.Now()
	opsPerClient := totalReqs / cfg.clients

	latencies := make([]time.Duration, totalReqs)
	var latencyIdx int64 = -1
	var totalErrors uint64
	var totalBytes uint64

	dummyValue := make([]byte, cfg.dataSize)
	rand.Read(dummyValue)

	for i := 0; i < cfg.clients; i++ {
		go func(clientID int) {
			defer wg.Done()

			client, err := tcp.NewClient(cfg.addr)
			if err != nil {
				if !warmup {
					log.Printf("Client %d connect error: %v", clientID, err)
					atomic.AddUint64(&totalErrors, 1)
				}
				return
			}
			defer client.Close()

			batchSize := cfg.pipeline
			if batchSize <= 0 {
				batchSize = 1
			}

			pipelineBuffer := make([]tcp.Packet, 0, batchSize)
			responseBuffer := make([]tcp.Packet, batchSize)

			for j := 0; j < opsPerClient; j++ {
				isRead := rand.Float64() < cfg.ratio

				var packet tcp.Packet

				if cfg.zsetMode {
					packet = buildZSetPacket(cfg, zsetWorkload, clientID, j, isRead)
				} else if cfg.bitmapMode {
					packet = buildBitmapPacket(cfg, clientID, j, isRead)
				} else if cfg.aiMode {
					packet = buildAIPacket(cfg, aiWorkload, clientID, j, isRead, dummyValue)
				} else if cfg.graphMode {
					packet = buildGraphPacket(cfg, graphWorkload, clientID, j, isRead)
				} else if cfg.timeMode {
					packet = buildTimePacket(cfg, timeWorkload, clientID, j, isRead)
				} else {
					packet = buildStandardPacket(cfg, clientID, j, isRead, dummyValue)
				}

				pipelineBuffer = append(pipelineBuffer, packet)

				if len(pipelineBuffer) == batchSize {
					reqStart := time.Now()
					err := client.PipelineFast(pipelineBuffer, responseBuffer)
					duration := time.Since(reqStart)

					if err != nil {
						atomic.AddUint64(&totalErrors, uint64(len(pipelineBuffer)))
					} else {
						avgLat := duration / time.Duration(batchSize)
						for k := 0; k < batchSize; k++ {
							idx := atomic.AddInt64(&latencyIdx, 1)
							if idx < int64(len(latencies)) {
								latencies[idx] = avgLat
							}
							atomic.AddUint64(&totalBytes, uint64(len(pipelineBuffer[k].Value)+len(responseBuffer[k].Value)))
						}
					}

					pipelineBuffer = pipelineBuffer[:0]
				}
			}

			if len(pipelineBuffer) > 0 {
				client.PipelineFast(pipelineBuffer, responseBuffer[:len(pipelineBuffer)])
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	validLatencies := make([]time.Duration, 0, totalReqs)
	var minLat, maxLat time.Duration
	var sumLat time.Duration

	maxLat = 0
	minLat = time.Hour

	count := int(atomic.LoadInt64(&latencyIdx)) + 1
	if count > totalReqs {
		count = totalReqs
	}

	for i := 0; i < count; i++ {
		l := latencies[i]
		validLatencies = append(validLatencies, l)
		sumLat += l
		if l < minLat {
			minLat = l
		}
		if l > maxLat {
			maxLat = l
		}
	}
	sort.Slice(validLatencies, func(i, j int) bool {
		return validLatencies[i] < validLatencies[j]
	})

	avgLat := 0.0
	if len(validLatencies) > 0 {
		avgLat = float64(sumLat.Microseconds()) / float64(len(validLatencies)) / 1000.0
	}

	throughput := float64(totalReqs) / duration.Seconds()
	bandwidth := float64(totalBytes) / duration.Seconds() / 1024 / 1024

	return BenchmarkResult{
		Duration:   duration,
		TotalOps:   uint64(totalReqs),
		Throughput: throughput,
		Bandwidth:  bandwidth,
		AvgLatency: avgLat,
		MinLatency: float64(minLat.Microseconds()) / 1000.0,
		MaxLatency: float64(maxLat.Microseconds()) / 1000.0,
		Latencies:  validLatencies,
		Errors:     totalErrors,
	}
}

func buildStandardPacket(cfg *Config, clientID, reqIdx int, isRead bool, value []byte) tcp.Packet {
	key := fmt.Sprintf("key_%d_%d", clientID, reqIdx%10000)
	if isRead {
		return tcp.Packet{Opcode: tcp.OpGet, Key: key}
	}
	return tcp.Packet{Opcode: tcp.OpSet, Key: key, Value: value}
}

func buildZSetPacket(cfg *Config, zw *ZSetWorkload, clientID, reqIdx int, isRead bool) tcp.Packet {
	key := zw.keys[rand.Intn(len(zw.keys))]

	if isRead {
		// 50% Range, 25% Score, 25% Rank
		r := rand.Float64()
		if r < 0.5 {
			// ZRange
			start := rand.Intn(100)
			stop := start + rand.Intn(20)
			req := ZRangeReq{Start: start, Stop: stop}
			payload, _ := json.Marshal(req)
			return tcp.Packet{Opcode: tcp.OpZRange, Key: key, Value: payload}
		} else if r < 0.75 {
			// ZScore
			member := fmt.Sprintf("user:%d", rand.Intn(cfg.zsetMembers))
			return tcp.Packet{Opcode: tcp.OpZScore, Key: key, Value: []byte(member)}
		} else {
			// ZRank
			member := fmt.Sprintf("user:%d", rand.Intn(cfg.zsetMembers))
			return tcp.Packet{Opcode: tcp.OpZRank, Key: key, Value: []byte(member)}
		}
	} else {
		// ZAdd
		member := fmt.Sprintf("user:%d", rand.Intn(cfg.zsetMembers))
		score := rand.Float64() * 10000
		req := ZAddReq{Member: member, Score: score}
		payload, _ := json.Marshal(req)
		return tcp.Packet{Opcode: tcp.OpZAdd, Key: key, Value: payload}
	}
}

func buildBitmapPacket(cfg *Config, clientID, reqIdx int, isRead bool) tcp.Packet {
	key := fmt.Sprintf("bench_bitmap_%d", clientID%10)

	if isRead {
		if rand.Float64() < 0.5 {
			offset := rand.Uint64() % cfg.bitmapMaxOffset
			req := BitGetReq{Offset: offset}
			payload, _ := json.Marshal(req)
			return tcp.Packet{Opcode: tcp.OpBitGet, Key: key, Value: payload}
		} else {
			req := BitCountReq{Start: 0, End: -1}
			payload, _ := json.Marshal(req)
			return tcp.Packet{Opcode: tcp.OpBitCount, Key: key, Value: payload}
		}
	} else {
		offset := rand.Uint64() % cfg.bitmapMaxOffset
		val := rand.Intn(2)
		req := BitSetReq{Offset: offset, Value: val}
		payload, _ := json.Marshal(req)
		return tcp.Packet{Opcode: tcp.OpBitSet, Key: key, Value: payload}
	}
}

func buildAIPacket(cfg *Config, aiWorkload *AIWorkload, clientID, reqIdx int, isRead bool, value []byte) tcp.Packet {
	toFloat32 := func(in []float64) []float32 {
		out := make([]float32, len(in))
		for i, v := range in {
			out[i] = float32(v)
		}
		return out
	}

	if isRead {
		targetIdx := (clientID*cfg.requests + reqIdx) % len(aiWorkload.keys)
		targetKey := aiWorkload.keys[targetIdx]

		aiWorkload.mu.RLock()
		targetVec := aiWorkload.embeddings[targetKey]
		aiWorkload.mu.RUnlock()

		queryVec := perturbVector(targetVec, cfg.semanticNoise)
		req := VectorSearchReq{Vector: toFloat32(queryVec), K: 5}
		payload, _ := json.Marshal(req)

		return tcp.Packet{Opcode: tcp.OpVectorSearch, Value: payload}
	}

	keyIdx := (clientID*cfg.requests + reqIdx) % len(aiWorkload.keys)
	key := aiWorkload.keys[keyIdx]

	aiWorkload.mu.RLock()
	embedding := aiWorkload.embeddings[key]
	aiWorkload.mu.RUnlock()

	req := VectorPutReq{Data: vectorToBytes(embedding), Vector: toFloat32(embedding), TTL: "1h"}
	payload, _ := json.Marshal(req)

	return tcp.Packet{Opcode: tcp.OpVectorPut, Key: key, Value: payload}
}

func buildGraphPacket(cfg *Config, gw *GraphWorkload, clientID, reqIdx int, isRead bool) tcp.Packet {
	nodeID := gw.nodeIDs[rand.Intn(len(gw.nodeIDs))]
	if isRead {
		return tcp.Packet{Opcode: tcp.OpGet, Key: nodeID}
	}
	return tcp.Packet{Opcode: tcp.OpSet, Key: nodeID, Value: []byte("graph_data")}
}

func buildTimePacket(cfg *Config, tw *TimeWorkload, clientID, reqIdx int, isRead bool) tcp.Packet {
	stream := tw.streamKeys[rand.Intn(len(tw.streamKeys))]
	if isRead {
		return tcp.Packet{Opcode: tcp.OpStreamRange, Key: stream, Value: []byte(`{}`)}
	}
	payload := []byte(fmt.Sprintf(`{"val":%f}`, rand.Float64()))
	return tcp.Packet{Opcode: tcp.OpStreamAppend, Key: stream, Value: payload}
}

func generateRandomVector(dim int) []float64 {
	vec := make([]float64, dim)
	var norm float64
	for i := 0; i < dim; i++ {
		vec[i] = rand.NormFloat64()
		norm += vec[i] * vec[i]
	}
	norm = math.Sqrt(norm)
	for i := 0; i < dim; i++ {
		vec[i] /= norm
	}
	return vec
}

func perturbVector(vec []float64, noise float64) []float64 {
	res := make([]float64, len(vec))
	var norm float64
	for i, v := range vec {
		res[i] = v + (rand.Float64()-0.5)*noise
		norm += res[i] * res[i]
	}
	norm = math.Sqrt(norm)
	for i := range res {
		res[i] /= norm
	}
	return res
}

func vectorToBytes(vec []float64) []byte {
	return []byte(fmt.Sprintf("%v", vec))
}

func printBanner(cfg *Config) {
	fmt.Println("========================================")
	fmt.Println("   POMAI CACHE BENCHMARK TOOL")
	fmt.Println("========================================")
	fmt.Printf("Server:         %s\n", cfg.addr)
	fmt.Printf("Clients:        %d\n", cfg.clients)
	fmt.Printf("Requests:       %d\n", cfg.requests)
	fmt.Printf("Pipeline:       %d\n", cfg.pipeline)
	fmt.Printf("Data Size:      %d bytes\n", cfg.dataSize)

	mode := "Standard (KV)"
	if cfg.zsetMode {
		mode = "Sorted Set (ZSet)"
	} else if cfg.bitmapMode {
		mode = fmt.Sprintf("Bitmap (Offset: %d)", cfg.bitmapMaxOffset)
	} else if cfg.aiMode {
		mode = fmt.Sprintf("AI Semantic (Dim: %d)", cfg.vectorDim)
	} else if cfg.graphMode {
		mode = "Graph"
	} else if cfg.timeMode {
		mode = "TimeStream"
	}
	fmt.Printf("Mode:           %s\n", mode)
	fmt.Println("========================================")
}

func printResult(run int, result BenchmarkResult) {
	fmt.Printf("[Benchmark Run %d] Results:\n", run)
	fmt.Printf("  Duration:       %.3fs\n", result.Duration.Seconds())
	fmt.Printf("  Total Ops:      %d\n", result.TotalOps)
	fmt.Printf("  Throughput:     %.0f req/s\n", result.Throughput)
	fmt.Printf("  Bandwidth:      %.2f MB/s\n", result.Bandwidth)
	fmt.Printf("  Avg Latency:    %.3f ms\n", result.AvgLatency)
	fmt.Printf("  Min Latency:    %.3f ms\n", result.MinLatency)
	fmt.Printf("  Max Latency:    %.3f ms\n", result.MaxLatency)
	fmt.Printf("  Errors:         %d (%.2f%%)\n", result.Errors, float64(result.Errors)/float64(result.TotalOps)*100)

	if len(result.Latencies) > 0 {
		fmt.Println("Latency Percentiles:")
		fmt.Printf("  P50:   %.3f ms\n", percentile(result.Latencies, 0.50))
		fmt.Printf("  P90:   %.3f ms\n", percentile(result.Latencies, 0.90))
		fmt.Printf("  P99:   %.3f ms\n", percentile(result.Latencies, 0.99))
		fmt.Printf("  P99.9: %.3f ms\n", percentile(result.Latencies, 0.999))
		fmt.Printf("  Max:   %.3f ms\n", result.MaxLatency)
	}
	fmt.Println()
}

func percentile(latencies []time.Duration, p float64) float64 {
	if len(latencies) == 0 {
		return 0
	}
	idx := int(float64(len(latencies)) * p)
	if idx >= len(latencies) {
		idx = len(latencies) - 1
	}
	return float64(latencies[idx]) / 1e6
}

func printAverageResults(results []BenchmarkResult) {
	if len(results) == 0 {
		return
	}

	var avgThroughput, avgBandwidth, avgLatency, avgErrors float64

	for _, r := range results {
		avgThroughput += r.Throughput
		avgBandwidth += r.Bandwidth
		avgLatency += r.AvgLatency
		avgErrors += float64(r.Errors) / float64(r.TotalOps) * 100
	}

	n := float64(len(results))
	fmt.Println("========================================")
	fmt.Println("AVERAGE RESULTS (" + fmt.Sprint(len(results)) + " runs)")
	fmt.Println("========================================")
	fmt.Printf("  Throughput:     %.0f req/s\n", avgThroughput/n)
	fmt.Printf("  Bandwidth:      %.2f MB/s\n", avgBandwidth/n)
	fmt.Printf("  Avg Latency:    %.3f ms\n", avgLatency/n)
	fmt.Printf("  Error Rate:     %.2f%%\n", avgErrors/n)
	fmt.Println("========================================")
}
