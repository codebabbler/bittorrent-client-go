package download

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/codebabbler/bittorrent-client-go/peer"
	"github.com/codebabbler/bittorrent-client-go/tracker"
)

type Session struct {
	maxConnections int
	maxWorkers     int
}

// NewSession creates a new global session manager.
func NewSession(maxConnections, maxWorkers int) *Session {
	return &Session{
		maxConnections: maxConnections,
		maxWorkers:     maxWorkers,
	}
}

// Download starts the asynchronous torrent download manager and orchestrates peer discovery and dialing.
func (s *Session) Download(
	infoHash []byte,
	pieces string,
	length int,
	pieceLength int,
	destPath string,
	discoverPeers func() ([]tracker.Peer, error),
) error {
	tm := NewTorrentManager(infoHash, pieces, length, pieceLength, destPath)
	pool := peer.NewPeerEndpointPool()

	tm.OnPeerDisconnect = func(peerAddr string) {
		pool.MarkFailed(peerAddr)
	}

	// Start TorrentManager loop
	tmErrCh := make(chan error, 1)
	go func() {
		if err := tm.Start(); err != nil {
			tmErrCh <- err
		}
	}()

	// 1. Initial peer discovery
	initialPeers, err := discoverPeers()
	if err == nil {
		for _, p := range initialPeers {
			pool.Add(p.Address())
		}
	} else {
		fmt.Fprintf(os.Stderr, "Initial peer discovery warning: %v\n", err)
	}

	// Tickers
	discoveryTicker := time.NewTicker(30 * time.Second)
	defer discoveryTicker.Stop()
	dialTicker := time.NewTicker(1 * time.Second)
	defer dialTicker.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dialSem := make(chan struct{}, s.maxWorkers)

	var dialMu sync.Mutex
	dialing := make(map[string]bool)

	// Dialing goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-dialTicker.C:
				if tm.Picker.IsCompleted() {
					return
				}

				activeCount := tm.ActivePeersCount()
				if activeCount >= s.maxConnections {
					continue
				}

				candidates := pool.GetCandidates(false)
				if len(candidates) == 0 {
					continue
				}

				toDial := s.maxConnections - activeCount
				if toDial > len(candidates) {
					toDial = len(candidates)
				}

				for i := 0; i < toDial; i++ {
					addr := candidates[i]

					dialMu.Lock()
					if dialing[addr] {
						dialMu.Unlock()
						continue
					}
					dialing[addr] = true
					dialMu.Unlock()

					select {
					case dialSem <- struct{}{}:
						go func(targetAddr string) {
							defer func() {
								dialMu.Lock()
								delete(dialing, targetAddr)
								dialMu.Unlock()
								<-dialSem
							}()

							client, err := peer.NewClient(targetAddr, infoHash, false)
							if err != nil {
								pool.MarkFailed(targetAddr)
								return
							}

							pool.MarkSucceeded(targetAddr)
							tm.AddPeer(client)
						}(addr)
					default:
						dialMu.Lock()
						delete(dialing, addr)
						dialMu.Unlock()
						// Concurrency limit reached for active dials
					}
				}
			}
		}
	}()

	// Discovery loop goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-discoveryTicker.C:
				if tm.Picker.IsCompleted() {
					return
				}
				newPeers, err := discoverPeers()
				if err == nil {
					for _, p := range newPeers {
						pool.Add(p.Address())
					}
				}
				pool.EvictDead()
			}
		}
	}()

	// Wait for download completion or manager error
	select {
	case <-tm.doneCh:
		return nil
	case err := <-tmErrCh:
		return err
	}
}
