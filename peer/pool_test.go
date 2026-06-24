package peer

import (
	"testing"
)

func TestPeerEndpointPool(t *testing.T) {
	pool := NewPeerEndpointPool()

	addr1 := "127.0.0.1:6881"
	addr2 := "127.0.0.1:6882"
	addr3 := "127.0.0.1:6883"

	pool.Add(addr1)
	pool.Add(addr2)
	pool.Add(addr3)

	// All should be candidates initially
	candidates := pool.GetCandidates(false)
	if len(candidates) != 3 {
		t.Fatalf("Expected 3 candidates, got %d", len(candidates))
	}

	// Mark addr1 failed
	pool.MarkFailed(addr1)

	// addr1 should be in backoff, leaving 2 candidates
	candidates = pool.GetCandidates(false)
	if len(candidates) != 2 {
		t.Fatalf("Expected 2 candidates after 1 failure, got %d", len(candidates))
	}

	// Mark addr2 as seed
	pool.MarkSeed(addr2, true)

	// Getting candidates excluding seeds should only return addr3
	candidates = pool.GetCandidates(true)
	if len(candidates) != 1 || candidates[0] != addr3 {
		t.Fatalf("Expected only addr3 candidate, got %v", candidates)
	}

	// Fail addr1 four more times (5 total fails) -> should be evicted
	for i := 0; i < 4; i++ {
		pool.MarkFailed(addr1)
	}
	pool.EvictDead()

	if pool.Size() != 2 {
		t.Fatalf("Expected pool size 2 after evicting dead peer, got %d", pool.Size())
	}
}
