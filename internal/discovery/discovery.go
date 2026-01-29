package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/schollz/peerdiscovery"
)

const (
	DiscoveryPort     = 9999
	BroadcastInterval = 3 * time.Second
	PeerTTL           = 10 * time.Second
)

type PeerPayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Platform string `json:"platform"`
}

type Service struct {
	registry *PeerRegistry
	logger   *slog.Logger
	cancel   context.CancelFunc
}

func NewService(registry *PeerRegistry, logger *slog.Logger) *Service {
	return &Service{
		registry: registry,
		logger:   logger,
	}
}

func (s *Service) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	self := s.registry.Self()
	payload := PeerPayload{
		ID:       self.ID,
		Name:     self.Name,
		Port:     self.Port,
		Platform: self.Platform,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Start cleanup goroutine
	go s.cleanupLoop(ctx)

	// Start discovery
	go func() {
		s.logger.Info("starting peer discovery", "port", DiscoveryPort)

		_, err := peerdiscovery.Discover(peerdiscovery.Settings{
			Limit:     -1,
			Port:      fmt.Sprintf("%d", DiscoveryPort),
			MulticastAddress: "239.255.255.250",
			Payload:   payloadBytes,
			Delay:     BroadcastInterval,
			TimeLimit: -1, // Run forever

			Notify: func(d peerdiscovery.Discovered) {
				var peerPayload PeerPayload
				if err := json.Unmarshal(d.Payload, &peerPayload); err != nil {
					s.logger.Warn("failed to unmarshal peer payload", "error", err)
					return
				}

				peer := &Peer{
					ID:       peerPayload.ID,
					Name:     peerPayload.Name,
					Address:  d.Address,
					Port:     peerPayload.Port,
					Platform: peerPayload.Platform,
				}

				s.registry.Update(peer)
				s.logger.Debug("discovered peer", "name", peer.Name, "address", peer.Address)
			},
		})

		if err != nil && ctx.Err() == nil {
			s.logger.Error("discovery error", "error", err)
		}
	}()

	return nil
}

func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(PeerTTL / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.registry.CleanExpired(PeerTTL)
		}
	}
}
