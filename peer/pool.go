package peer

import (
	"sync"
	"time"
)

// PeerEndpoint tracks connection stats for a specific peer address.
type PeerEndpoint struct {
	Address       string
	FailCount     int
	NextRetryTime time.Time
	IsSeed        bool
}

// PeerEndpointPool maintains a thread-safe set of known peer endpoints.
type PeerEndpointPool struct {
	mu        sync.RWMutex
	endpoints map[string]*PeerEndpoint
}

// NewPeerEndpointPool creates a new pool.
func NewPeerEndpointPool() *PeerEndpointPool {
	return &PeerEndpointPool{
		endpoints: make(map[string]*PeerEndpoint),
	}
}

// Add adds a new endpoint to the pool if it does not already exist.
func (p *PeerEndpointPool) Add(address string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.endpoints[address]; !ok {
		p.endpoints[address] = &PeerEndpoint{
			Address:       address,
			NextRetryTime: time.Time{}, // Eligible immediately
		}
	}
}

// MarkFailed records a failed connection attempt and updates the backoff.
func (p *PeerEndpointPool) MarkFailed(address string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ep, ok := p.endpoints[address]
	if !ok {
		ep = &PeerEndpoint{Address: address}
		p.endpoints[address] = ep
	}

	ep.FailCount++
	// Backoff: 15s * 2^(failCount-1), capped at 10 minutes (600s)
	backoffSec := 15 * (1 << (ep.FailCount - 1))
	if backoffSec > 600 || backoffSec <= 0 { // overflow check
		backoffSec = 600
	}
	ep.NextRetryTime = time.Now().Add(time.Duration(backoffSec) * time.Second)
}

// MarkSucceeded resets the failure counter.
func (p *PeerEndpointPool) MarkSucceeded(address string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if ep, ok := p.endpoints[address]; ok {
		ep.FailCount = 0
		ep.NextRetryTime = time.Time{}
	}
}

// MarkSeed sets the seed flag for the endpoint.
func (p *PeerEndpointPool) MarkSeed(address string, isSeed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if ep, ok := p.endpoints[address]; ok {
		ep.IsSeed = isSeed
	}
}

// GetCandidates returns a list of peer addresses that are eligible for dialing.
// An endpoint is eligible if the current time is past its NextRetryTime and ep.FailCount < 5.
func (p *PeerEndpointPool) GetCandidates(excludeSeeds bool) []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var candidates []string
	now := time.Now()
	for _, ep := range p.endpoints {
		if ep.FailCount >= 5 {
			continue
		}
		if excludeSeeds && ep.IsSeed {
			continue
		}
		if now.Before(ep.NextRetryTime) {
			continue
		}
		candidates = append(candidates, ep.Address)
	}
	return candidates
}

// EvictDead removes endpoints that have failed too many times.
func (p *PeerEndpointPool) EvictDead() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, ep := range p.endpoints {
		if ep.FailCount >= 5 {
			delete(p.endpoints, addr)
		}
	}
}

// Size returns the total number of endpoints in the pool.
func (p *PeerEndpointPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.endpoints)
}
