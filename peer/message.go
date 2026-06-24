package peer

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
)

// Message ID constants
const (
	MsgChoke         uint8 = 0
	MsgUnchoke       uint8 = 1
	MsgInterested    uint8 = 2
	MsgNotInterested uint8 = 3
	MsgHave          uint8 = 4
	MsgBitfield      uint8 = 5
	MsgRequest       uint8 = 6
	MsgPiece         uint8 = 7
	MsgCancel        uint8 = 8
	MsgExtension     uint8 = 20
)

// MessageBufferPool pools byte slice pointers of size 17000 to hold blocks + headers.
var MessageBufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 17000)
		return &b
	},
}

// readMessage is an optimized unexported function that uses the buffer pool to avoid allocations.
func readMessage(conn net.Conn) (id uint8, payload []byte, pBuf *[]byte, err error) {
	var lengthBuf [4]byte
	_, err = io.ReadFull(conn, lengthBuf[:])
	if err != nil {
		return 0, nil, nil, err
	}

	msgLen := binary.BigEndian.Uint32(lengthBuf[:])
	if msgLen == 0 {
		return 0, nil, nil, nil
	}

	var msgBuf []byte
	if msgLen <= 17000 {
		pBuf = MessageBufferPool.Get().(*[]byte)
		msgBuf = (*pBuf)[:msgLen]
	} else {
		msgBuf = make([]byte, msgLen)
	}

	_, err = io.ReadFull(conn, msgBuf)
	if err != nil {
		if pBuf != nil {
			MessageBufferPool.Put(pBuf)
		}
		return 0, nil, nil, err
	}

	return msgBuf[0], msgBuf[1:], pBuf, nil
}

// ReadMessage reads a length-prefixed message from the connection.
// Returns the message ID and payload (excluding the ID byte).
// For keepalives (length=0), returns id=0, nil payload, nil error.
func ReadMessage(conn net.Conn) (id uint8, payload []byte, err error) {
	id, payload, pBuf, err := readMessage(conn)
	if err == nil && pBuf != nil {
		// Handshake/initial callers of ReadMessage don't recycle buffers.
		// That is fine, they will be GC'ed normally. We don't recycle them here.
	}
	return id, payload, err
}

// WriteMessage writes a length-prefixed message to the connection.
func WriteMessage(conn net.Conn, id uint8, payload []byte) error {
	msg := make([]byte, 4+1+len(payload))
	binary.BigEndian.PutUint32(msg[0:4], uint32(1+len(payload)))
	msg[4] = id
	copy(msg[5:], payload)

	_, err := conn.Write(msg)
	return err
}

// ReadBitfield reads and discards the bitfield message.
func ReadBitfield(conn net.Conn) error {
	lengthBuf := make([]byte, 4)
	_, err := io.ReadFull(conn, lengthBuf)
	if err != nil {
		return fmt.Errorf("reading bitfield length: %w", err)
	}
	msgLen := binary.BigEndian.Uint32(lengthBuf)
	if msgLen > 0 {
		msgBuf := make([]byte, msgLen)
		_, err = io.ReadFull(conn, msgBuf)
		if err != nil {
			return fmt.Errorf("reading bitfield: %w", err)
		}
	}
	return nil
}

// ParseRequest parses a request message payload into index, begin, length.
func ParseRequest(payload []byte) (index, begin, length uint32) {
	index = binary.BigEndian.Uint32(payload[0:4])
	begin = binary.BigEndian.Uint32(payload[4:8])
	length = binary.BigEndian.Uint32(payload[8:12])
	return
}
