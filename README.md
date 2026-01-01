Dưới đây là bản **README.md** chuyên nghiệp, đầy đủ và "ngầu" dành cho dự án Pomai Cache của bạn. Nó được viết theo phong cách của các dự án Open Source lớn (như Redis, Etcd) để bạn có thể tự tin đẩy lên GitHub.

Bạn hãy tạo file `README.md` ở thư mục gốc và dán nội dung này vào.

---

# Pomai Cache

> **High-Performance, Distributed In-Memory Cache written in Go.**

**Pomai Cache** is a robust, concurrent, and persistent key-value store designed for low latency and high throughput. It features a custom binary protocol for maximum performance, smart eviction policies (TinyLFU), and a dual-plane architecture (TCP for data, HTTP for observability).

---

## Key Features

* **High Performance:**
* **Sharded Architecture:** Minimizes lock contention using 256+ concurrent shards.
* **Custom Binary Protocol:** Zero-allocation parsing with a fixed 8-byte header (Port `9090`).
* **Bloom Filters:** Prevents cache penetration by filtering out non-existent keys before accessing memory.


* **Smart Eviction (TinyLFU):**
* Uses **Count-Min Sketch** to track frequency efficiently.
* Admits only "hot" items when full, protecting the cache from one-hit wonders.


* **Persistence & Durability:**
* **WAL (Write-Ahead Log):** Ensures data integrity even after a crash.
* **Snapshots:** Periodic background snapshots for faster recovery.


* **Observability:**
* Dedicated **HTTP Admin API** (Port `8080`).
* **Prometheus Metrics** endpoint (`/metrics`) built-in.
* JSON Stats API (`/v1/stats`).


* **Production Ready:**
* Fully containerized with Docker & Docker Compose.
* Graceful Shutdown & Signal Handling.
* Environment-based configuration.



---

## Architecture

Pomai Cache adopts a **Dual-Protocol** design:

| Protocol | Port | Description |
| --- | --- | --- |
| **TCP Binary** | `9090` | **Data Plane.** Ultra-fast `SET`, `GET`, `DEL` operations. Used by application clients. |
| **HTTP/REST** | `8080` | **Control Plane.** Health checks, Statistics, Prometheus Metrics. Used by DevOps/Sysadmins. |

---

## Getting Started

### Method 1: Docker (Recommended)

The easiest way to run Pomai Cache is via Docker.

```bash
# Build the image
docker build -t pomai-cache:latest .

# Run the container
docker run -d \
  --name pomai \
  -p 9090:9090 \
  -p 8080:8080 \
  -v pomai_data:/root/data \
  -e PER_TENANT_CAPACITY_BYTES=536870912 \
  pomai-cache:latest

```

### Method 2: Docker Compose (With Monitoring)

Spin up a full stack with **Grafana** and **Prometheus**.

```bash
docker-compose up -d

```

* **Pomai Cache:** `localhost:9090` (TCP), `localhost:8080` (HTTP)
* **Prometheus:** `localhost:9091`
* **Grafana:** `localhost:3000` (Login: `admin`/`admin`)

### Method 3: Build from Source

```bash
# Clone the repo
git clone https://github.com/your-username/pomai-cache.git
cd pomai-cache

# Build binary
go build -o pomai-server ./cmd/server/main.go

# Run
./pomai-server

```

---

## Configuration

Pomai Cache is configured via Environment Variables (`.env` supported).

| Variable | Default | Description |
| --- | --- | --- |
| `TCP_PORT` | `9090` | Port for Binary Data Protocol. |
| `PORT` | `8080` | Port for HTTP Admin API. |
| `DATA_DIR` | `./data` | Directory to store WAL and Snapshots. |
| `CACHE_SHARDS` | `256` | Number of shards (higher = better concurrency). |
| `PER_TENANT_CAPACITY_BYTES` | `0` | Max memory per tenant (0 = Unlimited). |
| `PERSISTENCE_TYPE` | `noop` | `file` (Snapshot only), `wal` (WAL + Snapshot), or `noop`. |
| `WRITE_BUFFER_SIZE` | `1000` | Write-behind buffer size. |

---

## Client Usage

Pomai Cache uses a custom binary protocol. Here is a simple Go client example:

```go
package main

import (
	"fmt"
	"log"
	"github.com/your-username/pomai-cache/packages/client"
)

func main() {
	// Connect to Pomai Cache
	cli := client.NewClient("localhost:9090")

	// SET a value
	err := cli.Set("user:100", []byte("Hello Pomai"))
	if err != nil {
		log.Fatal(err)
	}

	// GET a value
	val, err := cli.Get("user:100")
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("Got value: %s\n", string(val))
}

```

### Binary Protocol Specification

Each packet consists of an 8-byte header followed by the payload.

```text
| Magic (1B) | Opcode (1B) | KeyLen (2B) | ValLen (4B) | ... Payload ... |
|     'P'    |  1=SET...   |   uint16    |   uint32    | KeyBytes | ValueBytes |
```

---

## Benchmarks

Benchmark ran on a standard laptop (Intel i5-4310U, 2 Cores):

| Concurrency | Requests | Duration | Throughput |
| --- | --- | --- | --- |
| 1 (Sequential) | 100,000 | 18.6s | ~5,300 req/s |
| **50 (Concurrent)** | **200,000** | **8.89s** | **~22,500 req/s** |

*> Note: Performance is currently CPU-bound on the test machine. On server-grade hardware with more cores, throughput is expected to scale linearly.*

---

## Roadmap

* [x] Core Engine & Sharding
* [x] TinyLFU Eviction Policy
* [x] Custom TCP Protocol
* [x] WAL Persistence
* [ ] Client Pipelining Support
* [ ] Distributed Cluster (Raft Consensus)

---

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3. Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the Branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## License

Distributed under the MIT License. See `LICENSE` for more information.