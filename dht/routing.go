package dht

import (
	"sort"
	"sync"
)

const (
	// BucketSize is the max number of nodes per k-bucket (BEP 5 uses k=8).
	BucketSize = 8
	// NumBuckets is the number of k-buckets (one per bit of the 160-bit ID space).
	NumBuckets = 160
)

// RoutingTable maintains a Kademlia routing table with 160 k-buckets.
type RoutingTable struct {
	selfID  NodeID
	buckets [NumBuckets][]Node
	mu      sync.RWMutex
}

// NewRoutingTable creates a new routing table for the given node ID.
func NewRoutingTable(selfID NodeID) *RoutingTable {
	return &RoutingTable{selfID: selfID}
}

// bucketIndex returns the bucket index for a given node ID.
// Based on the leading zero bits of XOR distance.
func (rt *RoutingTable) bucketIndex(id NodeID) int {
	dist := XORDistance(rt.selfID, id)
	for i := 0; i < 20; i++ {
		for bit := 7; bit >= 0; bit-- {
			if dist[i]&(1<<uint(bit)) != 0 {
				return i*8 + (7 - bit)
			}
		}
	}
	return NumBuckets - 1
}

// Add inserts a node into the routing table.
// If the bucket is full, the node is discarded (simplified — no eviction).
func (rt *RoutingTable) Add(node Node) {
	if node.ID == rt.selfID {
		return // don't add ourselves
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	idx := rt.bucketIndex(node.ID)
	bucket := rt.buckets[idx]

	// Check if already in bucket
	for i, n := range bucket {
		if n.ID == node.ID {
			// Move to end (most recently seen)
			rt.buckets[idx] = append(append(bucket[:i], bucket[i+1:]...), node)
			return
		}
	}

	// Add if bucket not full
	if len(bucket) < BucketSize {
		rt.buckets[idx] = append(bucket, node)
	}
	// If full, discard (simplified)
}

// FindClosest returns the k closest nodes to the target ID.
func (rt *RoutingTable) FindClosest(target NodeID, count int) []Node {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var all []Node
	for _, bucket := range rt.buckets {
		all = append(all, bucket...)
	}

	sort.Slice(all, func(i, j int) bool {
		return CompareDistance(target, all[i].ID, all[j].ID) < 0
	})

	if len(all) > count {
		all = all[:count]
	}
	return all
}

// Count returns the total number of nodes in the routing table.
func (rt *RoutingTable) Count() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	count := 0
	for _, bucket := range rt.buckets {
		count += len(bucket)
	}
	return count
}
