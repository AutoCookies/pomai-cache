package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/adapter/tcp"
)

func main() {
	serverAddr := "localhost:9090"
	totalRequests := 200000
	concurrency := 200

	fmt.Printf("Begin stress test: %d requests, %d threads...\n", totalRequests, concurrency)

	var wg sync.WaitGroup
	wg.Add(concurrency)

	start := time.Now()
	var successCount int64

	// Chia việc cho các luồng
	reqPerWorker := totalRequests / concurrency

	for i := 0; i < concurrency; i++ {
		go func(workerID int) {
			defer wg.Done()

			// Mỗi luồng mở 1 kết nối riêng (như production)
			conn, err := net.Dial("tcp", serverAddr)
			if err != nil {
				log.Printf("Worker %d connection error: %v", workerID, err)
				return
			}
			defer conn.Close()

			for j := 0; j < reqPerWorker; j++ {
				key := fmt.Sprintf("k-%d-%d", workerID, j)
				val := []byte("bench-data")

				// SET
				if err := tcp.WritePacket(conn, tcp.OpSet, key, val); err != nil {
					return
				}
				// READ RESPONSE (Quan trọng: phải đọc để không nghẽn buffer server)
				if _, err := tcp.ReadPacket(conn); err != nil {
					return
				}

				atomic.AddInt64(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	tps := float64(successCount) / duration.Seconds()

	fmt.Println("------------------------------------------------")
	fmt.Printf("Completed: %d/%d requests\n", successCount, totalRequests)
	fmt.Printf("Duration:  %v\n", duration)
	fmt.Printf("Speed:     %.0f requests/second\n", tps)
	fmt.Println("------------------------------------------------")
}
