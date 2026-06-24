package peer

import (
	"fmt"
	"time"

	"github.com/codebabbler/bittorrent-client-go/bencode"
)

// DoExtensionHandshake sends our extension handshake and receives the peer's.
// Returns the peer's ut_metadata extension ID.
func (c *Client) DoExtensionHandshake() (int, error) {
	// Set read/write deadline for extension handshake
	err := c.Conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return 0, fmt.Errorf("setting extension deadline: %w", err)
	}
	defer c.Conn.SetDeadline(time.Time{})

	// Send extension handshake: {"m": {"ut_metadata": 1}} if not sent yet
	if !c.ExtensionHandshakeSent {
		extPayload := []byte("d1:md11:ut_metadatai1eee")
		err = WriteMessage(c.Conn, MsgExtension, append([]byte{0}, extPayload...))
		if err != nil {
			return 0, fmt.Errorf("sending extension handshake: %w", err)
		}
		c.ExtensionHandshakeSent = true
	}

	// Receive extension handshake (skip non-extension messages)
	for {
		id, payload, err := c.ReadMessage()
		if err != nil {
			return 0, fmt.Errorf("reading extension handshake: %w", err)
		}
		if payload == nil {
			continue // keepalive
		}
		if id == MsgExtension {
			// payload[0] = ext msg id (0 = handshake), payload[1:] = bencoded dict
			dictStr := string(payload[1:])
			pos := 0
			extDict, _, err := bencode.DecodeDict(dictStr, &pos)
			if err != nil {
				return 0, fmt.Errorf("decoding extension handshake: %w", err)
			}
			mDict, ok := extDict["m"].(map[string]interface{})
			if !ok {
				return 0, fmt.Errorf("extension handshake missing 'm' dict")
			}
			peerExtId, ok := mDict["ut_metadata"].(int)
			if !ok {
				return 0, fmt.Errorf("extension handshake missing 'ut_metadata'")
			}
			return peerExtId, nil
		}
	}
}

// RequestMetadata requests all metadata pieces and returns the consolidated metadata.
func (c *Client) RequestMetadata(peerExtId int) ([]byte, error) {
	// Request piece 0 first to find out the total size.
	firstPiece, totalSize, err := c.requestMetadataPiece(peerExtId, 0)
	if err != nil {
		return nil, fmt.Errorf("requesting metadata piece 0: %w", err)
	}

	if totalSize <= 0 {
		return nil, fmt.Errorf("invalid metadata total_size: %d", totalSize)
	}

	// If metadata fits in one piece
	if len(firstPiece) >= totalSize {
		return firstPiece[:totalSize], nil
	}

	// Otherwise, allocate the full buffer and request the other pieces.
	metadata := make([]byte, totalSize)
	copy(metadata[0:], firstPiece)

	pieceSize := 16384
	numPieces := (totalSize + pieceSize - 1) / pieceSize

	for p := 1; p < numPieces; p++ {
		pieceData, _, err := c.requestMetadataPiece(peerExtId, p)
		if err != nil {
			return nil, fmt.Errorf("requesting metadata piece %d: %w", p, err)
		}
		offset := p * pieceSize
		if offset+len(pieceData) > totalSize {
			copy(metadata[offset:], pieceData[:totalSize-offset])
		} else {
			copy(metadata[offset:], pieceData)
		}
	}

	return metadata, nil
}

// requestMetadataPiece requests a single piece of metadata and returns its data and the total metadata size.
func (c *Client) requestMetadataPiece(peerExtId int, piece int) ([]byte, int, error) {
	// Set read/write deadline for metadata piece request
	err := c.Conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err != nil {
		return nil, 0, fmt.Errorf("setting metadata deadline: %w", err)
	}
	defer c.Conn.SetDeadline(time.Time{})

	// Send metadata request: {"msg_type": 0, "piece": <piece>}
	reqPayload := []byte(fmt.Sprintf("d8:msg_typei0e5:piecei%dee", piece))
	msg := append([]byte{byte(peerExtId)}, reqPayload...)
	err = WriteMessage(c.Conn, MsgExtension, msg)
	if err != nil {
		return nil, 0, fmt.Errorf("sending metadata request: %w", err)
	}

	// Receive metadata data response (skip non-extension messages)
	for {
		id, payload, err := c.ReadMessage()
		if err != nil {
			return nil, 0, fmt.Errorf("reading metadata response: %w", err)
		}
		if payload == nil {
			continue // keepalive
		}
		if id == MsgExtension {
			// payload[0] = our ut_metadata ID, payload[1:] = bencoded dict + raw metadata
			dictStr := string(payload[1:])
			pos := 0
			respDict, _, err := bencode.DecodeDict(dictStr, &pos)
			if err != nil {
				return nil, 0, fmt.Errorf("decoding metadata response dict: %w", err)
			}

			// Check msg_type (1 = data, 2 = reject)
			msgType, ok := respDict["msg_type"].(int)
			if !ok {
				return nil, 0, fmt.Errorf("metadata response missing msg_type")
			}
			if msgType == 2 {
				return nil, 0, fmt.Errorf("peer rejected metadata request for piece %d", piece)
			}

			totalSize, _ := respDict["total_size"].(int)

			// pos now points past the bencoded dict — raw metadata starts here
			rawMetadata := payload[1+pos:]
			return rawMetadata, totalSize, nil
		}
	}
}
