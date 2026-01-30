package signaling

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 10 * 1024 * 1024 // 10MB for relay chunks
)

// Client represents a WebSocket connection
type Client struct {
	id         string
	conn       *websocket.Conn
	hub        *Hub
	send       chan []byte
	ipRoom     *Room
	publicRoom *Room

	// Peer info
	name     string
	platform string
	ip       string

	logger *slog.Logger
}

// NewClient creates a new client
func NewClient(id string, conn *websocket.Conn, hub *Hub, ip string, logger *slog.Logger) *Client {
	return &Client{
		id:     id,
		conn:   conn,
		hub:    hub,
		send:   make(chan []byte, 256),
		ip:     ip,
		logger: logger,
	}
}

// ID returns the client ID
func (c *Client) ID() string {
	return c.id
}

// PeerInfo returns the peer information for this client
func (c *Client) PeerInfo() PeerInfo {
	return PeerInfo{
		ID:       c.id,
		Name:     c.name,
		Platform: c.platform,
	}
}

// Send queues a message to be sent to the client
func (c *Client) Send(msg []byte) {
	select {
	case c.send <- msg:
	default:
		// Channel full, client is slow
		c.logger.Warn("client send buffer full", "clientID", c.id)
	}
}

// ReadPump pumps messages from the WebSocket connection to the hub
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Warn("websocket read error", "error", err, "clientID", c.id)
			}
			break
		}
		c.handleMessage(message)
	}
}

// WritePump pumps messages from the hub to the WebSocket connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes an incoming message
func (c *Client) handleMessage(data []byte) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		c.logger.Warn("failed to unmarshal message", "error", err)
		return
	}

	switch msg.Type {
	case TypeJoin:
		c.handleJoin(msg.Payload)
	case TypeOffer, TypeAnswer, TypeIceCandidate:
		c.relayToTarget(msg, data)
	case TypeTransferRequest, TypeTransferResponse:
		c.relayToTarget(msg, data)
	case TypeRelayChunk:
		c.relayToTarget(msg, data)
	case TypeCreateRoom:
		c.handleCreateRoom()
	case TypeJoinRoom:
		c.handleJoinRoom(msg.Payload)
	case TypeLeaveRoom:
		c.handleLeaveRoom()
	case TypePing:
		c.Send(NewPongMessage())
	}
}

// handleJoin processes a join message
func (c *Client) handleJoin(payload json.RawMessage) {
	var joinPayload JoinPayload
	if err := json.Unmarshal(payload, &joinPayload); err != nil {
		c.logger.Warn("failed to unmarshal join payload", "error", err)
		return
	}

	c.name = joinPayload.Name
	c.platform = joinPayload.Platform

	// Join IP-based room
	c.hub.JoinIPRoom(c)

	c.logger.Info("client joined", "id", c.id, "name", c.name, "platform", c.platform, "ipRoom", c.ipRoom.ID())
}

// handleCreateRoom creates a new public room
func (c *Client) handleCreateRoom() {
	code := c.hub.CreatePublicRoom(c)

	msg, _ := NewRoomCreatedMessage(code)
	c.Send(msg)

	c.logger.Info("public room created", "code", code, "creator", c.id)
}

// handleJoinRoom joins a public room by code
func (c *Client) handleJoinRoom(payload json.RawMessage) {
	var roomPayload RoomCodePayload
	if err := json.Unmarshal(payload, &roomPayload); err != nil {
		c.logger.Warn("failed to unmarshal room code payload", "error", err)
		return
	}

	if err := c.hub.JoinPublicRoom(c, roomPayload.Code); err != nil {
		msg, _ := NewRoomErrorMessage(err.Error())
		c.Send(msg)
		return
	}

	c.logger.Info("client joined public room", "code", roomPayload.Code, "clientID", c.id)
}

// handleLeaveRoom leaves the current public room
func (c *Client) handleLeaveRoom() {
	if c.publicRoom != nil {
		c.hub.LeavePublicRoom(c)
		c.logger.Info("client left public room", "clientID", c.id)
	}
}

// relayToTarget forwards a message to the target peer
func (c *Client) relayToTarget(msg Message, rawData []byte) {
	if msg.TargetID == "" {
		return
	}

	// Add sender ID to the message
	msg.PeerID = c.id
	data, _ := json.Marshal(msg)

	// Try to relay in IP room first
	if c.ipRoom != nil && c.ipRoom.RelayTo(msg.TargetID, data) {
		return
	}

	// Try public room
	if c.publicRoom != nil && c.publicRoom.RelayTo(msg.TargetID, data) {
		return
	}

	c.logger.Debug("relay target not found", "targetID", msg.TargetID)
}
