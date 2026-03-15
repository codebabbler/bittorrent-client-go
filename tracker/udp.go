package tracker

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"time"
)

// UDP tracker protocol constants (BEP 15)
const (
	udpProtocolMagic uint64 = 0x41727101980
	actionConnect    uint32 = 0
	actionAnnounce   uint32 = 1

	udpConnectTimeout = 15 * time.Second
	udpMaxRetries     = 3
)

// GetPeersUDP contacts a UDP tracker and returns a list of peers.
func GetPeersUDP(trackerURL string, infoHash []byte, left int) ([]Peer, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, fmt.Errorf("parsing tracker URL: %w", err)
	}

	host := u.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		// No port specified, use default
		host = net.JoinHostPort(host, "6881")
	}

	serverAddr, err := net.ResolveUDPAddr("udp", host)
	if err != nil {
		return nil, fmt.Errorf("resolving tracker address: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return nil, fmt.Errorf("dialing tracker: %w", err)
	}
	defer conn.Close()

	// Step 1: Connect
	connectionID, err := udpConnect(conn)
	if err != nil {
		return nil, fmt.Errorf("UDP connect: %w", err)
	}

	// Step 2: Announce
	peers, err := udpAnnounce(conn, connectionID, infoHash, left)
	if err != nil {
		return nil, fmt.Errorf("UDP announce: %w", err)
	}

	return peers, nil
}

// udpConnect sends a connect request and returns the connection ID.
func udpConnect(conn *net.UDPConn) (uint64, error) {
	transactionID := rand.Uint32()

	// Build 16-byte connect request
	req := make([]byte, 16)
	binary.BigEndian.PutUint64(req[0:8], udpProtocolMagic)  // connection_id (magic)
	binary.BigEndian.PutUint32(req[8:12], actionConnect)     // action = connect
	binary.BigEndian.PutUint32(req[12:16], transactionID)    // transaction_id

	for retry := 0; retry < udpMaxRetries; retry++ {
		timeout := udpConnectTimeout * time.Duration(1<<uint(retry))
		conn.SetDeadline(time.Now().Add(timeout))

		_, err := conn.Write(req)
		if err != nil {
			return 0, fmt.Errorf("sending connect request: %w", err)
		}

		resp := make([]byte, 16)
		n, err := conn.Read(resp)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // retry
			}
			return 0, fmt.Errorf("reading connect response: %w", err)
		}
		if n < 16 {
			return 0, fmt.Errorf("connect response too short: %d bytes", n)
		}

		respAction := binary.BigEndian.Uint32(resp[0:4])
		respTxnID := binary.BigEndian.Uint32(resp[4:8])
		connectionID := binary.BigEndian.Uint64(resp[8:16])

		if respAction != actionConnect {
			return 0, fmt.Errorf("unexpected action in connect response: %d", respAction)
		}
		if respTxnID != transactionID {
			return 0, fmt.Errorf("transaction ID mismatch: expected %d, got %d", transactionID, respTxnID)
		}

		return connectionID, nil
	}

	return 0, fmt.Errorf("connect failed after %d retries", udpMaxRetries)
}

// udpAnnounce sends an announce request and returns peers.
func udpAnnounce(conn *net.UDPConn, connectionID uint64, infoHash []byte, left int) ([]Peer, error) {
	transactionID := rand.Uint32()

	// Build 98-byte announce request
	req := make([]byte, 98)
	binary.BigEndian.PutUint64(req[0:8], connectionID)       // connection_id
	binary.BigEndian.PutUint32(req[8:12], actionAnnounce)     // action = announce
	binary.BigEndian.PutUint32(req[12:16], transactionID)     // transaction_id
	copy(req[16:36], infoHash)                                // info_hash (20 bytes)
	copy(req[36:56], []byte("-MT1230-rT6yUi8OpLkJ"))          // peer_id (20 bytes)
	binary.BigEndian.PutUint64(req[56:64], 0)                 // downloaded
	binary.BigEndian.PutUint64(req[64:72], uint64(left))      // left
	binary.BigEndian.PutUint64(req[72:80], 0)                 // uploaded
	binary.BigEndian.PutUint32(req[80:84], 0)                 // event (0 = none)
	binary.BigEndian.PutUint32(req[84:88], 0)                 // IP address (0 = default)
	binary.BigEndian.PutUint32(req[88:92], rand.Uint32())     // key (random)
	binary.BigEndian.PutUint32(req[92:96], 0xFFFFFFFF)        // num_want (-1 = default)
	binary.BigEndian.PutUint16(req[96:98], 6881)              // port

	for retry := 0; retry < udpMaxRetries; retry++ {
		timeout := udpConnectTimeout * time.Duration(1<<uint(retry))
		conn.SetDeadline(time.Now().Add(timeout))

		_, err := conn.Write(req)
		if err != nil {
			return nil, fmt.Errorf("sending announce request: %w", err)
		}

		// Response: 20 bytes header + 6 bytes per peer
		resp := make([]byte, 4096)
		n, err := conn.Read(resp)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // retry
			}
			return nil, fmt.Errorf("reading announce response: %w", err)
		}
		if n < 20 {
			return nil, fmt.Errorf("announce response too short: %d bytes", n)
		}

		respAction := binary.BigEndian.Uint32(resp[0:4])
		respTxnID := binary.BigEndian.Uint32(resp[4:8])

		if respAction != actionAnnounce {
			// Check if it's an error response (action=3)
			if respAction == 3 && n > 8 {
				return nil, fmt.Errorf("tracker error: %s", string(resp[8:n]))
			}
			return nil, fmt.Errorf("unexpected action in announce response: %d", respAction)
		}
		if respTxnID != transactionID {
			return nil, fmt.Errorf("transaction ID mismatch: expected %d, got %d", transactionID, respTxnID)
		}

		// Parse peers from response (starts at offset 20, each peer is 6 bytes)
		peerData := resp[20:n]
		var peers []Peer
		for i := 0; i+6 <= len(peerData); i += 6 {
			ip := net.IP(peerData[i : i+4])
			port := binary.BigEndian.Uint16(peerData[i+4 : i+6])
			peers = append(peers, Peer{IP: ip, Port: port})
		}

		return peers, nil
	}

	return nil, fmt.Errorf("announce failed after %d retries", udpMaxRetries)
}
