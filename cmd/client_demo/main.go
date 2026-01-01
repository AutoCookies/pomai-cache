package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/adapter/tcp"
)

func main() {
	// 1. Kết nối vào Port 9090 (TCP Binary)
	conn, err := net.Dial("tcp", "localhost:9090")
	if err != nil {
		log.Fatal("Failed to connect to server:", err)
	}
	defer conn.Close()
	fmt.Println("Connected to Pomai Cache")

	// 2. Send 100,000 SET commands continuously
	start := time.Now()
	count := 100000

	fmt.Printf("Sending %d requests...\n", count)

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("user:%d", i)
		val := []byte("pomai-is-super-fast")

		// Gửi lệnh SET
		if err := tcp.WritePacket(conn, tcp.OpSet, key, val); err != nil {
			log.Fatal(err)
		}

		// Read response (Must read to avoid buffer blocking)
		res, err := tcp.ReadPacket(conn)
		if err != nil {
			log.Fatal(err)
		}
		if res.Opcode != 0 {
			log.Fatalf("Server error: %s", string(res.Value))
		}
	}

	duration := time.Since(start)
	fmt.Printf("Completed %d requests in %v\n", count, duration)
	fmt.Printf("Speed: %.0f requests/second\n", float64(count)/duration.Seconds())

	// 3. Test GET again
	tcp.WritePacket(conn, tcp.OpGet, "user:100", nil)
	res, _ := tcp.ReadPacket(conn)
	fmt.Printf("GET user:100 => %s\n", string(res.Value))
}
