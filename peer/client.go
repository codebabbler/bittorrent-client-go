package peer

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/codebabbler/bittorrent-client-go/bencode"
)

// bufferedMsg represents a message that has been read but not yet processed.
type bufferedMsg struct {
	id      uint8
	payload []byte
}

// Client wraps a TCP connection to a BitTorrent peer.
type Client struct {
	Conn                   net.Conn
	Address                string
	PeerID                 [20]byte
	PeerMetadataExtId      int
	SupportsExtensions     bool
	ExtensionHandshakeSent bool
	bufferedMsgs           []bufferedMsg

	// Asynchronous state
	Choked                 bool
	Interested             bool
	LastRead               time.Time
	LastWrite              time.Time
	StallScore             int
	mu                     sync.Mutex
	OutstandingRequests    map[string]OutstandingRequest
	writeCh                chan []byte
	done                   chan struct{}
}

// NewClient dials a peer, performs the handshake, and reads the bitfield.
func NewClient(address string, infoHash []byte, extensions bool) (*Client, error) {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}

	// Set deadline for handshake and bitfield exchange
	err = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting deadline: %w", err)
	}

	peerID, supportsExtensions, err := DoHandshake(conn, infoHash, extensions)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if extensions && !supportsExtensions {
		conn.Close()
		return nil, fmt.Errorf("peer does not support extensions")
	}

	client := &Client{
		Conn:               conn,
		Address:            address,
		PeerID:             peerID,
		SupportsExtensions: supportsExtensions,
	}

	if extensions && supportsExtensions {
		extPayload := []byte("d1:md11:ut_metadatai1eee")
		err = WriteMessage(conn, MsgExtension, append([]byte{0}, extPayload...))
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("sending extension handshake: %w", err)
		}
		client.ExtensionHandshakeSent = true
	}

	// Read initial messages (like bitfield or extension handshake) with a short timeout
	for {
		err = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("setting read deadline: %w", err)
		}

		id, payload, err := ReadMessage(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break // no more messages for now
			}
			conn.Close()
			return nil, fmt.Errorf("reading initial message: %w", err)
		}

		if payload == nil {
			continue // keepalive
		}

		if id == MsgBitfield {
			// Consume bitfield (we don't store it)
		} else {
			if id == MsgExtension && len(payload) > 1 && payload[0] == 0 {
				dictStr := string(payload[1:])
				pos := 0
				extDict, _, err := bencode.DecodeDict(dictStr, &pos)
				if err == nil {
					if mDict, ok := extDict["m"].(map[string]interface{}); ok {
						if peerExtId, ok := mDict["ut_metadata"].(int); ok {
							client.PeerMetadataExtId = peerExtId
						}
					}
				}
			}
			// Buffer other messages (e.g. MsgExtension)
			client.bufferedMsgs = append(client.bufferedMsgs, bufferedMsg{id: id, payload: payload})
		}
	}

	// Reset deadline for subsequent message processing
	err = conn.SetDeadline(time.Time{})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("clearing deadline: %w", err)
	}

	return client, nil
}

// ReadMessage reads a message from the buffer if one exists, otherwise reads from the network.
func (c *Client) ReadMessage() (uint8, []byte, error) {
	if len(c.bufferedMsgs) > 0 {
		msg := c.bufferedMsgs[0]
		c.bufferedMsgs = c.bufferedMsgs[1:]
		return msg.id, msg.payload, nil
	}
	return ReadMessage(c.Conn)
}

// SetupExtensions performs the extension handshake.
func (c *Client) SetupExtensions() error {
	if c.PeerMetadataExtId != 0 {
		return nil
	}
	peerExtId, err := c.DoExtensionHandshake()
	if err != nil {
		return err
	}
	c.PeerMetadataExtId = peerExtId
	return nil
}

// FetchMetadata requests and returns the raw metadata bytes from the peer.
func (c *Client) FetchMetadata() ([]byte, error) {
	return c.RequestMetadata(c.PeerMetadataExtId)
}

// SendInterested sends an interested message.
func (c *Client) SendInterested() error {
	return WriteMessage(c.Conn, MsgInterested, nil)
}

// WaitForUnchoke waits until the peer sends an unchoke message.
func (c *Client) WaitForUnchoke() error {
	err := c.Conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err != nil {
		return fmt.Errorf("setting unchoke deadline: %w", err)
	}
	defer c.Conn.SetDeadline(time.Time{})

	for {
		id, payload, err := c.ReadMessage()
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
	err := c.Conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("setting piece deadline: %w", err)
	}
	defer c.Conn.SetDeadline(time.Time{})

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
		id, payload, err := c.ReadMessage()
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
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done != nil {
		select {
		case <-c.done:
			return
		default:
			close(c.done)
		}
	}
	c.Conn.Close()
}
