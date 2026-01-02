// File: cmd/pomai-bench/main.go
package main

import (
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
)

type Config struct {
	addr          string
	clients       int
	requests      int
	pipeline      int
	dataSize      int
	workloadRatio float64
	runs          int
	aiMode        bool
	vectorDim     int
	semanticNoise float64
}

type BenchmarkResult struct {
	Duration   time.Duration
	TotalOps   int
	Throughput float64
	Bandwidth  float64
	AvgLatency float64
	MinLatency float64
	MaxLatency float64
	Errors     int64
	Latencies  []time.Duration
}

type AIWorkload struct {
	embeddings map[string][]float64
	keys       []string
	mu         sync.RWMutex
}

func main() {
	flag.Parse()

	cfg := &Config{
		addr:          *addr,
		clients:       *clients,
		requests:      *requests,
		pipeline:      *pipeline,
		dataSize:      *dataSize,
		workloadRatio: *workloadRatio,
		runs:          *runs,
		aiMode:        *aiMode,
		vectorDim:     *vectorDim,
		semanticNoise: *semanticNoise,
	}

	printBanner(cfg)

	var aiWorkload *AIWorkload
	if cfg.aiMode {
		log.Println("Initializing AI workload...")
		aiWorkload = initAIWorkload(cfg)
		log.Printf("Generated %d embeddings (%d dims)", len(aiWorkload.keys), cfg.vectorDim)
	}

	warmup(cfg, aiWorkload)

	results := make([]BenchmarkResult, cfg.runs)

	for i := 0; i < cfg.runs; i++ {
		fmt.Printf("\nRun %d/%d:\n\n", i+1, cfg.runs)
		results[i] = runBenchmark(cfg, aiWorkload)
		printResult(results[i], i+1)
		time.Sleep(time.Second)
	}

	printAverageResults(results)
}

func printBanner(cfg *Config) {
	mode := "Mixed"
	if cfg.aiMode {
		mode = "AI Semantic"
	}

	fmt.Println("========================================")
	fmt.Println("   POMAI CACHE BENCHMARK TOOL")
	fmt.Println("========================================")
	fmt.Printf("Server:          %s\n", cfg.addr)
	fmt.Printf("Clients:        %d\n", cfg.clients)
	fmt.Printf("Requests:       %d\n", cfg.requests)
	fmt.Printf("Pipeline:       %d\n", cfg.pipeline)
	fmt.Printf("Data Size:      %d bytes\n", cfg.dataSize)
	fmt.Printf("Mode:           %s (Ratio: %.1f)\n", mode, cfg.workloadRatio)
	if cfg.aiMode {
		fmt.Printf("Vector Dim:     %d\n", cfg.vectorDim)
		fmt.Printf("Semantic Noise: %.2f\n", cfg.semanticNoise)
	}
	fmt.Println("========================================\n")
}

func initAIWorkload(cfg *Config) *AIWorkload {
	aw := &AIWorkload{
		embeddings: make(map[string][]float64),
		keys:       make([]string, 0, cfg.requests),
	}

	for i := 0; i < cfg.requests; i++ {
		key := fmt.Sprintf("emb_%d", i)
		aw.keys = append(aw.keys, key)
		aw.embeddings[key] = randomVector(cfg.vectorDim)
	}

	return aw
}

func warmup(cfg *Config, aiWorkload *AIWorkload) {
	log.Printf("Warming up (%d requests)...\n", 10000)

	client, err := tcp.NewClient(cfg.addr)
	if err != nil {
		log.Fatalf("Failed to connect:  %v", err)
	}
	defer client.Close()

	value := make([]byte, cfg.dataSize)
	for i := 0; i < 10000; i++ {
		var key string
		if cfg.aiMode && aiWorkload != nil {
			key = aiWorkload.keys[i%len(aiWorkload.keys)]
			value = vectorToBytes(aiWorkload.embeddings[key])
		} else {
			key = fmt.Sprintf("key%d", i)
			for j := range value {
				value[j] = byte(i % 256)
			}
		}
		client.Set(key, value)
	}
}

func runBenchmark(cfg *Config, aiWorkload *AIWorkload) BenchmarkResult {
	var wg sync.WaitGroup
	var totalOps atomic.Int64
	var totalErrors atomic.Int64
	var latenciesMu sync.Mutex
	allLatencies := make([]time.Duration, 0, cfg.requests)

	reqsPerClient := cfg.requests / cfg.clients

	start := time.Now()

	for i := 0; i < cfg.clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runClient(cfg, id, reqsPerClient, aiWorkload, &totalOps, &totalErrors, &latenciesMu, &allLatencies)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	sort.Slice(allLatencies, func(i, j int) bool {
		return allLatencies[i] < allLatencies[j]
	})

	return BenchmarkResult{
		Duration:   duration,
		TotalOps:   int(totalOps.Load()),
		Throughput: float64(totalOps.Load()) / duration.Seconds(),
		Bandwidth:  float64(totalOps.Load()*int64(cfg.dataSize)) / duration.Seconds() / 1024 / 1024,
		AvgLatency: avgLatency(allLatencies),
		MinLatency: minLatency(allLatencies),
		MaxLatency: maxLatency(allLatencies),
		Errors:     totalErrors.Load(),
		Latencies:  allLatencies,
	}
}

func runClient(cfg *Config, id, reqsPerClient int, aiWorkload *AIWorkload,
	totalOps, totalErrors *atomic.Int64, latenciesMu *sync.Mutex, allLatencies *[]time.Duration) {

	client, err := tcp.NewClient(cfg.addr)
	if err != nil {
		log.Printf("Client %d failed to connect: %v", id, err)
		return
	}
	defer client.Close()

	value := make([]byte, cfg.dataSize)
	localLatencies := make([]time.Duration, 0, reqsPerClient)

	for i := 0; i < reqsPerClient; i += cfg.pipeline {
		batchSize := cfg.pipeline
		if i+batchSize > reqsPerClient {
			batchSize = reqsPerClient - i
		}

		packets := make([]tcp.Packet, batchSize)
		responses := make([]tcp.Packet, batchSize)

		for j := 0; j < batchSize; j++ {
			reqIdx := i + j
			isRead := rand.Float64() < cfg.workloadRatio

			if cfg.aiMode && aiWorkload != nil {
				packets[j] = buildAIPacket(cfg, aiWorkload, id, reqIdx, isRead, value)
			} else {
				packets[j] = buildStandardPacket(cfg, id, reqIdx, isRead, value)
			}
		}

		start := time.Now()
		err := client.PipelineFast(packets, responses)
		latency := time.Since(start)

		if err != nil {
			totalErrors.Add(1)
			continue
		}

		totalOps.Add(int64(batchSize))
		for j := 0; j < batchSize; j++ {
			localLatencies = append(localLatencies, latency/time.Duration(batchSize))
		}
	}

	latenciesMu.Lock()
	*allLatencies = append(*allLatencies, localLatencies...)
	latenciesMu.Unlock()
}

func buildAIPacket(cfg *Config, aiWorkload *AIWorkload, clientID, reqIdx int, isRead bool, value []byte) tcp.Packet {
	if isRead {
		targetIdx := (clientID*cfg.requests + reqIdx) % len(aiWorkload.keys)
		targetKey := aiWorkload.keys[targetIdx]

		aiWorkload.mu.RLock()
		targetVec := aiWorkload.embeddings[targetKey]
		aiWorkload.mu.RUnlock()

		queryVec := perturbVector(targetVec, cfg.semanticNoise)
		nearestKey := findNearest(aiWorkload, queryVec)

		return tcp.Packet{
			Opcode: tcp.OpGet,
			Key:    nearestKey,
		}
	}

	keyIdx := (clientID*cfg.requests + reqIdx) % len(aiWorkload.keys)
	key := aiWorkload.keys[keyIdx]

	aiWorkload.mu.RLock()
	embedding := aiWorkload.embeddings[key]
	aiWorkload.mu.RUnlock()

	value = vectorToBytes(embedding)

	return tcp.Packet{
		Opcode: tcp.OpSet,
		Key:    key,
		Value:  value,
	}
}

func buildStandardPacket(cfg *Config, clientID, reqIdx int, isRead bool, value []byte) tcp.Packet {
	key := fmt.Sprintf("key%d", clientID*cfg.requests+reqIdx)

	if isRead {
		return tcp.Packet{
			Opcode: tcp.OpGet,
			Key:    key,
		}
	}

	for j := range value {
		value[j] = byte((clientID + reqIdx) % 256)
	}

	return tcp.Packet{
		Opcode: tcp.OpSet,
		Key:    key,
		Value:  value,
	}
}

func randomVector(dim int) []float64 {
	vec := make([]float64, dim)
	for i := range vec {
		vec[i] = rand.Float64()
	}
	return normalize(vec)
}

func perturbVector(vec []float64, noise float64) []float64 {
	newVec := make([]float64, len(vec))
	for i := range vec {
		newVec[i] = vec[i] + (rand.Float64()-0.5)*noise
	}
	return normalize(newVec)
}

func normalize(vec []float64) []float64 {
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)

	result := make([]float64, len(vec))
	for i, v := range vec {
		if norm > 0 {
			result[i] = v / norm
		}
	}
	return result
}

func findNearest(aiWorkload *AIWorkload, query []float64) string {
	aiWorkload.mu.RLock()
	defer aiWorkload.mu.RUnlock()

	var bestKey string
	bestSim := -1.0

	sampleSize := 100
	if len(aiWorkload.keys) < sampleSize {
		sampleSize = len(aiWorkload.keys)
	}

	for i := 0; i < sampleSize; i++ {
		idx := rand.Intn(len(aiWorkload.keys))
		key := aiWorkload.keys[idx]
		vec := aiWorkload.embeddings[key]
		sim := cosineSim(query, vec)
		if sim > bestSim {
			bestSim = sim
			bestKey = key
		}
	}

	return bestKey
}

func cosineSim(a, b []float64) float64 {
	dot := 0.0
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

func vectorToBytes(vec []float64) []byte {
	result := make([]byte, len(vec)*8)
	for i, v := range vec {
		bits := math.Float64bits(v)
		for j := 0; j < 8; j++ {
			result[i*8+j] = byte(bits >> (j * 8))
		}
	}
	return result
}

func avgLatency(latencies []time.Duration) float64 {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	return float64(total) / float64(len(latencies)) / 1e6
}

func minLatency(latencies []time.Duration) float64 {
	if len(latencies) == 0 {
		return 0
	}
	min := latencies[0]
	for _, l := range latencies {
		if l < min {
			min = l
		}
	}
	return float64(min) / 1e6
}

func maxLatency(latencies []time.Duration) float64 {
	if len(latencies) == 0 {
		return 0
	}
	max := latencies[0]
	for _, l := range latencies {
		if l > max {
			max = l
		}
	}
	return float64(max) / 1e6
}

func printResult(result BenchmarkResult, runNum int) {
	fmt.Printf("[Benchmark Run %d] Results:\n", runNum)
	fmt.Printf("  Duration:      %.3fs\n", result.Duration.Seconds())
	fmt.Printf("  Total Ops:     %d\n", result.TotalOps)
	fmt.Printf("  Throughput:    %d req/s\n", int(result.Throughput))
	fmt.Printf("  Bandwidth:     %.2f MB/s\n", result.Bandwidth)
	fmt.Printf("  Avg Latency:   %.3f ms\n", result.AvgLatency)
	fmt.Printf("  Min Latency:   %.3f ms\n", result.MinLatency)
	fmt.Printf("  Max Latency:   %.3f ms\n", result.MaxLatency)
	fmt.Printf("  Errors:        %d (%.2f%%)\n", result.Errors, float64(result.Errors)/float64(result.TotalOps)*100)

	if len(result.Latencies) > 0 {
		fmt.Println("Latency Percentiles:")
		fmt.Printf("  P50:    %.3f ms\n", percentile(result.Latencies, 0.50))
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
	avgThroughput /= n
	avgBandwidth /= n
	avgLatency /= n
	avgErrors /= n

	fmt.Println("========================================")
	fmt.Printf("AVERAGE RESULTS (%d runs)\n", len(results))
	fmt.Println("========================================")
	fmt.Printf("  Throughput:    %d req/s\n", int(avgThroughput))
	fmt.Printf("  Bandwidth:     %.2f MB/s\n", avgBandwidth)
	fmt.Printf("  Avg Latency:   %.3f ms\n", avgLatency)
	fmt.Printf("  Error Rate:    %.2f%%\n", avgErrors)
	fmt.Println("========================================\n")
}
