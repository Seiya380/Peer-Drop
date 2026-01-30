package signaling

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for local network
	},
}

// Hub manages all WebSocket connections and rooms
type Hub struct {
	// IP-based rooms (auto-joined)
	ipRooms   map[string]*Room
	ipRoomsMu sync.RWMutex

	// Public rooms (joined by code)
	publicRooms   map[string]*Room
	publicRoomsMu sync.RWMutex

	// All connected clients
	clients   map[string]*Client
	clientsMu sync.RWMutex

	logger *slog.Logger
}

// NewHub creates a new Hub
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		ipRooms:     make(map[string]*Room),
		publicRooms: make(map[string]*Room),
		clients:     make(map[string]*Client),
		logger:      logger,
	}
}

// HandleWebSocket handles WebSocket upgrade and client connection
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	// Generate unique client ID
	clientID := generateClientID()

	// Get client IP
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}

	client := NewClient(clientID, conn, h, ip, h.logger)

	// Register client
	h.clientsMu.Lock()
	h.clients[clientID] = client
	h.clientsMu.Unlock()

	h.logger.Info("new client connected", "id", clientID, "ip", ip)

	// Start read/write pumps
	go client.WritePump()
	go client.ReadPump()
}

// Unregister removes a client from all rooms and the hub
func (h *Hub) Unregister(client *Client) {
	// Remove from IP room
	if client.ipRoom != nil {
		client.ipRoom.RemoveClient(client.id)

		// Notify other peers in the room
		msg, _ := NewPeerLeftMessage(client.id)
		client.ipRoom.Broadcast(msg, client.id)

		// Clean up empty IP rooms
		if client.ipRoom.IsEmpty() {
			h.ipRoomsMu.Lock()
			delete(h.ipRooms, client.ipRoom.ID())
			h.ipRoomsMu.Unlock()
		}
	}

	// Remove from public room
	if client.publicRoom != nil {
		client.publicRoom.RemoveClient(client.id)

		// Notify other peers in the public room
		msg, _ := NewPeerLeftMessage(client.id)
		client.publicRoom.Broadcast(msg, client.id)

		// Clean up empty public rooms
		if client.publicRoom.IsEmpty() {
			h.publicRoomsMu.Lock()
			delete(h.publicRooms, client.publicRoom.ID())
			h.publicRoomsMu.Unlock()
		}
	}

	// Remove from clients map
	h.clientsMu.Lock()
	delete(h.clients, client.id)
	h.clientsMu.Unlock()

	// Close send channel
	close(client.send)

	h.logger.Info("client disconnected", "id", client.id)
}

// JoinIPRoom adds a client to their IP-based room
func (h *Hub) JoinIPRoom(client *Client) {
	roomID := ExtractIPRoomID(client.ip, "")

	h.ipRoomsMu.Lock()
	room, exists := h.ipRooms[roomID]
	if !exists {
		room = NewRoom(roomID, false)
		h.ipRooms[roomID] = room
	}
	h.ipRoomsMu.Unlock()

	// Add client to room
	room.AddClient(client)
	client.ipRoom = room

	// Get existing peers
	peers := room.GetPeerInfos(client.id)

	// Send peer list to new client
	peersMsg, _ := NewPeersMessage(peers)
	client.Send(peersMsg)

	// Notify existing peers about new client
	joinedMsg, _ := NewPeerJoinedMessage(client.PeerInfo())
	room.Broadcast(joinedMsg, client.id)
}

// CreatePublicRoom creates a new public room and adds the client to it
func (h *Hub) CreatePublicRoom(client *Client) string {
	// Generate unique room code
	var code string
	for {
		code = GenerateRoomCode()
		h.publicRoomsMu.RLock()
		_, exists := h.publicRooms[code]
		h.publicRoomsMu.RUnlock()
		if !exists {
			break
		}
	}

	room := NewRoom(code, true)
	room.AddClient(client)

	h.publicRoomsMu.Lock()
	h.publicRooms[code] = room
	h.publicRoomsMu.Unlock()

	client.publicRoom = room

	return code
}

// JoinPublicRoom adds a client to an existing public room
func (h *Hub) JoinPublicRoom(client *Client, code string) error {
	h.publicRoomsMu.RLock()
	room, exists := h.publicRooms[code]
	h.publicRoomsMu.RUnlock()

	if !exists {
		return errors.New("room not found")
	}

	// Leave current public room if any
	if client.publicRoom != nil {
		h.LeavePublicRoom(client)
	}

	// Add to new room
	room.AddClient(client)
	client.publicRoom = room

	// Get existing peers in the public room
	peers := room.GetPeerInfos(client.id)

	// Send room joined message with peer list
	joinedMsg, _ := NewRoomJoinedMessage(code, peers)
	client.Send(joinedMsg)

	// Notify existing peers about new client
	peerJoinedMsg, _ := NewPeerJoinedMessage(client.PeerInfo())
	room.Broadcast(peerJoinedMsg, client.id)

	return nil
}

// LeavePublicRoom removes a client from their public room
func (h *Hub) LeavePublicRoom(client *Client) {
	if client.publicRoom == nil {
		return
	}

	room := client.publicRoom
	room.RemoveClient(client.id)

	// Notify other peers
	msg, _ := NewPeerLeftMessage(client.id)
	room.Broadcast(msg, client.id)

	// Clean up empty public rooms
	if room.IsEmpty() {
		h.publicRoomsMu.Lock()
		delete(h.publicRooms, room.ID())
		h.publicRoomsMu.Unlock()
	}

	client.publicRoom = nil
}

// StartCleanup starts a goroutine that cleans up empty rooms periodically
func (h *Hub) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			h.cleanupEmptyRooms()
		}
	}()
}

func (h *Hub) cleanupEmptyRooms() {
	// Clean IP rooms
	h.ipRoomsMu.Lock()
	for id, room := range h.ipRooms {
		if room.IsEmpty() {
			delete(h.ipRooms, id)
		}
	}
	h.ipRoomsMu.Unlock()

	// Clean public rooms
	h.publicRoomsMu.Lock()
	for id, room := range h.publicRooms {
		if room.IsEmpty() {
			delete(h.publicRooms, id)
		}
	}
	h.publicRoomsMu.Unlock()
}

// Stats returns hub statistics
func (h *Hub) Stats() map[string]int {
	h.clientsMu.RLock()
	clientCount := len(h.clients)
	h.clientsMu.RUnlock()

	h.ipRoomsMu.RLock()
	ipRoomCount := len(h.ipRooms)
	h.ipRoomsMu.RUnlock()

	h.publicRoomsMu.RLock()
	publicRoomCount := len(h.publicRooms)
	h.publicRoomsMu.RUnlock()

	return map[string]int{
		"clients":      clientCount,
		"ip_rooms":     ipRoomCount,
		"public_rooms": publicRoomCount,
	}
}

func generateClientID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
