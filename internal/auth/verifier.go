package auth

import (
	"context"
	"fmt"

	"github.com/grs/klaviger/internal/config"
	"go.uber.org/zap"
)

// Claims represents the claims extracted from a verified token
type Claims struct {
	Subject   string
	Issuer    string
	Audience  []string
	Scopes    []string
	ExpiresAt int64
	IssuedAt  int64
	NotBefore int64
	Extra     map[string]interface{}
}

// Verifier is the interface for token verification
type Verifier interface {
	// Verify verifies a token and returns the claims
	Verify(ctx context.Context, token string) (*Claims, error)
}

// NewVerifier creates a new verifier based on configuration
func NewVerifier(cfg *config.VerificationConfig, logger *zap.Logger) (Verifier, error) {
	switch cfg.Mode {
	case "jwt":
		if cfg.JWT == nil {
			return nil, fmt.Errorf("JWT config is required for jwt mode")
		}
		return NewJWTVerifier(cfg.JWT, logger)
	case "introspection":
		if cfg.Introspection == nil {
			return nil, fmt.Errorf("introspection config is required for introspection mode")
		}
		return NewIntrospectionVerifier(cfg.Introspection, logger)
	case "k8s", "kubernetes":
		if cfg.Kubernetes == nil {
			return nil, fmt.Errorf("kubernetes config is required for k8s mode")
		}
		return NewKubernetesVerifier(cfg.Kubernetes, logger)
	default:
		return nil, fmt.Errorf("unknown verification mode: %s", cfg.Mode)
	}
}
