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
	Index        int
	Length       int
	ExpectedHash string // raw 20-byte hash
	Retries      int
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

	// Create work channel (buffered to accommodate retries)
	workCh := make(chan pieceWork, totalPieces*4)
	for i := 0; i < totalPieces; i++ {
		pl := pieceLength
		if i == totalPieces-1 {
			pl = totalLength - (i * pieceLength)
		}
		workCh <- pieceWork{
			Index:        i,
			Length:       pl,
			ExpectedHash: pieces[i*20 : (i+1)*20],
			Retries:      0,
		}
	}

	// Results channels
	resultCh := make(chan pieceResult, totalPieces)
	errCh := make(chan error, numWorkers)
	done := make(chan struct{})
	defer close(done)

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

			for {
				select {
				case <-done:
					return
				case work := <-workCh:
					data, err := client.DownloadPiece(work.Index, work.Length)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Worker: piece %d error on %s: %v\n", work.Index, peerAddr, err)
						if work.Retries < 3 {
							select {
							case workCh <- pieceWork{
								Index:        work.Index,
								Length:       work.Length,
								ExpectedHash: work.ExpectedHash,
								Retries:      work.Retries + 1,
							}:
							case <-done:
							}
						} else {
							select {
							case errCh <- fmt.Errorf("piece %d failed after 3 retries: %w", work.Index, err):
							case <-done:
							}
							return
						}
						continue
					}

					// Verify hash
					actualHash := sha1.Sum(data)
					if string(actualHash[:]) != work.ExpectedHash {
						fmt.Fprintf(os.Stderr, "Worker: hash mismatch for piece %d on %s\n", work.Index, peerAddr)
						if work.Retries < 3 {
							select {
							case workCh <- pieceWork{
								Index:        work.Index,
								Length:       work.Length,
								ExpectedHash: work.ExpectedHash,
								Retries:      work.Retries + 1,
							}:
							case <-done:
							}
						} else {
							select {
							case errCh <- fmt.Errorf("hash mismatch for piece %d after 3 retries", work.Index):
							case <-done:
							}
							return
						}
						continue
					}

					select {
					case resultCh <- pieceResult{Index: work.Index, Data: data}:
					case <-done:
					}
					fmt.Fprintf(os.Stderr, "Piece %d downloaded from %s\n", work.Index, peerAddr)
				}
			}
		}(peers[w%len(peers)].Address())
	}

	// Monitor when all workers finish
	allWorkersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(allWorkersDone)
	}()

	// Collect results
	pieceMap := make(map[int][]byte, totalPieces)
	var downloadErr error

	for len(pieceMap) < totalPieces {
		select {
		case result := <-resultCh:
			pieceMap[result.Index] = result.Data
		case err := <-errCh:
			downloadErr = err
			goto finished
		case <-allWorkersDone:
			if len(pieceMap) < totalPieces {
				downloadErr = fmt.Errorf("all workers exited before completing download (got %d/%d pieces)", len(pieceMap), totalPieces)
			}
			goto finished
		}
	}

finished:
	if downloadErr != nil {
		return nil, downloadErr
	}

	// Assemble in order
	fileData := make([]byte, 0, totalLength)
	for i := 0; i < totalPieces; i++ {
		fileData = append(fileData, pieceMap[i]...)
	}

	return fileData, nil
}
