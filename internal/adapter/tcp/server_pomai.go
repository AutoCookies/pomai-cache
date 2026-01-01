package tcp

import (
	"log"
	"net"

	"github.com/AutoCookies/pomai-cache/internal/engine"
)

type PomaiServer struct {
	tenants *engine.TenantManager
}

func NewPomaiServer(tenants *engine.TenantManager) *PomaiServer {
	return &PomaiServer{tenants: tenants}
}

func (s *PomaiServer) ListenAndServe(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("🚀 Pomai Binary Server listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *PomaiServer) handleConn(conn net.Conn) {
	defer conn.Close()
	store := s.tenants.GetStore("default") // Mặc định

	for {
		// 1. Đọc Packet theo chuẩn Pomai Protocol
		req, err := ReadPacket(conn)
		if err != nil {
			return // Client đóng kết nối hoặc lỗi
		}

		// 2. Xử lý Logic
		var resVal []byte
		var resCode uint8 = 0 // 0 = OK, 1 = Error/Miss

		switch req.Opcode {
		case OpSet:
			err := store.Put(req.Key, req.Value, 0)
			if err != nil {
				resCode = 1
				resVal = []byte(err.Error())
			}
		case OpGet:
			val, found := store.Get(req.Key)
			if !found {
				resCode = 1 // Not Found
			} else {
				resVal = val
			}
		case OpDel:
			store.Delete(req.Key)
		}

		// 3. Phản hồi (Tận dụng lại hàm WritePacket nhưng Opcode dùng làm Status Code)
		// Opcode trả về: 0 = Success, 1 = Error/NotFound
		// Key trả về: Rỗng (để tiết kiệm băng thông)
		err = WritePacket(conn, resCode, "", resVal)
		if err != nil {
			return
		}
	}
}
