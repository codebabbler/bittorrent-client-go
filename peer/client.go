package peer

import (
	"encoding/binary"
	"fmt"
	"net"
)

// Client wraps a TCP connection to a BitTorrent peer.
type Client struct {
	Conn              net.Conn
	PeerID            [20]byte
	PeerMetadataExtId int
}

// NewClient dials a peer, performs the handshake, and reads the bitfield.
func NewClient(address string, infoHash []byte, extensions bool) (*Client, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}

	peerID, err := DoHandshake(conn, infoHash, extensions)
	if err != nil {
		conn.Close()
		return nil, err
	}

	err = ReadBitfield(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return &Client{Conn: conn, PeerID: peerID}, nil
}

// SetupExtensions performs the extension handshake.
func (c *Client) SetupExtensions() error {
	peerExtId, err := DoExtensionHandshake(c.Conn)
	if err != nil {
		return err
	}
	c.PeerMetadataExtId = peerExtId
	return nil
}

// FetchMetadata requests and returns the raw metadata bytes from the peer.
func (c *Client) FetchMetadata() ([]byte, error) {
	return RequestMetadata(c.Conn, c.PeerMetadataExtId)
}

// SendInterested sends an interested message.
func (c *Client) SendInterested() error {
	return WriteMessage(c.Conn, MsgInterested, nil)
}

// WaitForUnchoke waits until the peer sends an unchoke message.
func (c *Client) WaitForUnchoke() error {
	for {
		id, payload, err := ReadMessage(c.Conn)
		if err != nil {
			return fmt.Errorf("waiting for unchoke: %w", err)
		}
		if payload == nil {
			continue // keepalive
		}
		if id == MsgUnchoke {
			return nil
		}
	}
}

// DownloadPiece downloads a single piece by requesting 16 KiB blocks.
func (c *Client) DownloadPiece(pieceIndex, pieceLength int) ([]byte, error) {
	blockSize := 16384
	totalBlocks := (pieceLength + blockSize - 1) / blockSize
	pieceData := make([]byte, pieceLength)

	// Send all block requests
	for i := 0; i < totalBlocks; i++ {
		offset := i * blockSize
		length := blockSize
		if offset+length > pieceLength {
			length = pieceLength - offset
		}

		payload := make([]byte, 12)
		binary.BigEndian.PutUint32(payload[0:4], uint32(pieceIndex))
		binary.BigEndian.PutUint32(payload[4:8], uint32(offset))
		binary.BigEndian.PutUint32(payload[8:12], uint32(length))
		err := WriteMessage(c.Conn, MsgRequest, payload)
		if err != nil {
			return nil, fmt.Errorf("sending block request: %w", err)
		}
	}

	// Receive all blocks
	blocksReceived := 0
	for blocksReceived < totalBlocks {
		id, payload, err := ReadMessage(c.Conn)
		if err != nil {
			return nil, fmt.Errorf("reading piece block: %w", err)
		}
		if payload == nil {
			continue // keepalive
		}
		if id != MsgPiece {
			continue
		}
		begin := binary.BigEndian.Uint32(payload[4:8])
		copy(pieceData[begin:], payload[8:])
		blocksReceived++
	}

	return pieceData, nil
}

// Close closes the connection.
func (c *Client) Close() {
	c.Conn.Close()
}
