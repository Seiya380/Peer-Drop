package signaling

import "encoding/json"

// Message types
const (
	TypeJoin             = "join"
	TypePeers            = "peers"
	TypePeerJoined       = "peer-joined"
	TypePeerLeft         = "peer-left"
	TypeOffer            = "offer"
	TypeAnswer           = "answer"
	TypeIceCandidate     = "ice-candidate"
	TypeTransferRequest  = "transfer-request"
	TypeTransferResponse = "transfer-response"
	TypePing             = "ping"
	TypePong             = "pong"
	TypeCreateRoom       = "create-room"
	TypeRoomCreated      = "room-created"
	TypeJoinRoom         = "join-room"
	TypeLeaveRoom        = "leave-room"
	TypeRoomJoined       = "room-joined"
	TypeRoomLeft         = "room-left"
	TypeRoomError        = "room-error"
	TypeRelayChunk       = "relay-chunk"
)

// Message is the base structure for all WebSocket messages
type Message struct {
	Type     string          `json:"type"`
	PeerID   string          `json:"peerId,omitempty"`
	TargetID string          `json:"targetId,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// JoinPayload is sent when a client connects
type JoinPayload struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// PeerInfo represents a peer in the network
type PeerInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// PeersPayload is the list of peers in a room
type PeersPayload struct {
	Peers []PeerInfo `json:"peers"`
}

// PeerJoinedPayload is sent when a new peer joins
type PeerJoinedPayload struct {
	Peer PeerInfo `json:"peer"`
}

// PeerLeftPayload is sent when a peer leaves
type PeerLeftPayload struct {
	PeerID string `json:"peerId"`
}

// SDPPayload for WebRTC offer/answer
type SDPPayload struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"`
}

// ICECandidatePayload for WebRTC ICE candidates
type ICECandidatePayload struct {
	Candidate     string `json:"candidate"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex int    `json:"sdpMLineIndex"`
}

// FileInfo represents a file to be transferred
type FileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// TransferRequestPayload is sent to request a file transfer
type TransferRequestPayload struct {
	TransferID string     `json:"transferId"`
	Files      []FileInfo `json:"files"`
	TotalSize  int64      `json:"totalSize"`
}

// TransferResponsePayload is the response to a transfer request
type TransferResponsePayload struct {
	TransferID string `json:"transferId"`
	Accepted   bool   `json:"accepted"`
}

// RoomCodePayload for public room operations
type RoomCodePayload struct {
	Code string `json:"code"`
}

// RelayChunkPayload for WebSocket relay fallback
type RelayChunkPayload struct {
	TransferID string `json:"transferId"`
	FileIndex  int    `json:"fileIndex"`
	ChunkIndex int    `json:"chunkIndex"`
	Data       string `json:"data"` // base64 encoded
	IsLast     bool   `json:"isLast"`
}

// Helper functions to create messages

func NewPeersMessage(peers []PeerInfo) ([]byte, error) {
	payload, _ := json.Marshal(PeersPayload{Peers: peers})
	return json.Marshal(Message{
		Type:    TypePeers,
		Payload: payload,
	})
}

func NewPeerJoinedMessage(peer PeerInfo) ([]byte, error) {
	payload, _ := json.Marshal(PeerJoinedPayload{Peer: peer})
	return json.Marshal(Message{
		Type:    TypePeerJoined,
		Payload: payload,
	})
}

func NewPeerLeftMessage(peerID string) ([]byte, error) {
	payload, _ := json.Marshal(PeerLeftPayload{PeerID: peerID})
	return json.Marshal(Message{
		Type:    TypePeerLeft,
		Payload: payload,
	})
}

func NewRoomCreatedMessage(code string) ([]byte, error) {
	payload, _ := json.Marshal(RoomCodePayload{Code: code})
	return json.Marshal(Message{
		Type:    TypeRoomCreated,
		Payload: payload,
	})
}

func NewRoomJoinedMessage(code string, peers []PeerInfo) ([]byte, error) {
	payload, _ := json.Marshal(struct {
		Code  string     `json:"code"`
		Peers []PeerInfo `json:"peers"`
	}{Code: code, Peers: peers})
	return json.Marshal(Message{
		Type:    TypeRoomJoined,
		Payload: payload,
	})
}

func NewRoomErrorMessage(err string) ([]byte, error) {
	payload, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: err})
	return json.Marshal(Message{
		Type:    TypeRoomError,
		Payload: payload,
	})
}

func NewPongMessage() []byte {
	msg, _ := json.Marshal(Message{Type: TypePong})
	return msg
}
