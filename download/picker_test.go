package download

import (
	"testing"
	"time"
)

func TestPiecePickerRarestFirst(t *testing.T) {
	// 3 pieces, 16384 bytes each
	picker := NewPiecePicker(16384*3, 16384)

	picker.RegisterPeer("peer1")
	picker.RegisterPeer("peer2")

	// Set availability:
	// piece 0: has by peer1 and peer2 (avail = 2)
	// piece 1: has only by peer2 (avail = 1)
	// piece 2: has by nobody (avail = 0)
	picker.UpdatePeerHave("peer1", 0)
	picker.UpdatePeerHave("peer2", 0)
	picker.UpdatePeerHave("peer2", 1)

	// Since peer1 only has piece 0, picker must return block for piece 0
	b1, ok := picker.NextBlock("peer1")
	if !ok || b1.Index != 0 {
		t.Fatalf("Expected block from piece 0, got index %d (ok: %t)", b1.Index, ok)
	}

	// peer2 has piece 0 and 1. Piece 1 is rarer (avail=1 vs avail=2).
	// NextBlock for peer2 should return block for piece 1.
	b2, ok := picker.NextBlock("peer2")
	if !ok || b2.Index != 1 {
		t.Fatalf("Expected block from piece 1 (rarest), got index %d (ok: %t)", b2.Index, ok)
	}
}

func TestEndGameMode(t *testing.T) {
	// 2 pieces, 1 block each
	picker := NewPiecePicker(16384*2, 16384)
	picker.RegisterPeer("peer1")
	picker.UpdatePeerHave("peer1", 0)
	picker.UpdatePeerHave("peer1", 1)

	// Request block 0
	b0, ok := picker.NextBlock("peer1")
	if !ok || b0.Index != 0 {
		t.Fatalf("Expected piece 0")
	}

	// Request block 1
	b1, ok := picker.NextBlock("peer1")
	if !ok || b1.Index != 1 {
		t.Fatalf("Expected piece 1")
	}

	// Both blocks are pending. There are no unrequested blocks left.
	// End-Game Mode should trigger.
	// NextBlock should return a pending block (duplicate request).
	bDup, ok := picker.NextBlock("peer1")
	if !ok {
		t.Fatalf("Expected duplicate block in End-Game Mode")
	}
	if bDup.State != BlockPending {
		t.Fatalf("Expected block state to be pending, got %v", bDup.State)
	}
}

func TestStalledRequests(t *testing.T) {
	// 1 piece, 1 block
	picker := NewPiecePicker(16384, 16384)
	picker.RegisterPeer("peer1")
	picker.UpdatePeerHave("peer1", 0)

	_, ok := picker.NextBlock("peer1")
	if !ok {
		t.Fatalf("Expected block")
	}

	// Fast forward time by simulating a past request
	picker.pieces[0].Blocks[0].Sent = time.Now().Add(-20 * time.Second)

	stalled := picker.GetStalledRequests(15 * time.Second)
	if len(stalled) != 1 || stalled[0].Index != 0 {
		t.Fatalf("Expected 1 stalled block for piece 0, got %d", len(stalled))
	}

	// Should be unrequested again
	if picker.pieces[0].Blocks[0].State != BlockUnrequested {
		t.Fatalf("Expected block to be reset to Unrequested")
	}
}
