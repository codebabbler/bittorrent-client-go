package peer

import (
	"fmt"
	"net"

	"github.com/codebabbler/bittorrent-client-go/bencode"
)

// DoExtensionHandshake sends our extension handshake and receives the peer's.
// Returns the peer's ut_metadata extension ID.
func DoExtensionHandshake(conn net.Conn) (int, error) {
	// Send extension handshake: {"m": {"ut_metadata": 1}}
	extPayload := []byte("d1:md11:ut_metadatai1eee")
	err := WriteMessage(conn, MsgExtension, append([]byte{0}, extPayload...))
	if err != nil {
		return 0, fmt.Errorf("sending extension handshake: %w", err)
	}

	// Receive extension handshake (skip non-extension messages)
	for {
		id, payload, err := ReadMessage(conn)
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

// RequestMetadata sends a metadata request and receives the raw metadata bytes.
func RequestMetadata(conn net.Conn, peerExtId int) ([]byte, error) {
	// Send metadata request: {"msg_type": 0, "piece": 0}
	reqPayload := []byte("d8:msg_typei0e5:piecei0ee")
	msg := append([]byte{byte(peerExtId)}, reqPayload...)
	err := WriteMessage(conn, MsgExtension, msg)
	if err != nil {
		return nil, fmt.Errorf("sending metadata request: %w", err)
	}

	// Receive metadata data response (skip non-extension messages)
	for {
		id, payload, err := ReadMessage(conn)
		if err != nil {
			return nil, fmt.Errorf("reading metadata response: %w", err)
		}
		if payload == nil {
			continue // keepalive
		}
		if id == MsgExtension {
			// payload[0] = our ut_metadata ID, payload[1:] = bencoded dict + raw metadata
			dictStr := string(payload[1:])
			pos := 0
			_, _, err = bencode.DecodeDict(dictStr, &pos)
			if err != nil {
				return nil, fmt.Errorf("decoding metadata response: %w", err)
			}
			// pos now points past the bencoded dict — raw metadata starts here
			rawMetadata := payload[1+pos:]
			return rawMetadata, nil
		}
	}
}
