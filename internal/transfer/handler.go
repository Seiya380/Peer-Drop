package transfer

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Handler struct {
	manager     *Manager
	downloadDir string
	logger      *slog.Logger
}

func NewHandler(manager *Manager, downloadDir string, logger *slog.Logger) *Handler {
	return &Handler{
		manager:     manager,
		downloadDir: downloadDir,
		logger:      logger,
	}
}

type TransferRequestBody struct {
	SenderID   string     `json:"sender_id"`
	SenderName string     `json:"sender_name"`
	Files      []FileInfo `json:"files"`
}

type TransferResponse struct {
	TransferID string `json:"transfer_id"`
	Status     Status `json:"status"`
}

func (h *Handler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body TransferRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	senderAddr := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		senderAddr = strings.Split(fwd, ",")[0]
	}

	transfer := h.manager.CreateTransfer(body.SenderID, body.SenderName, senderAddr, body.Files)

	h.logger.Info("transfer request received",
		"id", transfer.ID,
		"sender", body.SenderName,
		"files", len(body.Files))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(TransferResponse{
		TransferID: transfer.ID,
		Status:     transfer.Status,
	})
}

func (h *Handler) HandleAccept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Transfer ID required", http.StatusBadRequest)
		return
	}

	if !h.manager.Accept(id) {
		http.Error(w, "Transfer not found or not pending", http.StatusNotFound)
		return
	}

	h.logger.Info("transfer accepted", "id", id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TransferResponse{
		TransferID: id,
		Status:     StatusAccepted,
	})
}

func (h *Handler) HandleReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Transfer ID required", http.StatusBadRequest)
		return
	}

	if !h.manager.Reject(id) {
		http.Error(w, "Transfer not found or not pending", http.StatusNotFound)
		return
	}

	h.logger.Info("transfer rejected", "id", id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TransferResponse{
		TransferID: id,
		Status:     StatusRejected,
	})
}

func (h *Handler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Transfer ID required", http.StatusBadRequest)
		return
	}

	transfer := h.manager.Get(id)
	if transfer == nil {
		http.Error(w, "Transfer not found", http.StatusNotFound)
		return
	}

	if transfer.Status != StatusAccepted {
		http.Error(w, "Transfer not accepted", http.StatusBadRequest)
		return
	}

	h.manager.SetInProgress(id)

	// Parse multipart form (64MB max memory)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		h.manager.Fail(id, err.Error())
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Ensure download directory exists
	if err := os.MkdirAll(h.downloadDir, 0755); err != nil {
		h.manager.Fail(id, err.Error())
		http.Error(w, "Failed to create download directory", http.StatusInternalServerError)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		h.manager.Fail(id, err.Error())
		http.Error(w, "Failed to get file from form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Sanitize filename
	safeName := sanitizeFilename(header.Filename)
	destPath := filepath.Join(h.downloadDir, safeName)

	// Handle filename conflicts
	destPath = uniquePath(destPath)

	h.logger.Info("receiving file", "id", id, "name", safeName, "size", header.Size)

	dst, err := os.Create(destPath)
	if err != nil {
		h.manager.Fail(id, err.Error())
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy with progress tracking
	var written int64
	buf := make([]byte, 32*1024)
	for {
		nr, readErr := file.Read(buf)
		if nr > 0 {
			nw, writeErr := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				h.manager.Fail(id, "invalid write result")
				http.Error(w, "Write error", http.StatusInternalServerError)
				return
			}
			written += int64(nw)
			h.manager.UpdateProgress(id, written)

			if writeErr != nil {
				h.manager.Fail(id, writeErr.Error())
				http.Error(w, "Write error", http.StatusInternalServerError)
				return
			}
			if nr != nw {
				h.manager.Fail(id, "short write")
				http.Error(w, "Short write", http.StatusInternalServerError)
				return
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			h.manager.Fail(id, readErr.Error())
			http.Error(w, "Read error", http.StatusInternalServerError)
			return
		}
	}

	h.manager.Complete(id)
	h.logger.Info("file received successfully", "id", id, "path", destPath, "bytes", written)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TransferResponse{
		TransferID: id,
		Status:     StatusCompleted,
	})
}

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Transfer ID required", http.StatusBadRequest)
		return
	}

	transfer := h.manager.Get(id)
	if transfer == nil {
		http.Error(w, "Transfer not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transfer)
}

func (h *Handler) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	transfers := h.manager.GetAll()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"transfers": transfers,
	})
}

func sanitizeFilename(name string) string {
	// Get base name only
	name = filepath.Base(name)

	// Replace dangerous characters
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)

	name = replacer.Replace(name)

	// Remove leading dots (hidden files)
	for len(name) > 0 && name[0] == '.' {
		name = name[1:]
	}

	if name == "" {
		name = "unnamed_file"
	}

	return name
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	for i := 1; i < 1000; i++ {
		newPath := base + "_" + string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10)) + ext
		if i < 10 {
			newPath = base + "_" + string(rune('0'+i)) + ext
		} else if i < 100 {
			newPath = base + "_" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + ext
		}
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}

	return path
}
