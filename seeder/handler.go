package seeder

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/codebabbler/bittorrent-client-go/peer"
)

// handlePeer manages a single peer connection for seeding.
func handlePeer(conn net.Conn, infoHash [20]byte, pieces [][]byte, numPieces int) {
	defer conn.Close()

	fmt.Fprintf(os.Stderr, "Seeder: new connection from %s\n", conn.RemoteAddr())

	// Receive handshake from peer
	peerInfoHash, peerID, err := peer.ReceiveHandshake(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Seeder: handshake error: %v\n", err)
		return
	}

	// Validate info hash
	if peerInfoHash != infoHash {
		fmt.Fprintf(os.Stderr, "Seeder: info hash mismatch from %s\n", conn.RemoteAddr())
		return
	}

	// Send handshake back
	_, _, err = peer.DoHandshake(conn, infoHash[:], false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Seeder: send handshake error: %v\n", err)
		return
	}
	_ = peerID // logged but not used further

	// Send bitfield
	bitfield := BuildBitfield(numPieces)
	err = peer.WriteMessage(conn, peer.MsgBitfield, bitfield)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Seeder: send bitfield error: %v\n", err)
		return
	}

	// Main loop: handle peer messages
	peerInterested := false
	for {
		id, payload, err := peer.ReadMessage(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Seeder: read error from %s: %v\n", conn.RemoteAddr(), err)
			return
		}

		if payload == nil {
			continue // keepalive
		}

		switch id {
		case peer.MsgInterested:
			peerInterested = true
			// Send unchoke
			err = peer.WriteMessage(conn, peer.MsgUnchoke, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Seeder: send unchoke error: %v\n", err)
				return
			}

		case peer.MsgNotInterested:
			peerInterested = false
			// Send choke
			err = peer.WriteMessage(conn, peer.MsgChoke, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Seeder: send choke error: %v\n", err)
				return
			}

		case peer.MsgRequest:
			if !peerInterested {
				continue // ignore requests from choked peers
			}

			if len(payload) < 12 {
				continue
			}

			pieceIndex, begin, length := peer.ParseRequest(payload)

			if int(pieceIndex) >= len(pieces) {
				fmt.Fprintf(os.Stderr, "Seeder: invalid piece index %d\n", pieceIndex)
				continue
			}

			pieceData := pieces[pieceIndex]
			if int(begin+length) > len(pieceData) {
				fmt.Fprintf(os.Stderr, "Seeder: request out of bounds: piece=%d begin=%d length=%d\n",
					pieceIndex, begin, length)
				continue
			}

			// Build piece response: index(4) + begin(4) + block
			block := pieceData[begin : begin+length]
			respPayload := make([]byte, 8+len(block))
			binary.BigEndian.PutUint32(respPayload[0:4], pieceIndex)
			binary.BigEndian.PutUint32(respPayload[4:8], begin)
			copy(respPayload[8:], block)

			err = peer.WriteMessage(conn, peer.MsgPiece, respPayload)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Seeder: send piece error: %v\n", err)
				return
			}

		case peer.MsgCancel:
			// Ignore cancel for now (simplified)
			continue
		}
	}
}
