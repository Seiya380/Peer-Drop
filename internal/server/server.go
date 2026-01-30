package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"Peer-Drop/internal/signaling"
	"Peer-Drop/web"
)

type Server struct {
	httpServer *http.Server
	hub        *signaling.Hub
	logger     *slog.Logger
	port       int
}

func New(port int, logger *slog.Logger) *Server {
	hub := signaling.NewHub(logger)

	s := &Server{
		hub:    hub,
		logger: logger,
		port:   port,
	}

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	s.httpServer = &http.Server{
		Addr:        fmt.Sprintf(":%d", port),
		Handler:     corsMiddleware(logMiddleware(mux, logger)),
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 120 * time.Second,
	}

	// Start room cleanup
	hub.StartCleanup(5 * time.Minute)

	return s
}

func (s *Server) setupRoutes(mux *http.ServeMux) {
	// WebSocket endpoint
	mux.HandleFunc("GET /ws", s.hub.HandleWebSocket)

	// API routes
	mux.HandleFunc("GET /api/stats", s.handleStats)

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

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.hub.Stats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
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
		// Don't log WebSocket upgrades (they log separately)
		if r.URL.Path != "/ws" {
			logger.Debug("request",
				"method", r.Method,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"duration", time.Since(start))
		}
	})
}
