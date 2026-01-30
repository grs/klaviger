package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/spiffe"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// Server manages HTTP servers for reverse and forward proxies
type Server struct {
	reverseProxyServer *http.Server
	forwardProxyServer *http.Server
	metricsServer      *http.Server
	spiffeSource       *spiffe.Source
	logger             *zap.Logger
}

// New creates a new Server instance
func New(
	reverseProxyHandler http.Handler,
	forwardProxyHandler http.Handler,
	metricsHandler http.Handler,
	cfg *config.Config,
	logger *zap.Logger,
) (*Server, error) {
	// Create reverse proxy server
	reverseProxyServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.ReverseProxyBind, cfg.Server.ReverseProxyPort),
		Handler:      reverseProxyHandler,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout),
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout),
	}

	var spiffeSource *spiffe.Source

	// Configure TLS if enabled
	if cfg.Server.TLS.Enabled {
		// Check if using SPIFFE
		if cfg.Server.TLS.SPIFFE != nil && cfg.Server.TLS.SPIFFE.Enabled {
			// Create SPIFFE source for server TLS
			ctx := context.Background()
			src, err := spiffe.NewSource(ctx, cfg.Server.TLS.SPIFFE.SocketPath, "server", logger)
			if err != nil {
				return nil, fmt.Errorf("failed to create SPIFFE source for server TLS: %w", err)
			}
			spiffeSource = src

			// Get server TLS config from SPIFFE source
			tlsConfig, err := src.GetServerTLSConfig(cfg.Server.TLS.SPIFFE.TrustDomain, cfg.Server.TLS.SPIFFE.AcceptedSPIFFEIDs)
			if err != nil {
				src.Close()
				return nil, fmt.Errorf("failed to get server TLS config from SPIFFE: %w", err)
			}
			reverseProxyServer.TLSConfig = tlsConfig
		} else {
			// Use file-based TLS
			reverseProxyServer.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
		}
	}

	// Create forward proxy server
	forwardProxyServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.ForwardProxyBind, cfg.Server.ForwardProxyPort),
		Handler:      forwardProxyHandler,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout),
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout),
	}

	// Create metrics server if enabled
	var metricsServer *http.Server
	if cfg.Observability.Metrics.Enabled {
		metricsServer = &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Observability.Metrics.Port),
			Handler:      metricsHandler,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		}
	}

	return &Server{
		reverseProxyServer: reverseProxyServer,
		forwardProxyServer: forwardProxyServer,
		metricsServer:      metricsServer,
		spiffeSource:       spiffeSource,
		logger:             logger,
	}, nil
}

// Start starts all HTTP servers
func (s *Server) Start(ctx context.Context, cfg *config.Config) error {
	g, ctx := errgroup.WithContext(ctx)

	// Start reverse proxy server
	g.Go(func() error {
		usingSPIFFE := cfg.Server.TLS.Enabled && cfg.Server.TLS.SPIFFE != nil && cfg.Server.TLS.SPIFFE.Enabled

		s.logger.Info("starting reverse proxy server",
			zap.String("addr", s.reverseProxyServer.Addr),
			zap.Bool("tls", cfg.Server.TLS.Enabled),
			zap.Bool("spiffe", usingSPIFFE),
		)

		var err error
		if cfg.Server.TLS.Enabled {
			if usingSPIFFE {
				// For SPIFFE, TLS config is already set in reverseProxyServer.TLSConfig
				// Call ListenAndServeTLS with empty cert/key paths
				err = s.reverseProxyServer.ListenAndServeTLS("", "")
			} else {
				// Use file-based certificates
				err = s.reverseProxyServer.ListenAndServeTLS(
					cfg.Server.TLS.CertFile,
					cfg.Server.TLS.KeyFile,
				)
			}
		} else {
			err = s.reverseProxyServer.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("reverse proxy server error: %w", err)
		}
		return nil
	})

	// Start forward proxy server
	g.Go(func() error {
		s.logger.Info("starting forward proxy server",
			zap.String("addr", s.forwardProxyServer.Addr),
		)

		err := s.forwardProxyServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("forward proxy server error: %w", err)
		}
		return nil
	})

	// Start metrics server if enabled
	if s.metricsServer != nil {
		g.Go(func() error {
			s.logger.Info("starting metrics server",
				zap.String("addr", s.metricsServer.Addr),
			)

			err := s.metricsServer.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("metrics server error: %w", err)
			}
			return nil
		})
	}

	// Wait for context cancellation
	g.Go(func() error {
		<-ctx.Done()
		s.logger.Info("shutting down servers")
		return s.Shutdown(context.Background())
	})

	return g.Wait()
}

// Shutdown gracefully shuts down all servers
func (s *Server) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	g, shutdownCtx := errgroup.WithContext(shutdownCtx)

	// Shutdown reverse proxy server
	g.Go(func() error {
		s.logger.Info("shutting down reverse proxy server")
		if err := s.reverseProxyServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("reverse proxy shutdown error: %w", err)
		}
		return nil
	})

	// Shutdown forward proxy server
	g.Go(func() error {
		s.logger.Info("shutting down forward proxy server")
		if err := s.forwardProxyServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("forward proxy shutdown error: %w", err)
		}
		return nil
	})

	// Shutdown metrics server if enabled
	if s.metricsServer != nil {
		g.Go(func() error {
			s.logger.Info("shutting down metrics server")
			if err := s.metricsServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("metrics server shutdown error: %w", err)
			}
			return nil
		})
	}

	// Close SPIFFE source if present
	if s.spiffeSource != nil {
		g.Go(func() error {
			s.logger.Info("closing SPIFFE source")
			if err := s.spiffeSource.Close(); err != nil {
				return fmt.Errorf("SPIFFE source close error: %w", err)
			}
			return nil
		})
	}

	return g.Wait()
}

// HealthHandler returns a handler for health endpoints
func HealthHandler() http.Handler {
	mux := http.NewServeMux()

	// Liveness probe
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Readiness probe
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Add actual readiness checks (e.g., backend availability)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	return mux
}
