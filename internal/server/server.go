package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"Peer-Drop/internal/discovery"
	"Peer-Drop/internal/transfer"
	"Peer-Drop/web"
)

type Server struct {
	httpServer      *http.Server
	peerRegistry    *discovery.PeerRegistry
	transferManager *transfer.Manager
	transferHandler *transfer.Handler
	logger          *slog.Logger
}

func New(port int, peerRegistry *discovery.PeerRegistry, transferManager *transfer.Manager, downloadDir string, logger *slog.Logger) *Server {
	s := &Server{
		peerRegistry:    peerRegistry,
		transferManager: transferManager,
		transferHandler: transfer.NewHandler(transferManager, downloadDir, logger),
		logger:          logger,
	}

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      corsMiddleware(logMiddleware(mux, logger)),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // No timeout for file uploads
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) setupRoutes(mux *http.ServeMux) {
	// API routes
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/peers", s.handlePeers)
	mux.HandleFunc("POST /api/transfer/request", s.transferHandler.HandleRequest)
	mux.HandleFunc("POST /api/transfer/{id}/accept", s.transferHandler.HandleAccept)
	mux.HandleFunc("POST /api/transfer/{id}/reject", s.transferHandler.HandleReject)
	mux.HandleFunc("POST /api/transfer/{id}/upload", s.transferHandler.HandleUpload)
	mux.HandleFunc("GET /api/transfer/{id}/status", s.transferHandler.HandleStatus)
	mux.HandleFunc("GET /api/transfers", s.transferHandler.HandleList)

	// Static files and web UI
	mux.Handle("GET /static/", http.FileServer(http.FS(web.Assets)))
	mux.HandleFunc("GET /", s.handleIndex)
}

func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	self := s.peerRegistry.Self()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":       self.ID,
		"name":     self.Name,
		"port":     self.Port,
		"platform": self.Platform,
	})
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	peers := s.peerRegistry.GetActive(discovery.PeerTTL)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": peers,
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data, err := web.Assets.ReadFile("templates/index.html")
	if err != nil {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func logMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Debug("request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"duration", time.Since(start))
	})
}
