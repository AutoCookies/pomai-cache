# Pomai Cache

Pomai Cache is a high-performance in-memory caching engine designed for modern application needs. It combines traditional key-value caching with advanced primitives such as vector (embedding) indexes (HNSW), a knowledge graph cache, probabilistic data structures, and adaptive eviction. Pomai is built to support AI/semantic workloads, real-time analytics, and graph-driven applications while providing low latency and high throughput.

This README gives an overview of the system, the main data types it supports, and how to build and run Pomai and the benchmark tools.

## Key Features

- Core key-value cache with TTL support and multiple shard implementations (lock-free adapters).
- Vector/embedding index using HNSW for fast approximate nearest neighbor search (ANN).
- Knowledge Graph cache with nodes, typed edges, shortest-path queries, PageRank, community detection and subgraph extraction.
- Bloom filter integration for fast negative lookups and incremental rebuilds.
- Eviction manager with multiple modes (standard eviction, hard-override / overwrite tail entries).
- Probabilistic structures (Bloom filters, sketches) and frequency estimation for TinyLFU-like admission.
- Bench tool for workload simulation: key-value, AI semantic (vector), and graph workloads.
- Thread-safe, low-latency design with options to tune efSearch (HNSW), shard counts and capacity.

## Components and Data Types

Pomai supports and extends common caching primitives:

- Key-Value
  - Plain binary values with optional TTL
  - Fast get/put/delete, multi-get/multi-set
  - Zero-copy options in internal Entry objects

- Vector / Embeddings
  - HNSW-based ANN index with SIMD-optimized distance functions
  - Create named indices, insert vectors, search (KNN), delete vectors
  - Adaptive `efSearch` tuning for recall / latency trade-offs
  - API: `PutWithEmbedding`, `SemanticSearch`, `CreateVectorIndex`, `InsertVector`

- Knowledge Graph
  - Add nodes with properties, add typed edges with weights and properties
  - Neighbor traversal with depth and edge-type filtering
  - Shortest path (Dijkstra), PageRank, community detection (Louvain or label propagation)
  - Subgraph extraction and graph statistics

- Probabilistic Data Structures
  - Bloom filters (with counting/scalable variants in roadmap)
  - Sketch data structures for frequency estimation and TinyLFU-style admission

- Eviction & Memory Control
  - Background eviction worker and on-demand eviction manager
  - Hard max override mode (overwrite LRU tail entries)
  - Memory controller hooks to integrate with quota/reservation systems

## Project Layout (high level)

- `internal/engine/core` — core store, shards, entry metadata, eviction integration, TTL cleaner
- `internal/engine/ttl` — TTL management and cleanup (including bloom rebuilding)
- `internal/engine/eviction` — eviction manager, heuristics, metrics
- `internal/engine/core/vector_store.go` and `packages/ds/vector` — vector index (HNSW) and distance SIMD implementations
- `packages/ds/graph` — knowledge graph engine
- `cmd/pomai-bench` — benchmark tool with `--ai-mode` and `--graph-mode`
- `internal/adapter/tcp` — client/server adapter used by bench tool (client example)

## Build

You need Go 1.20+ (or later). From project root:

```bash
# Build the benchmark tool
cd cmd/pomai-bench
go build -o ../../bin/pomai-bench

# (Optional) Build a server binary if present under cmd (name may vary)
# go build -o ../../bin/pomai-server ./cmd/pomai-server
```

Adjust package paths if your repo layout differs. The repository includes an in-repo HNSW implementation; an alternative is to use an external HNSW library if preferred.

## Run

### Running the server
If a server implementation is available in `cmd`, build and run it:

```bash
# Example (replace with actual server cmd if different)
./bin/pomai-server --addr=0.0.0.0:7600 --shards=256 --capacity-bytes=1073741824
```

Server flags (examples)
- `--addr` server bind address
- `--shards` number of shards
- `--capacity-bytes` total cache capacity

(See server main for all flags supported in your build.)

### Running the benchmark tool

The benchmark tool supports multiple modes: standard key-value, AI semantic (vector embedding), and graph workloads.

Build and run:

```bash
./bin/pomai-bench --addr=localhost:7600 --clients=50 --requests=1000000
```

AI semantic workload:

```bash
./bin/pomai-bench \
  --addr=localhost:7600 \
  --clients=50 \
  --requests=1000000 \
  --ai-mode \
  --vector-dim=128 \
  --semantic-noise=0.1
```

Graph workload:

```bash
./bin/pomai-bench \
  --addr=localhost:7600 \
  --clients=50 \
  --requests=1000000 \
  --graph-mode \
  --graph-nodes=10000 \
  --graph-degree=10
```

Key bench flags:
- `--clients` number of concurrent clients
- `--requests` total requests to issue
- `--pipeline` pipeline batch size
- `--ai-mode` enable vector workload (embeddings)
- `--vector-dim` vector dimension for AI mode
- `--graph-mode` run graph workload
- `--graph-nodes`, `--graph-degree` control graph size/topology

## Example Usage (Go API)

The store API exposes functions for vectors and graph operations.

Vector example:

```go
store := core.NewStore(256)
store.CreateVectorIndex("emb", 128, "cosine")
embedding := make([]float32, 128)
// fill embedding...
store.PutWithEmbedding("doc:1", []byte("document1"), embedding, 0)
results, err := store.SemanticSearch(embedding, 10)
```

Graph example:

```go
store := core.NewStore(256)
store.CreateGraph("social")
store.AddGraphNode("social", "alice", map[string]interface{}{"name":"Alice"})
store.AddGraphNode("social", "bob", map[string]interface{}{"name":"Bob"})
store.AddGraphEdge("social", "alice", "bob", "friend", 1.0, nil)
path, err := store.GraphShortestPath("social", "alice", "bob", 6)
neighbors, _ := store.GraphNeighbors("social", "alice", 2, "friend")
ranks, _ := store.GraphPageRank("social", 20)
```

## Configuration and Tuning

- Shard count: choose based on CPU cores to reduce contention.
- HNSW parameters:
  - `M` (connections per node) and `efConstruction` impact index quality and build time.
  - `efSearch` controls recall vs latency at query time. It is tunable at runtime and can be auto-tuned.
- Bloom filter: can be enabled to speed up negative lookups; rebuilds are triggered by TTL cleanup or manual operations.
- Eviction policies: the engine supports adaptive eviction; "hard max override" lets you overwrite tail entries under pressure.

## Operational Notes

- Memory accounting: store tracks bytes per-shard and global; optional MemoryController hook can be provided for integration with external memory management.
- TTL cleanup: background cleaner removes expired entries and can trigger incremental bloom rebuilds.
- Concurrency: core structures use shards, lock-free adapters and atomic counters to maximize throughput.
- Durability: snapshot/restore APIs are available to serialize/deserialize key-value contents. Graph and vector indices require separate export/import if necessary.

## Contributing

Contributions are welcome. Please follow the repository's coding standards:
- Add unit tests for new functionality.
- Document public API changes.
- Keep performance-sensitive code free of unnecessary allocations.
- Discuss large design changes in issues before implementing.

## License

Specify your project license here (e.g., MIT, Apache 2.0). Add a LICENSE file to the repository.

## Contact

For questions or collaboration, open an issue or contact the maintainers in the repository.
