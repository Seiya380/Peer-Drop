package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"Peer-Drop/internal/config"
	"Peer-Drop/internal/server"
)

var (
	version = "2.0.0"
)

func main() {
	// Flags
	port := flag.Int("port", 0, "Server port (default: 8080)")
	verbose := flag.Bool("verbose", false, "Verbose logging")
	showVersion := flag.Bool("version", false, "Show version")
	showHelp := flag.Bool("help", false, "Show help")

	flag.Parse()

	if *showVersion {
		fmt.Printf("Peer-Drop v%s\n", version)
		return
	}

	if *showHelp {
		printHelp()
		return
	}

	runServer(*port, *verbose)
}

func printHelp() {
	fmt.Println(`Peer-Drop - Local Network File Sharing

Usage:
  peer-drop [flags]

Flags:
  -port int       Server port (default 8080)
  -verbose        Enable verbose logging
  -version        Print version information
  -help           Show this help message

How it works:
  1. Run peer-drop on any computer
  2. Open http://localhost:8080 in any browser (desktop or phone)
  3. Devices on the same network automatically see each other
  4. Click on a device to send files

Features:
  - Automatic peer discovery on local network
  - Works in any browser (no app needed)
  - WebRTC for fast P2P file transfers
  - Public rooms for sharing across networks`)
}

func runServer(port int, verbose bool) {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Override with flags
	if port > 0 {
		cfg.Port = port
	}

	// Setup logger
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	// Start server
	srv := server.New(cfg.Port, logger)

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("shutting down...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		srv.Shutdown(shutdownCtx)
		os.Exit(0)
	}()

	fmt.Printf("\n")
	fmt.Printf("  Peer-Drop v%s\n", version)
	fmt.Printf("  ─────────────────────────────\n")
	fmt.Printf("  Port: %d\n", cfg.Port)
	fmt.Printf("\n")
	fmt.Printf("  Open in your browser:\n")
	fmt.Printf("  → http://localhost:%d\n", cfg.Port)
	fmt.Printf("\n")
	fmt.Printf("  On other devices (same network):\n")
	fmt.Printf("  → http://<this-computer-ip>:%d\n", cfg.Port)
	fmt.Printf("\n")

	if err := srv.Start(); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
