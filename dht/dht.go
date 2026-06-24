package dht

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sort"
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
	dhtTimeout     = 2 * time.Second
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

// readResponseForTransaction reads UDP packets until a response matching txnID is found or timeout is reached.
func (d *DHT) readResponseForTransaction(txnID string, timeout time.Duration) (*KRPCResponse, *net.UDPAddr, error) {
	deadline := time.Now().Add(timeout)
	for {
		now := time.Now()
		if now.After(deadline) {
			return nil, nil, fmt.Errorf("timeout waiting for transaction %s", txnID)
		}

		d.conn.SetReadDeadline(deadline)
		buf := make([]byte, 4096)
		n, addr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			return nil, nil, err
		}

		resp, err := ParseKRPCResponse(buf[:n])
		if err != nil {
			// Skip/ignore malformed packets
			continue
		}

		if resp.TransactionID == txnID {
			return resp, addr, nil
		}
	}
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

		resp, _, err := d.readResponseForTransaction(txnID, dhtTimeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DHT: no response from %s: %v\n", addr, err)
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
	target := NodeID(infoHash)
	queried := make(map[NodeID]bool)

	// Keep a list of candidate nodes
	candidates := d.routingTable.FindClosest(target, 20)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no nodes in routing table")
	}

	for iter := 0; iter < maxIterations; iter++ {
		// Pick the closest alpha nodes that have not been queried yet
		var toQuery []Node
		for _, node := range candidates {
			if !queried[node.ID] {
				toQuery = append(toQuery, node)
				if len(toQuery) == alpha {
					break
				}
			}
		}

		if len(toQuery) == 0 {
			break // all candidates queried
		}

		newNodesFound := false
		for _, node := range toQuery {
			queried[node.ID] = true
			fmt.Fprintf(os.Stderr, "DHT: querying node %s (%s)...\n", node.ID, node.Addr)

			txnID := GenerateTransactionID()
			query := BuildGetPeersQuery(txnID, d.nodeID, infoHash)

			_, err := d.conn.WriteToUDP(query, node.Addr)
			if err != nil {
				continue
			}

			resp, _, err := d.readResponseForTransaction(txnID, dhtTimeout)
			if err != nil {
				fmt.Fprintf(os.Stderr, "DHT: query to %s failed: %v\n", node.Addr, err)
				continue
			}

			// Add responding node to routing table
			d.routingTable.Add(Node{ID: resp.ID, Addr: node.Addr})

			// Got peers!
			if len(resp.Values) > 0 {
				peersFound := 0
				for _, v := range resp.Values {
					peers := parsePeerValues(v)
					allPeers = append(allPeers, peers...)
					peersFound += len(peers)
				}
				fmt.Fprintf(os.Stderr, "DHT: node %s returned %d peers\n", node.Addr, peersFound)
			}

			// Got closer nodes — add to candidate list
			if len(resp.Nodes) > 0 {
				for _, n := range resp.Nodes {
					d.routingTable.Add(n)

					// Add to candidates if not already present
					found := false
					for _, c := range candidates {
						if c.ID == n.ID {
							found = true
							break
						}
					}
					if !found {
						candidates = append(candidates, n)
						newNodesFound = true
					}
				}
			}
		}

		// Sort candidates by distance to target and keep the closest 20
		sort.Slice(candidates, func(i, j int) bool {
			return CompareDistance(target, candidates[i].ID, candidates[j].ID) < 0
		})
		if len(candidates) > 20 {
			candidates = candidates[:20]
		}

		// If no new nodes were added and we have queried the closest candidates, stop
		if !newNodesFound && len(toQuery) < alpha {
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
