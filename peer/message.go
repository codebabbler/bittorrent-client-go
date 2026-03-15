package peer

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
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

// ReadMessage reads a length-prefixed message from the connection.
// Returns the message ID and payload (excluding the ID byte).
// For keepalives (length=0), returns id=0, nil payload, nil error with keepalive=true.
func ReadMessage(conn net.Conn) (id uint8, payload []byte, err error) {
	lengthBuf := make([]byte, 4)
	_, err = io.ReadFull(conn, lengthBuf)
	if err != nil {
		return 0, nil, err
	}

	msgLen := binary.BigEndian.Uint32(lengthBuf)
	if msgLen == 0 {
		// keepalive — return a sentinel
		return 0, nil, nil
	}

	msgBuf := make([]byte, msgLen)
	_, err = io.ReadFull(conn, msgBuf)
	if err != nil {
		return 0, nil, err
	}

	return msgBuf[0], msgBuf[1:], nil
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
