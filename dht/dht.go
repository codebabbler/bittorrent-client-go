package dht

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/codebabbler/bittorrent-client-go/tracker"
)

// Default bootstrap nodes for the DHT network.
var DefaultBootstrapNodes = []string{
	"router.bittorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"router.utorrent.com:6881",
}

const (
	dhtTimeout     = 5 * time.Second
	alpha          = 3  // concurrency parameter
	maxIterations  = 20 // max rounds of iterative lookup
)

// DHT represents a DHT node that can discover peers.
type DHT struct {
	conn         *net.UDPConn
	nodeID       NodeID
	routingTable *RoutingTable
	port         int
}

// New creates a new DHT instance bound to the given port.
func New(port int) (*DHT, error) {
	addr := &net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("binding UDP port %d: %w", port, err)
	}

	nodeID, err := GenerateNodeID()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("generating node ID: %w", err)
	}

	return &DHT{
		conn:         conn,
		nodeID:       nodeID,
		routingTable: NewRoutingTable(nodeID),
		port:         port,
	}, nil
}

// Bootstrap contacts the given addresses with find_node queries to populate the routing table.
func (d *DHT) Bootstrap(addresses []string) error {
	for _, addr := range addresses {
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DHT: skipping bootstrap node %s: %v\n", addr, err)
			continue
		}

		txnID := GenerateTransactionID()
		query := BuildFindNodeQuery(txnID, d.nodeID, d.nodeID)

		_, err = d.conn.WriteToUDP(query, udpAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DHT: failed to send to %s: %v\n", addr, err)
			continue
		}

		d.conn.SetReadDeadline(time.Now().Add(dhtTimeout))
		buf := make([]byte, 4096)
		n, _, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DHT: no response from %s: %v\n", addr, err)
			continue
		}

		resp, err := ParseKRPCResponse(buf[:n])
		if err != nil {
			fmt.Fprintf(os.Stderr, "DHT: bad response from %s: %v\n", addr, err)
			continue
		}

		// Add responding node to routing table
		d.routingTable.Add(Node{ID: resp.ID, Addr: udpAddr})

		// Add any returned nodes
		for _, node := range resp.Nodes {
			d.routingTable.Add(node)
		}

		fmt.Fprintf(os.Stderr, "DHT: bootstrapped from %s, got %d nodes\n", addr, len(resp.Nodes))
	}

	if d.routingTable.Count() == 0 {
		return fmt.Errorf("failed to bootstrap: no nodes responded")
	}

	return nil
}

// GetPeers performs an iterative lookup to find peers for the given info hash.
func (d *DHT) GetPeers(infoHash [20]byte) ([]tracker.Peer, error) {
	var allPeers []tracker.Peer
	queried := make(map[NodeID]bool)

	for iter := 0; iter < maxIterations; iter++ {
		// Find closest known nodes to info hash
		target := NodeID(infoHash)
		closest := d.routingTable.FindClosest(target, alpha)

		if len(closest) == 0 {
			break
		}

		newResponses := false
		for _, node := range closest {
			if queried[node.ID] {
				continue
			}
			queried[node.ID] = true

			txnID := GenerateTransactionID()
			query := BuildGetPeersQuery(txnID, d.nodeID, infoHash)

			_, err := d.conn.WriteToUDP(query, node.Addr)
			if err != nil {
				continue
			}

			d.conn.SetReadDeadline(time.Now().Add(dhtTimeout))
			buf := make([]byte, 4096)
			n, _, err := d.conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}

			resp, err := ParseKRPCResponse(buf[:n])
			if err != nil {
				continue
			}

			// Add responding node to routing table
			d.routingTable.Add(Node{ID: resp.ID, Addr: node.Addr})

			// Got peers!
			if len(resp.Values) > 0 {
				for _, v := range resp.Values {
					peers := parsePeerValues(v)
					allPeers = append(allPeers, peers...)
				}
			}

			// Got closer nodes — add to routing table and continue
			if len(resp.Nodes) > 0 {
				for _, n := range resp.Nodes {
					d.routingTable.Add(n)
				}
				newResponses = true
			}
		}

		// If we found peers, return them
		if len(allPeers) > 0 {
			return allPeers, nil
		}

		// If no new nodes were discovered, stop iterating
		if !newResponses {
			break
		}
	}

	if len(allPeers) == 0 {
		return nil, fmt.Errorf("no peers found for info hash")
	}

	return allPeers, nil
}

// parsePeerValues parses compact peer info (6 bytes per peer).
func parsePeerValues(data string) []tracker.Peer {
	var peers []tracker.Peer
	for i := 0; i+6 <= len(data); i += 6 {
		ip := net.IP(make([]byte, 4))
		copy(ip, []byte(data[i:i+4]))
		port := binary.BigEndian.Uint16([]byte(data[i+4 : i+6]))
		peers = append(peers, tracker.Peer{IP: ip, Port: port})
	}
	return peers
}

// Close closes the DHT connection.
func (d *DHT) Close() {
	d.conn.Close()
}
