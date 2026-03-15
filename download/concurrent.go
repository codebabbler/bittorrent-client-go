package download

import (
	"crypto/sha1"
	"fmt"
	"os"
	"sync"

	"github.com/codebabbler/bittorrent-client-go/peer"
	"github.com/codebabbler/bittorrent-client-go/tracker"
)

// pieceWork represents a piece to download.
type pieceWork struct {
	Index       int
	Length      int
	ExpectedHash string // raw 20-byte hash
}

// pieceResult holds a downloaded and verified piece.
type pieceResult struct {
	Index int
	Data  []byte
}

// ConcurrentFile downloads all pieces concurrently from multiple peers.
// It creates one worker goroutine per peer, each pulling work from a shared channel.
func ConcurrentFile(
	peers []tracker.Peer,
	infoHash []byte,
	pieces string,
	totalLength int,
	pieceLength int,
	maxWorkers int,
) ([]byte, error) {
	totalPieces := (totalLength + pieceLength - 1) / pieceLength

	// Cap workers to number of peers
	numWorkers := maxWorkers
	if numWorkers > len(peers) {
		numWorkers = len(peers)
	}
	if numWorkers > totalPieces {
		numWorkers = totalPieces
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Create work channel
	workCh := make(chan pieceWork, totalPieces)
	for i := 0; i < totalPieces; i++ {
		pl := pieceLength
		if i == totalPieces-1 {
			pl = totalLength - (i * pieceLength)
		}
		workCh <- pieceWork{
			Index:        i,
			Length:       pl,
			ExpectedHash: pieces[i*20 : (i+1)*20],
		}
	}
	close(workCh)

	// Results channel
	resultCh := make(chan pieceResult, totalPieces)
	errCh := make(chan error, numWorkers)

	var wg sync.WaitGroup

	// Launch worker goroutines
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(peerAddr string) {
			defer wg.Done()

			client, err := peer.NewClient(peerAddr, infoHash, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Worker: failed to connect to %s: %v\n", peerAddr, err)
				return
			}
			defer client.Close()

			err = client.SendInterested()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Worker: interested error on %s: %v\n", peerAddr, err)
				return
			}

			err = client.WaitForUnchoke()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Worker: unchoke error on %s: %v\n", peerAddr, err)
				return
			}

			for work := range workCh {
				data, err := client.DownloadPiece(work.Index, work.Length)
				if err != nil {
					// Put work back for another worker (best effort)
					fmt.Fprintf(os.Stderr, "Worker: piece %d error on %s: %v\n", work.Index, peerAddr, err)
					errCh <- fmt.Errorf("piece %d: %w", work.Index, err)
					return
				}

				// Verify hash
				actualHash := sha1.Sum(data)
				if string(actualHash[:]) != work.ExpectedHash {
					errCh <- fmt.Errorf("hash mismatch for piece %d", work.Index)
					return
				}

				resultCh <- pieceResult{Index: work.Index, Data: data}
				fmt.Fprintf(os.Stderr, "Piece %d downloaded from %s\n", work.Index, peerAddr)
			}
		}(peers[w%len(peers)].Address())
	}

	// Close results when all workers finish
	go func() {
		wg.Wait()
		close(resultCh)
		close(errCh)
	}()

	// Collect results
	pieceMap := make(map[int][]byte, totalPieces)
	for result := range resultCh {
		pieceMap[result.Index] = result.Data
	}

	// Check for errors
	if err, ok := <-errCh; ok {
		return nil, err
	}

	if len(pieceMap) != totalPieces {
		return nil, fmt.Errorf("only downloaded %d/%d pieces", len(pieceMap), totalPieces)
	}

	// Assemble in order
	fileData := make([]byte, 0, totalLength)
	for i := 0; i < totalPieces; i++ {
		fileData = append(fileData, pieceMap[i]...)
	}

	return fileData, nil
}
