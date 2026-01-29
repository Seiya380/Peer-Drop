package transfer

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusAccepted   Status = "accepted"
	StatusRejected   Status = "rejected"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

type FileInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

type Transfer struct {
	ID          string     `json:"id"`
	Status      Status     `json:"status"`
	Files       []FileInfo `json:"files"`
	SenderID    string     `json:"sender_id"`
	SenderName  string     `json:"sender_name"`
	SenderAddr  string     `json:"sender_addr"`
	Progress    float64    `json:"progress"`
	BytesRecv   int64      `json:"bytes_received"`
	TotalBytes  int64      `json:"total_bytes"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt time.Time  `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type Manager struct {
	mu        sync.RWMutex
	transfers map[string]*Transfer
	pending   chan *Transfer
}

func NewManager() *Manager {
	return &Manager{
		transfers: make(map[string]*Transfer),
		pending:   make(chan *Transfer, 100),
	}
}

func (m *Manager) CreateTransfer(senderID, senderName, senderAddr string, files []FileInfo) *Transfer {
	id := generateID()

	var totalBytes int64
	for _, f := range files {
		totalBytes += f.Size
	}

	transfer := &Transfer{
		ID:         id,
		Status:     StatusPending,
		Files:      files,
		SenderID:   senderID,
		SenderName: senderName,
		SenderAddr: senderAddr,
		TotalBytes: totalBytes,
		CreatedAt:  time.Now(),
	}

	m.mu.Lock()
	m.transfers[id] = transfer
	m.mu.Unlock()

	// Notify pending channel (non-blocking)
	select {
	case m.pending <- transfer:
	default:
	}

	return transfer
}

func (m *Manager) Get(id string) *Transfer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.transfers[id]
}

func (m *Manager) Accept(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.transfers[id]
	if !ok || t.Status != StatusPending {
		return false
	}

	t.Status = StatusAccepted
	return true
}

func (m *Manager) Reject(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.transfers[id]
	if !ok || t.Status != StatusPending {
		return false
	}

	t.Status = StatusRejected
	return true
}

func (m *Manager) SetInProgress(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.transfers[id]
	if !ok || t.Status != StatusAccepted {
		return false
	}

	t.Status = StatusInProgress
	return true
}

func (m *Manager) UpdateProgress(id string, bytesRecv int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.transfers[id]
	if !ok {
		return
	}

	t.BytesRecv = bytesRecv
	if t.TotalBytes > 0 {
		t.Progress = float64(bytesRecv) / float64(t.TotalBytes)
	}
}

func (m *Manager) Complete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.transfers[id]
	if !ok {
		return
	}

	t.Status = StatusCompleted
	t.Progress = 1.0
	t.BytesRecv = t.TotalBytes
	t.CompletedAt = time.Now()
}

func (m *Manager) Fail(id string, err string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.transfers[id]
	if !ok {
		return
	}

	t.Status = StatusFailed
	t.Error = err
	t.CompletedAt = time.Now()
}

func (m *Manager) GetAll() []*Transfer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	transfers := make([]*Transfer, 0, len(m.transfers))
	for _, t := range m.transfers {
		transfers = append(transfers, t)
	}
	return transfers
}

func (m *Manager) GetPending() []*Transfer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []*Transfer
	for _, t := range m.transfers {
		if t.Status == StatusPending {
			pending = append(pending, t)
		}
	}
	return pending
}

func (m *Manager) PendingChan() <-chan *Transfer {
	return m.pending
}

func (m *Manager) CleanOld(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, t := range m.transfers {
		if t.Status == StatusCompleted || t.Status == StatusRejected || t.Status == StatusFailed {
			if t.CompletedAt.Before(cutoff) {
				delete(m.transfers, id)
			}
		}
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
