package signaling

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"sync"
)

// Room represents a group of peers that can see each other
type Room struct {
	id       string
	isPublic bool
	clients  map[string]*Client
	mu       sync.RWMutex
}

// NewRoom creates a new room
func NewRoom(id string, isPublic bool) *Room {
	return &Room{
		id:       id,
		isPublic: isPublic,
		clients:  make(map[string]*Client),
	}
}

// ID returns the room identifier
func (r *Room) ID() string {
	return r.id
}

// IsPublic returns whether this is a public room
func (r *Room) IsPublic() bool {
	return r.isPublic
}

// AddClient adds a client to the room
func (r *Room) AddClient(client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[client.id] = client
}

// RemoveClient removes a client from the room
func (r *Room) RemoveClient(clientID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, clientID)
}

// GetClient returns a client by ID
func (r *Room) GetClient(clientID string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[clientID]
}

// GetClients returns all clients in the room
func (r *Room) GetClients() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	clients := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	return clients
}

// GetPeerInfos returns peer info for all clients except the excluded one
func (r *Room) GetPeerInfos(excludeID string) []PeerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	peers := make([]PeerInfo, 0, len(r.clients))
	for _, c := range r.clients {
		if c.id != excludeID {
			peers = append(peers, c.PeerInfo())
		}
	}
	return peers
}

// Broadcast sends a message to all clients in the room except the excluded one
func (r *Room) Broadcast(msg []byte, excludeID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, client := range r.clients {
		if client.id != excludeID {
			client.Send(msg)
		}
	}
}

// RelayTo sends a message to a specific client in the room
func (r *Room) RelayTo(targetID string, msg []byte) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if client, ok := r.clients[targetID]; ok {
		client.Send(msg)
		return true
	}
	return false
}

// IsEmpty returns true if the room has no clients
func (r *Room) IsEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients) == 0
}

// ClientCount returns the number of clients in the room
func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// ExtractIPRoomID determines the room ID based on client IP
// Devices on the same /24 subnet (IPv4) or /64 subnet (IPv6) are grouped together
func ExtractIPRoomID(remoteAddr string, xForwardedFor string) string {
	ip := remoteAddr

	// Prefer X-Forwarded-For if behind proxy
	if xForwardedFor != "" {
		parts := strings.Split(xForwardedFor, ",")
		ip = strings.TrimSpace(parts[0])
	}

	// Extract host part (remove port)
	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "default"
	}

	// IPv4: Use /24 subnet (first 3 octets)
	if v4 := parsed.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d", v4[0], v4[1], v4[2])
	}

	// IPv6: Use /64 subnet (first 4 segments)
	// IPv6 addresses are 16 bytes, we take first 8 bytes
	return fmt.Sprintf("%02x%02x:%02x%02x:%02x%02x:%02x%02x",
		parsed[0], parsed[1], parsed[2], parsed[3],
		parsed[4], parsed[5], parsed[6], parsed[7])
}

// GenerateRoomCode creates a random 5-character room code
func GenerateRoomCode() string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // Removed ambiguous chars (0, O, I, 1)
	b := make([]byte, 5)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}
