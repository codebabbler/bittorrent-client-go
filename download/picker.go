package download

import (
	"sort"
	"sync"
	"time"
)

type BlockState int

const (
	BlockUnrequested BlockState = iota
	BlockPending
	BlockCompleted
)

type Block struct {
	Index  int       // Piece index
	Begin  int       // Block offset within piece
	Length int       // Block length
	State  BlockState
	Peer   string    // Peer address currently holding the request
	Sent   time.Time // Request timestamp
}

type pieceState struct {
	Index     int
	Length    int
	Blocks    []Block
	Completed bool
}

type PiecePicker struct {
	mu           sync.Mutex
	pieces       []pieceState
	peerPieces   map[string][]bool
	availability []int
	totalBlocks  int
	pendingCount int
	completed    int
	endGame      bool
}

// NewPiecePicker initializes the picker with piece dimensions.
func NewPiecePicker(totalLength, normalPieceLength int) *PiecePicker {
	totalPieces := (totalLength + normalPieceLength - 1) / normalPieceLength
	pieces := make([]pieceState, totalPieces)
	totalBlocks := 0

	for i := 0; i < totalPieces; i++ {
		pieceLen := normalPieceLength
		if i == totalPieces-1 {
			pieceLen = totalLength - (i * normalPieceLength)
		}

		blockSize := 16384
		numBlocks := (pieceLen + blockSize - 1) / blockSize
		blocks := make([]Block, numBlocks)

		for b := 0; b < numBlocks; b++ {
			begin := b * blockSize
			length := blockSize
			if begin+length > pieceLen {
				length = pieceLen - begin
			}
			blocks[b] = Block{
				Index:  i,
				Begin:  begin,
				Length: length,
				State:  BlockUnrequested,
			}
			totalBlocks++
		}

		pieces[i] = pieceState{
			Index:  i,
			Length: pieceLen,
			Blocks: blocks,
		}
	}

	return &PiecePicker{
		pieces:       pieces,
		peerPieces:   make(map[string][]bool),
		availability: make([]int, totalPieces),
		totalBlocks:  totalBlocks,
	}
}

// RegisterPeer registers a peer's availability array.
func (p *PiecePicker) RegisterPeer(peerAddr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.peerPieces[peerAddr] = make([]bool, len(p.pieces))
}

// UnregisterPeer removes a peer and updates availability.
func (p *PiecePicker) UnregisterPeer(peerAddr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if bitmap, ok := p.peerPieces[peerAddr]; ok {
		for i, has := range bitmap {
			if has {
				p.availability[i]--
			}
		}
		delete(p.peerPieces, peerAddr)
	}

	// Revert pending blocks requested by this peer
	for i := range p.pieces {
		if p.pieces[i].Completed {
			continue
		}
		for b := range p.pieces[i].Blocks {
			block := &p.pieces[i].Blocks[b]
			if block.State == BlockPending && block.Peer == peerAddr {
				block.State = BlockUnrequested
				block.Peer = ""
				p.pendingCount--
			}
		}
	}
}

// UpdatePeerBitfield updates peer pieces from bitfield bytes.
func (p *PiecePicker) UpdatePeerBitfield(peerAddr string, bitfield []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	bitmap, ok := p.peerPieces[peerAddr]
	if !ok {
		return
	}

	for i := range bitmap {
		byteIndex := i / 8
		bitIndex := 7 - (i % 8)
		if byteIndex < len(bitfield) {
			has := (bitfield[byteIndex] & (1 << bitIndex)) != 0
			if has && !bitmap[i] {
				bitmap[i] = true
				p.availability[i]++
			}
		}
	}
}

// UpdatePeerHave updates peer piece availability for a single index.
func (p *PiecePicker) UpdatePeerHave(peerAddr string, pieceIndex int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	bitmap, ok := p.peerPieces[peerAddr]
	if !ok {
		return
	}

	if pieceIndex >= 0 && pieceIndex < len(bitmap) {
		if !bitmap[pieceIndex] {
			bitmap[pieceIndex] = true
			p.availability[pieceIndex]++
		}
	}
}

// NextBlock selects the next block to request from a peer.
// Implements rarest-first picking and End-Game Mode.
func (p *PiecePicker) NextBlock(peerAddr string) (Block, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	bitmap, ok := p.peerPieces[peerAddr]
	if !ok {
		return Block{}, false
	}

	// 1. Gather all incomplete pieces the peer has.
	type candidatePiece struct {
		index int
		avail int
	}
	var candidates []candidatePiece

	hasUnrequested := false
	for i, piece := range p.pieces {
		if piece.Completed {
			continue
		}
		if bitmap[i] {
			candidates = append(candidates, candidatePiece{index: i, avail: p.availability[i]})
			for _, b := range piece.Blocks {
				if b.State == BlockUnrequested {
					hasUnrequested = true
				}
			}
		}
	}

	if len(candidates) == 0 {
		return Block{}, false
	}

	// Sort candidates by availability (rarest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].avail < candidates[j].avail
	})

	// Check if we should trigger End-Game Mode.
	// End-game is active if there are no more unrequested blocks remaining in the entire torrent.
	if !hasUnrequested && p.completed < p.totalBlocks {
		p.endGame = true
	}

	// 2. Pick next block
	if !p.endGame {
		// Normal Mode: Pick first Unrequested block from the rarest piece.
		for _, cand := range candidates {
			piece := &p.pieces[cand.index]
			for b := range piece.Blocks {
				block := &piece.Blocks[b]
				if block.State == BlockUnrequested {
					block.State = BlockPending
					block.Peer = peerAddr
					block.Sent = time.Now()
					p.pendingCount++
					return *block, true
				}
			}
		}
	} else {
		// End-Game Mode: Pick any Pending block that has not been completed.
		for _, cand := range candidates {
			piece := &p.pieces[cand.index]
			for b := range piece.Blocks {
				block := &piece.Blocks[b]
				if block.State == BlockPending {
					// We duplicate request this block
					block.Sent = time.Now()
					return *block, true
				}
			}
		}
	}

	return Block{}, false
}

// MarkCompleted marks a block as successfully downloaded.
// Returns completed piece index and a boolean indicating if the entire piece is complete.
func (p *PiecePicker) MarkCompleted(pieceIndex, begin int, peerAddr string) (int, bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pieceIndex < 0 || pieceIndex >= len(p.pieces) {
		return -1, false, false
	}

	piece := &p.pieces[pieceIndex]
	if piece.Completed {
		return -1, false, false
	}

	var foundBlock *Block
	for b := range piece.Blocks {
		if piece.Blocks[b].Begin == begin {
			foundBlock = &piece.Blocks[b]
			break
		}
	}

	if foundBlock == nil {
		return -1, false, false
	}

	if foundBlock.State == BlockCompleted {
		return -1, false, false
	}

	if foundBlock.State == BlockPending {
		p.pendingCount--
	}

	foundBlock.State = BlockCompleted
	foundBlock.Peer = peerAddr
	p.completed++

	// Check if entire piece is completed
	pieceDone := true
	for _, b := range piece.Blocks {
		if b.State != BlockCompleted {
			pieceDone = false
			break
		}
	}

	if pieceDone {
		piece.Completed = true
		return pieceIndex, true, p.completed == p.totalBlocks
	}

	return pieceIndex, false, p.completed == p.totalBlocks
}

// MarkFailed puts a block back into the unrequested queue.
func (p *PiecePicker) MarkFailed(pieceIndex, begin int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pieceIndex < 0 || pieceIndex >= len(p.pieces) {
		return
	}

	piece := &p.pieces[pieceIndex]
	for b := range piece.Blocks {
		block := &piece.Blocks[b]
		if block.Begin == begin {
			if block.State == BlockPending {
				block.State = BlockUnrequested
				block.Peer = ""
				p.pendingCount--
			}
			break
		}
	}
}

// GetStalledRequests reclaims block requests that have timed out.
// Returns list of blocks to cancel and retry.
func (p *PiecePicker) GetStalledRequests(timeout time.Duration) []Block {
	p.mu.Lock()
	defer p.mu.Unlock()

	var stalled []Block
	now := time.Now()

	for i := range p.pieces {
		if p.pieces[i].Completed {
			continue
		}
		for b := range p.pieces[i].Blocks {
			block := &p.pieces[i].Blocks[b]
			if block.State == BlockPending && now.Sub(block.Sent) > timeout {
				stalled = append(stalled, *block)
				// Revert to unrequested so another worker can request it
				block.State = BlockUnrequested
				block.Peer = ""
				p.pendingCount--
			}
		}
	}
	return stalled
}

// IsCompleted returns if all pieces are downloaded and verified.
func (p *PiecePicker) IsCompleted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed == p.totalBlocks
}
