package peer

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
)

// DoHandshake performs the BitTorrent handshake on an existing connection.
// If extensions is true, sets bit 20 in the reserved bytes.
// Returns the peer's ID from the response.
func DoHandshake(conn net.Conn, infoHash []byte, extensions bool) ([20]byte, error) {
	var handshake [68]byte
	handshake[0] = 19
	copy(handshake[1:20], []byte("BitTorrent protocol"))

	if extensions {
		handshake[25] = 0x10 // set bit 20: extension support
	}

	copy(handshake[28:48], infoHash)

	var peerId [20]byte
	_, err := rand.Read(peerId[:])
	if err != nil {
		return [20]byte{}, fmt.Errorf("generating peer ID: %w", err)
	}
	copy(handshake[48:68], peerId[:])

	_, err = conn.Write(handshake[:])
	if err != nil {
		return [20]byte{}, fmt.Errorf("sending handshake: %w", err)
	}

	var response [68]byte
	_, err = io.ReadFull(conn, response[:])
	if err != nil {
		return [20]byte{}, fmt.Errorf("reading handshake: %w", err)
	}

	var peerID [20]byte
	copy(peerID[:], response[48:68])
	return peerID, nil
}

// ReceiveHandshake reads an incoming handshake (for seeding — we receive first).
// Returns the peer's info hash and peer ID.
func ReceiveHandshake(conn net.Conn) ([20]byte, [20]byte, error) {
	var response [68]byte
	_, err := io.ReadFull(conn, response[:])
	if err != nil {
		return [20]byte{}, [20]byte{}, fmt.Errorf("reading incoming handshake: %w", err)
	}

	var infoHash [20]byte
	copy(infoHash[:], response[28:48])

	var peerID [20]byte
	copy(peerID[:], response[48:68])

	return infoHash, peerID, nil
}
