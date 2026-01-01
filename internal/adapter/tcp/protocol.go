package tcp

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	MagicByte = 'P'
	OpSet     = 1
	OpGet     = 2
	OpDel     = 3
)

// Header cố định 8 bytes
const HeaderSize = 8

type Packet struct {
	Opcode uint8
	Key    string
	Value  []byte
}

// WritePacket đóng gói dữ liệu thành Binary gửi đi
func WritePacket(w io.Writer, op uint8, key string, value []byte) error {
	keyLen := len(key)
	valLen := len(value)

	// Kiểm tra giới hạn
	if keyLen > 65535 {
		return errors.New("key too long")
	}

	// 1. Buffer cho Header (8 bytes)
	// [Magic 1] [Op 1] [KeyLen 2] [ValLen 4]
	buf := make([]byte, HeaderSize)
	buf[0] = MagicByte
	buf[1] = op
	binary.BigEndian.PutUint16(buf[2:4], uint16(keyLen))
	binary.BigEndian.PutUint32(buf[4:8], uint32(valLen))

	// 2. Ghi Header
	if _, err := w.Write(buf); err != nil {
		return err
	}

	// 3. Ghi Body (Key + Value)
	if _, err := w.Write([]byte(key)); err != nil {
		return err
	}
	if valLen > 0 {
		if _, err := w.Write(value); err != nil {
			return err
		}
	}

	return nil
}

// ReadPacket đọc và parse dữ liệu nhị phân
func ReadPacket(r io.Reader) (Packet, error) {
	// 1. Đọc đúng 8 bytes Header
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return Packet{}, err
	}

	// 2. Validate Magic Byte
	if header[0] != MagicByte {
		return Packet{}, errors.New("invalid magic byte")
	}

	op := header[1]
	keyLen := binary.BigEndian.Uint16(header[2:4])
	valLen := binary.BigEndian.Uint32(header[4:8])

	// 3. Đọc Body (Key + Value)
	totalBodyLen := int(keyLen) + int(valLen)
	body := make([]byte, totalBodyLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return Packet{}, err
	}

	// 4. Tách Key và Value (Zero-copy slicing nếu tối ưu, ở đây copy string cho an toàn)
	key := string(body[:keyLen])
	value := body[keyLen:]

	return Packet{
		Opcode: op,
		Key:    key,
		Value:  value,
	}, nil
}
