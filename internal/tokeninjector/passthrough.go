package tokeninjector

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

// PassthroughInjector passes requests through without modification
type PassthroughInjector struct {
	logger *zap.Logger
}

// NewPassthroughInjector creates a new passthrough injector
func NewPassthroughInjector(logger *zap.Logger) *PassthroughInjector {
	return &PassthroughInjector{
		logger: logger.With(zap.String("component", "passthrough_injector")),
	}
}

// Inject does nothing (passthrough)
func (i *PassthroughInjector) Inject(ctx context.Context, req *http.Request) error {
	i.logger.Debug("passthrough: no token injection",
		zap.String("host", req.Host),
	)
	return nil
}

// Close closes the injector
func (i *PassthroughInjector) Close() error {
	return nil
}
