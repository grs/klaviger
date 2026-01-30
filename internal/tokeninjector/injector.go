package tokeninjector

import (
	"context"
	"fmt"
	"net/http"

	"github.com/grs/klaviger/internal/config"
	"go.uber.org/zap"
)

// Injector is the interface for token injection
type Injector interface {
	// Inject injects a token into the request
	Inject(ctx context.Context, req *http.Request) error

	// Close closes any resources held by the injector
	Close() error
}

// NewInjector creates a new injector based on configuration
func NewInjector(cfg *config.InjectionMode, serverCfg *config.ServerConfig, logger *zap.Logger) (Injector, error) {
	switch cfg.Type {
	case "passthrough":
		return NewPassthroughInjector(logger), nil
	case "file":
		if cfg.File == nil {
			return nil, fmt.Errorf("file config is required for file mode")
		}
		return NewFileInjector(cfg.File, logger)
	case "oauth":
		if cfg.OAuth == nil {
			return nil, fmt.Errorf("oauth config is required for oauth mode")
		}
		return NewOAuthInjector(cfg.OAuth, serverCfg, logger)
	case "vault":
		if cfg.Vault == nil {
			return nil, fmt.Errorf("vault config is required for vault mode")
		}
		return NewVaultInjector(cfg.Vault, logger)
	default:
		return nil, fmt.Errorf("unknown injection mode: %s", cfg.Type)
	}
}
