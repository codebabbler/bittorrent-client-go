package dht

import (
	"crypto/rand"
	"net"
)

// NodeID is a 20-byte identifier for a DHT node.
type NodeID [20]byte

// Node represents a DHT node with its ID and network address.
type Node struct {
	ID   NodeID
	Addr *net.UDPAddr
}

// GenerateNodeID creates a random 20-byte node ID.
func GenerateNodeID() (NodeID, error) {
	var id NodeID
	_, err := rand.Read(id[:])
	return id, err
}

// XORDistance computes the XOR distance between two node IDs.
func XORDistance(a, b NodeID) NodeID {
	var dist NodeID
	for i := 0; i < 20; i++ {
		dist[i] = a[i] ^ b[i]
	}
	return dist
}

// CompareDistance returns -1 if a is closer to target than b, +1 if farther, 0 if equal.
func CompareDistance(target, a, b NodeID) int {
	distA := XORDistance(target, a)
	distB := XORDistance(target, b)
	for i := 0; i < 20; i++ {
		if distA[i] < distB[i] {
			return -1
		}
		if distA[i] > distB[i] {
			return 1
		}
	}
	return 0
}

// CompactNodesEncode encodes a list of nodes into the compact format (26 bytes per node).
func CompactNodesEncode(nodes []Node) []byte {
	data := make([]byte, 0, len(nodes)*26)
	for _, n := range nodes {
		data = append(data, n.ID[:]...)
		data = append(data, n.Addr.IP.To4()...)
		port := make([]byte, 2)
		port[0] = byte(n.Addr.Port >> 8)
		port[1] = byte(n.Addr.Port)
		data = append(data, port...)
	}
	return data
}

// CompactNodesDecode decodes nodes from the compact format (26 bytes per node).
func CompactNodesDecode(data []byte) []Node {
	var nodes []Node
	for i := 0; i+26 <= len(data); i += 26 {
		var id NodeID
		copy(id[:], data[i:i+20])
		ip := net.IP(make([]byte, 4))
		copy(ip, data[i+20:i+24])
		port := int(data[i+24])<<8 | int(data[i+25])
		nodes = append(nodes, Node{
			ID:   id,
			Addr: &net.UDPAddr{IP: ip, Port: port},
		})
	}
	return nodes
}
