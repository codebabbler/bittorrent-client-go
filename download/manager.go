package download

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/codebabbler/bittorrent-client-go/peer"
)

type BlockBuffer struct {
	Data []byte
	PBuf *[]byte
}

type PieceWrite struct {
	Index int
	Data  []byte
}

type TorrentManager struct {
	InfoHash     []byte
	Pieces       string
	Length       int
	PieceLength  int
	DestPath     string

	Picker       *PiecePicker
	activePeers  map[string]*peer.Client
	peersMu      sync.RWMutex

	// Channels
	InMessages   chan peer.PeerMessage
	storageCh    chan PieceWrite
	doneCh       chan struct{}
	errCh        chan error

	// Buffers for incomplete pieces: pieceIndex -> blockOffset -> blockData
	pieceBufs    map[int]map[int]BlockBuffer
	pieceBufsMu  sync.Mutex

	OnPeerDisconnect func(peerAddr string)
}

// NewTorrentManager initializes a new TorrentManager.
func NewTorrentManager(infoHash []byte, pieces string, length, pieceLength int, destPath string) *TorrentManager {
	picker := NewPiecePicker(length, pieceLength)
	return &TorrentManager{
		InfoHash:    infoHash,
		Pieces:      pieces,
		Length:      length,
		PieceLength: pieceLength,
		DestPath:    destPath,
		Picker:      picker,
		activePeers: make(map[string]*peer.Client),
		InMessages:  make(chan peer.PeerMessage, 1024),
		storageCh:   make(chan PieceWrite, 64),
		doneCh:      make(chan struct{}),
		errCh:       make(chan error, 1),
		pieceBufs:   make(map[int]map[int]BlockBuffer),
	}
}

// AddPeer registers and starts the message loop for a peer client.
func (tm *TorrentManager) AddPeer(client *peer.Client) {
	tm.peersMu.Lock()
	defer tm.peersMu.Unlock()

	tm.activePeers[client.Address] = client
	tm.Picker.RegisterPeer(client.Address)

	// Start client loops before sending interested message
	client.StartLoop(tm.InMessages)

	// Inform client we are interested in its pieces
	client.QueueMessage(peer.MsgInterested, nil)
}

// RemovePeer closes and unregisters a peer client.
func (tm *TorrentManager) RemovePeer(peerAddr string) {
	tm.peersMu.Lock()
	client, ok := tm.activePeers[peerAddr]
	if ok {
		delete(tm.activePeers, peerAddr)
	}
	tm.peersMu.Unlock()

	if ok {
		client.Close()
		tm.Picker.UnregisterPeer(peerAddr)
	}
}

// Start runs the TorrentManager event loop and block-request pipeline.
func (tm *TorrentManager) Start() error {
	// Create destination directory if it doesn't exist
	dir := filepath.Dir(tm.DestPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Initialize file size
	file, err := os.OpenFile(tm.DestPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	err = file.Truncate(int64(tm.Length))
	file.Close()
	if err != nil {
		return fmt.Errorf("setting file size: %w", err)
	}

	// Start storage loop
	go tm.storageLoop()

	// Ticker for timeout checks and pipelining
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-tm.doneCh:
			close(tm.storageCh)
			return nil
		case err := <-tm.errCh:
			close(tm.storageCh)
			return err
		case <-ticker.C:
			// Reclaim stalled requests
			stalled := tm.Picker.GetStalledRequests(15 * time.Second)
			for _, block := range stalled {
				fmt.Fprintf(os.Stderr, "Block timeout: piece %d offset %d re-queued\n", block.Index, block.Begin)
			}
			tm.pipelineRequests()
		case msg := <-tm.InMessages:
			tm.handleMessage(msg)
			if msg.ID != peer.MsgPiece && msg.PBuf != nil {
				peer.MessageBufferPool.Put(msg.PBuf)
			}
			tm.pipelineRequests()
		}
	}
}

func (tm *TorrentManager) handleMessage(msg peer.PeerMessage) {
	switch msg.ID {
	case 0xFF: // Connection dropped / error
		fmt.Fprintf(os.Stderr, "Peer %s disconnected: %s\n", msg.PeerAddress, string(msg.Payload))
		tm.RemovePeer(msg.PeerAddress)
		if tm.OnPeerDisconnect != nil {
			tm.OnPeerDisconnect(msg.PeerAddress)
		}

	case 0xFE: // Block timeout sentinel from Client ticker
		if len(msg.Payload) >= 8 {
			index := binary.BigEndian.Uint32(msg.Payload[0:4])
			begin := binary.BigEndian.Uint32(msg.Payload[4:8])
			tm.Picker.MarkFailed(int(index), int(begin))
		}

	case peer.MsgBitfield:
		tm.Picker.UpdatePeerBitfield(msg.PeerAddress, msg.Payload)

	case peer.MsgHave:
		if len(msg.Payload) >= 4 {
			index := binary.BigEndian.Uint32(msg.Payload[0:4])
			tm.Picker.UpdatePeerHave(msg.PeerAddress, int(index))
		}

	case peer.MsgPiece:
		if len(msg.Payload) < 8 {
			return
		}
		index := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
		begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
		blockData := msg.Payload[8:]

		tm.pieceBufsMu.Lock()
		if _, ok := tm.pieceBufs[index]; !ok {
			tm.pieceBufs[index] = make(map[int]BlockBuffer)
		}
		tm.pieceBufs[index][begin] = BlockBuffer{Data: blockData, PBuf: msg.PBuf}
		tm.pieceBufsMu.Unlock()

		_, pieceDone, torrentDone := tm.Picker.MarkCompleted(index, begin, msg.PeerAddress)
		if pieceDone {
			tm.verifyAndWritePiece(index)
		}

		if torrentDone {
			close(tm.doneCh)
		}
	}
}

func (tm *TorrentManager) verifyAndWritePiece(index int) {
	tm.pieceBufsMu.Lock()
	blocks, ok := tm.pieceBufs[index]
	tm.pieceBufsMu.Unlock()
	if !ok {
		return
	}

	pieceLen := tm.PieceLength
	totalPieces := (tm.Length + tm.PieceLength - 1) / tm.PieceLength
	if index == totalPieces-1 {
		pieceLen = tm.Length - (index * tm.PieceLength)
	}

	pieceData := make([]byte, pieceLen)
	blockSize := 16384
	numBlocks := (pieceLen + blockSize - 1) / blockSize

	for b := 0; b < numBlocks; b++ {
		begin := b * blockSize
		blockBuf, ok := blocks[begin]
		if !ok {
			// Incomplete piece, mark failed
			tm.revertPiece(index)
			return
		}
		copy(pieceData[begin:], blockBuf.Data)
		if blockBuf.PBuf != nil {
			peer.MessageBufferPool.Put(blockBuf.PBuf)
		}
	}

	// Verify SHA-1 hash
	expectedHash := tm.Pieces[index*20 : (index+1)*20]
	actualHash := sha1.Sum(pieceData)
	if string(actualHash[:]) != expectedHash {
		fmt.Fprintf(os.Stderr, "SHA1 hash mismatch for piece %d! Re-queueing blocks.\n", index)
		tm.revertPiece(index)
		return
	}

	// Send to storage thread
	tm.storageCh <- PieceWrite{Index: index, Data: pieceData}

	tm.pieceBufsMu.Lock()
	delete(tm.pieceBufs, index)
	tm.pieceBufsMu.Unlock()

	fmt.Fprintf(os.Stderr, "Piece %d/%d downloaded and verified.\n", index+1, totalPieces)
}

func (tm *TorrentManager) revertPiece(index int) {
	tm.pieceBufsMu.Lock()
	blocks, ok := tm.pieceBufs[index]
	if ok {
		for _, blockBuf := range blocks {
			if blockBuf.PBuf != nil {
				peer.MessageBufferPool.Put(blockBuf.PBuf)
			}
		}
		delete(tm.pieceBufs, index)
	}
	tm.pieceBufsMu.Unlock()

	pieceLen := tm.PieceLength
	totalPieces := (tm.Length + tm.PieceLength - 1) / tm.PieceLength
	if index == totalPieces-1 {
		pieceLen = tm.Length - (index * tm.PieceLength)
	}
	blockSize := 16384
	numBlocks := (pieceLen + blockSize - 1) / blockSize

	for b := 0; b < numBlocks; b++ {
		begin := b * blockSize
		tm.Picker.MarkFailed(index, begin)
	}
}

func (tm *TorrentManager) pipelineRequests() {
	tm.peersMu.RLock()
	defer tm.peersMu.RUnlock()

	for _, client := range tm.activePeers {
		if client.IsChoked() {
			continue
		}

		// Keep up to 5 concurrent block requests in flight per client
		for client.GetOutstandingCount() < 5 {
			block, ok := tm.Picker.NextBlock(client.Address)
			if !ok {
				break
			}
			client.QueueRequest(block.Index, block.Begin, block.Length)
		}
	}
}

func (tm *TorrentManager) storageLoop() {
	file, err := os.OpenFile(tm.DestPath, os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Storage loop error: %v\n", err)
		return
	}
	defer file.Close()

	for pw := range tm.storageCh {
		offset := int64(pw.Index) * int64(tm.PieceLength)
		_, err := file.WriteAt(pw.Data, offset)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Storage loop: error writing piece %d: %v\n", pw.Index, err)
		}
	}
}

// ActivePeersCount returns the number of currently connected peer clients.
func (tm *TorrentManager) ActivePeersCount() int {
	tm.peersMu.RLock()
	defer tm.peersMu.RUnlock()
	return len(tm.activePeers)
}
