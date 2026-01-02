// File: internal/transport/tcp/protocol.go
package tcp

import (
	"bufio"
	"errors"
	"io"
	"net"
	"sync"
)

// ============================================================================
// PROTOCOL CONSTANTS
// ============================================================================

const (
	MagicByte = 'P'
	Version   = 1
)

// Opcodes
const (
	OpSet    = 1
	OpGet    = 2
	OpDel    = 3
	OpIncr   = 4
	OpMGet   = 5
	OpMSet   = 6
	OpExists = 7
	OpStats  = 8
)

// Status codes
const (
	StatusOK             = 0
	StatusKeyNotFound    = 1
	StatusServerError    = 2
	StatusInvalidRequest = 3
	StatusKeyTooLarge    = 4
	StatusQuotaExceeded  = 5
)

const HeaderSize = 8

// ============================================================================
// BUFFER POOLS
// ============================================================================

var (
	smallBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 4096)
			return &buf
		},
	}
	largeBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 65536)
			return &buf
		},
	}
	headerPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, HeaderSize)
			return &buf
		},
	}
)

// ============================================================================
// PACKET STRUCTURE
// ============================================================================

// Packet represents a protocol packet
type Packet struct {
	Opcode uint8
	Key    string
	Value  []byte
}

// String returns string representation of opcode
func (p *Packet) OpcodeString() string {
	switch p.Opcode {
	case OpSet:
		return "SET"
	case OpGet:
		return "GET"
	case OpDel:
		return "DEL"
	case OpIncr:
		return "INCR"
	case OpMGet:
		return "MGET"
	case OpMSet:
		return "MSET"
	case OpExists:
		return "EXISTS"
	case OpStats:
		return "STATS"
	default:
		return "UNKNOWN"
	}
}

// ============================================================================
// BUFFERED CONNECTION
// ============================================================================

// BufferedConn wraps a connection with buffered I/O
type BufferedConn struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	wmu    sync.Mutex
	rmu    sync.Mutex
}

// NewBufferedConn creates a new buffered connection
func NewBufferedConn(conn net.Conn) *BufferedConn {
	return &BufferedConn{
		conn:   conn,
		reader: bufio.NewReaderSize(conn, 65536),
		writer: bufio.NewWriterSize(conn, 65536),
	}
}

// WritePacket writes a packet to the connection
func (bc *BufferedConn) WritePacket(op uint8, key string, value []byte) error {
	bc.wmu.Lock()
	defer bc.wmu.Unlock()

	keyLen := len(key)
	valLen := len(value)

	if keyLen > 65535 {
		return errors.New("key too long")
	}

	totalSize := HeaderSize + keyLen + valLen
	bufPtr := getBuffer(totalSize)
	buf := *bufPtr

	if cap(buf) < totalSize {
		newBuf := make([]byte, totalSize)
		putBuffer(bufPtr)
		buf = newBuf
		bufPtr = &buf
	} else {
		buf = buf[:totalSize]
	}

	writeHeaderFast(buf, op, uint16(keyLen), uint32(valLen))
	copy(buf[HeaderSize:], key)
	copy(buf[HeaderSize+keyLen:], value)

	_, err := bc.writer.Write(buf)
	putBuffer(bufPtr)

	if err != nil {
		return err
	}

	return bc.writer.Flush()
}

// ReadPacket reads a packet from the connection
func (bc *BufferedConn) ReadPacket() (Packet, error) {
	bc.rmu.Lock()
	defer bc.rmu.Unlock()

	headerPtr := headerPool.Get().(*[]byte)
	header := *headerPtr

	if _, err := io.ReadFull(bc.reader, header); err != nil {
		headerPool.Put(headerPtr)
		return Packet{}, err
	}

	if header[0] != MagicByte {
		headerPool.Put(headerPtr)
		return Packet{}, errors.New("invalid magic byte")
	}

	op, keyLen, valLen := parseHeaderFast(header)
	headerPool.Put(headerPtr)

	totalBodyLen := int(keyLen) + int(valLen)
	bufPtr := getBuffer(totalBodyLen)
	body := *bufPtr

	if cap(body) < totalBodyLen {
		newBuf := make([]byte, totalBodyLen)
		putBuffer(bufPtr)
		body = newBuf
		bufPtr = &body
	} else {
		body = body[:totalBodyLen]
	}

	if _, err := io.ReadFull(bc.reader, body); err != nil {
		putBuffer(bufPtr)
		return Packet{}, err
	}

	key := string(body[:keyLen])
	value := make([]byte, valLen)
	copy(value, body[keyLen:])

	putBuffer(bufPtr)

	return Packet{
		Opcode: op,
		Key:    key,
		Value:  value,
	}, nil
}

// Close closes the connection
func (bc *BufferedConn) Close() error {
	return bc.conn.Close()
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// getBuffer gets a buffer from the pool
func getBuffer(size int) *[]byte {
	if size <= 4096 {
		return smallBufferPool.Get().(*[]byte)
	}
	return largeBufferPool.Get().(*[]byte)
}

// putBuffer returns a buffer to the pool
func putBuffer(buf *[]byte) {
	if cap(*buf) <= 4096 {
		smallBufferPool.Put(buf)
	} else if cap(*buf) <= 65536 {
		largeBufferPool.Put(buf)
	}
}

// parseHeaderFast parses header bytes
func parseHeaderFast(header []byte) (op uint8, keyLen uint16, valLen uint32) {
	op = header[1]
	keyLen = uint16(header[2])<<8 | uint16(header[3])
	valLen = uint32(header[4])<<24 | uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])
	return
}

// writeHeaderFast writes header bytes
func writeHeaderFast(buf []byte, op uint8, keyLen uint16, valLen uint32) {
	buf[0] = MagicByte
	buf[1] = op
	buf[2] = byte(keyLen >> 8)
	buf[3] = byte(keyLen)
	buf[4] = byte(valLen >> 24)
	buf[5] = byte(valLen >> 16)
	buf[6] = byte(valLen >> 8)
	buf[7] = byte(valLen)
}

// ============================================================================
// STANDALONE FUNCTIONS
// ============================================================================

// WritePacket writes a packet to a writer
func WritePacket(w io.Writer, op uint8, key string, value []byte) error {
	keyLen := len(key)
	valLen := len(value)

	if keyLen > 65535 {
		return errors.New("key too long")
	}

	totalSize := HeaderSize + keyLen + valLen
	bufPtr := getBuffer(totalSize)
	buf := *bufPtr

	if cap(buf) < totalSize {
		newBuf := make([]byte, totalSize)
		putBuffer(bufPtr)
		buf = newBuf
		bufPtr = &buf
	} else {
		buf = buf[:totalSize]
	}

	writeHeaderFast(buf, op, uint16(keyLen), uint32(valLen))
	copy(buf[HeaderSize:], key)
	copy(buf[HeaderSize+keyLen:], value)

	_, err := w.Write(buf)
	putBuffer(bufPtr)

	return err
}

// ReadPacket reads a packet from a reader
func ReadPacket(r io.Reader) (Packet, error) {
	headerPtr := headerPool.Get().(*[]byte)
	header := *headerPtr

	if _, err := io.ReadFull(r, header); err != nil {
		headerPool.Put(headerPtr)
		return Packet{}, err
	}

	if header[0] != MagicByte {
		headerPool.Put(headerPtr)
		return Packet{}, errors.New("invalid magic byte")
	}

	op, keyLen, valLen := parseHeaderFast(header)
	headerPool.Put(headerPtr)

	totalBodyLen := int(keyLen) + int(valLen)
	bufPtr := getBuffer(totalBodyLen)
	body := *bufPtr

	if cap(body) < totalBodyLen {
		newBuf := make([]byte, totalBodyLen)
		putBuffer(bufPtr)
		body = newBuf
		bufPtr = &body
	} else {
		body = body[:totalBodyLen]
	}

	if _, err := io.ReadFull(r, body); err != nil {
		putBuffer(bufPtr)
		return Packet{}, err
	}

	key := string(body[:keyLen])
	value := make([]byte, valLen)
	copy(value, body[keyLen:])

	putBuffer(bufPtr)

	return Packet{
		Opcode: op,
		Key:    key,
		Value:  value,
	}, nil
}

// ============================================================================
// PACKET BUFFER (REUSABLE)
// ============================================================================

// PacketBuffer provides reusable buffers for packet I/O
type PacketBuffer struct {
	headerBuf [HeaderSize]byte
	bodyBuf   []byte
}

// NewPacketBuffer creates a new packet buffer
func NewPacketBuffer() *PacketBuffer {
	return &PacketBuffer{
		bodyBuf: make([]byte, 4096),
	}
}

// ReadPacket reads a packet using the buffer
func (pb *PacketBuffer) ReadPacket(r io.Reader) (Packet, error) {
	if _, err := io.ReadFull(r, pb.headerBuf[:]); err != nil {
		return Packet{}, err
	}

	if pb.headerBuf[0] != MagicByte {
		return Packet{}, errors.New("invalid magic byte")
	}

	op, keyLen, valLen := parseHeaderFast(pb.headerBuf[:])

	totalBodyLen := int(keyLen) + int(valLen)

	if cap(pb.bodyBuf) < totalBodyLen {
		pb.bodyBuf = make([]byte, totalBodyLen)
	} else {
		pb.bodyBuf = pb.bodyBuf[:totalBodyLen]
	}

	if _, err := io.ReadFull(r, pb.bodyBuf); err != nil {
		return Packet{}, err
	}

	key := string(pb.bodyBuf[:keyLen])
	value := pb.bodyBuf[keyLen:totalBodyLen]

	return Packet{
		Opcode: op,
		Key:    key,
		Value:  value,
	}, nil
}

// WritePacket writes a packet using the buffer
func (pb *PacketBuffer) WritePacket(w io.Writer, op uint8, key string, value []byte) error {
	keyLen := len(key)
	valLen := len(value)

	if keyLen > 65535 {
		return errors.New("key too long")
	}

	totalSize := HeaderSize + keyLen + valLen

	if cap(pb.bodyBuf) < totalSize {
		pb.bodyBuf = make([]byte, totalSize)
	} else {
		pb.bodyBuf = pb.bodyBuf[:totalSize]
	}

	writeHeaderFast(pb.bodyBuf, op, uint16(keyLen), uint32(valLen))
	copy(pb.bodyBuf[HeaderSize:], key)
	copy(pb.bodyBuf[HeaderSize+keyLen:], value)

	_, err := w.Write(pb.bodyBuf)
	return err
}
