package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/grs/klaviger/internal/auth"
	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/forwardproxy"
	"github.com/grs/klaviger/internal/observability"
	"github.com/grs/klaviger/internal/reverseproxy"
	"github.com/grs/klaviger/internal/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	configPath = flag.String("config", "config.yaml", "Path to configuration file")
	version    = flag.Bool("version", false, "Print version and exit")
)

const (
	appVersion = "0.1.0"
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("klaviger version %s\n", appVersion)
		os.Exit(0)
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logging
	logger, err := observability.SetupLogging(&cfg.Observability.Logging)
	if err != nil {
		return fmt.Errorf("failed to setup logging: %w", err)
	}
	defer logger.Sync()

	logger.Info("klaviger starting",
		zap.String("version", appVersion),
		zap.String("config", *configPath),
	)

	// Log sanitized configuration
	sanitized := cfg.Sanitize()
	logger.Debug("loaded configuration",
		zap.Any("config", sanitized),
	)

	// Setup tracing
	ctx := context.Background()
	shutdownTracing, err := observability.SetupTracing(ctx, &cfg.Observability.Tracing)
	if err != nil {
		return fmt.Errorf("failed to setup tracing: %w", err)
	}
	defer shutdownTracing(ctx)

	// Create placeholder handlers (will be replaced in later phases)
	reverseProxyHandler := createReverseProxyHandler(cfg, logger)
	forwardProxyHandler := createForwardProxyHandler(cfg, logger)
	metricsHandler := createMetricsHandler(cfg)

	// Create and start server
	srv, err := server.New(
		reverseProxyHandler,
		forwardProxyHandler,
		metricsHandler,
		cfg,
		logger,
	)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal, initiating shutdown", zap.String("signal", sig.String()))
		cancel()
	}()

	// Start server
	logger.Info("klaviger started successfully")
	if err := srv.Start(ctx, cfg); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	logger.Info("klaviger shutdown complete")
	return nil
}

// createReverseProxyHandler creates the reverse proxy handler
func createReverseProxyHandler(cfg *config.Config, logger *zap.Logger) http.Handler {
	// Create verifier
	verifier, err := auth.NewVerifier(&cfg.ReverseProxy.Verification, logger)
	if err != nil {
		logger.Fatal("failed to create verifier", zap.Error(err))
	}

	// Create reverse proxy
	proxy, err := reverseproxy.New(&cfg.ReverseProxy, verifier, logger)
	if err != nil {
		logger.Fatal("failed to create reverse proxy", zap.Error(err))
	}

	return proxy.Handler()
}

// createForwardProxyHandler creates the forward proxy handler
func createForwardProxyHandler(cfg *config.Config, logger *zap.Logger) http.Handler {
	// Create forward proxy
	proxy, err := forwardproxy.New(&cfg.ForwardProxy, &cfg.Server, logger)
	if err != nil {
		logger.Fatal("failed to create forward proxy", zap.Error(err))
	}

	return proxy.Handler()
}

// createMetricsHandler creates the metrics handler
func createMetricsHandler(cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(cfg.Observability.Metrics.Path, promhttp.Handler())
	return mux
}
