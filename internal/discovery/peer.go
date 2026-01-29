package discovery

import (
	"runtime"
	"sync"
	"time"
)

type Peer struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Address  string    `json:"address"`
	Port     int       `json:"port"`
	Platform string    `json:"platform"`
	LastSeen time.Time `json:"last_seen"`
}

type PeerRegistry struct {
	mu    sync.RWMutex
	peers map[string]*Peer
	self  *Peer
}

func NewPeerRegistry(selfID, selfName string, port int) *PeerRegistry {
	return &PeerRegistry{
		peers: make(map[string]*Peer),
		self: &Peer{
			ID:       selfID,
			Name:     selfName,
			Port:     port,
			Platform: runtime.GOOS,
		},
	}
}

func (r *PeerRegistry) Self() *Peer {
	return r.self
}

func (r *PeerRegistry) Update(peer *Peer) {
	if peer.ID == r.self.ID {
		return // Don't add ourselves
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	peer.LastSeen = time.Now()
	r.peers[peer.ID] = peer
}

func (r *PeerRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, id)
}

func (r *PeerRegistry) Get(id string) *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.peers[id]
}

func (r *PeerRegistry) GetActive(ttl time.Duration) []*Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cutoff := time.Now().Add(-ttl)
	var active []*Peer

	for _, p := range r.peers {
		if p.LastSeen.After(cutoff) {
			active = append(active, p)
		}
	}

	return active
}

func (r *PeerRegistry) CleanExpired(ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-ttl)

	for id, p := range r.peers {
		if p.LastSeen.Before(cutoff) {
			delete(r.peers, id)
		}
	}
}
