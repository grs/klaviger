package observability

import (
	"fmt"

	"github.com/grs/klaviger/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// SetupLogging initializes the logging system based on configuration
func SetupLogging(cfg *config.LoggingConfig) (*zap.Logger, error) {
	var zapConfig zap.Config

	// Set encoding based on format
	if cfg.Format == "json" {
		zapConfig = zap.NewProductionConfig()
	} else {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// Set log level
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %s: %w", cfg.Level, err)
	}
	zapConfig.Level = zap.NewAtomicLevelAt(level)

	// Disable stack traces for info and below in production
	if cfg.Format == "json" {
		zapConfig.DisableStacktrace = true
	}

	logger, err := zapConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build logger: %w", err)
	}

	// Replace global logger
	zap.ReplaceGlobals(logger)

	return logger, nil
}
