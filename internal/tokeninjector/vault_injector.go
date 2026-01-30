package tokeninjector

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/observability"
	"github.com/grs/klaviger/internal/util"
	vault "github.com/hashicorp/vault/api"
	"go.uber.org/zap"
)

// VaultInjector injects tokens from HashiCorp Vault
type VaultInjector struct {
	address    string
	path       string
	field      string
	authMethod string
	role       string
	cacheTTL   time.Duration
	cache      *util.TokenCache
	client     *vault.Client
	logger     *zap.Logger
}

// NewVaultInjector creates a new Vault injector
func NewVaultInjector(cfg *config.VaultConfig, logger *zap.Logger) (*VaultInjector, error) {
	// Create Vault client
	vaultConfig := vault.DefaultConfig()
	vaultConfig.Address = cfg.Address

	client, err := vault.NewClient(vaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	injector := &VaultInjector{
		address:    cfg.Address,
		path:       cfg.Path,
		field:      cfg.Field,
		authMethod: cfg.AuthMethod,
		role:       cfg.Role,
		cacheTTL:   time.Duration(cfg.CacheTTL),
		cache:      util.NewTokenCache(logger.With(zap.String("component", "vault_token_cache"))),
		client:     client,
		logger:     logger.With(zap.String("component", "vault_injector")),
	}

	// Authenticate with Vault
	if err := injector.authenticate(cfg); err != nil {
		return nil, fmt.Errorf("vault authentication failed: %w", err)
	}

	return injector, nil
}

// authenticate authenticates with Vault
func (i *VaultInjector) authenticate(cfg *config.VaultConfig) error {
	switch cfg.AuthMethod {
	case "kubernetes":
		return i.authenticateKubernetes()
	case "token":
		if cfg.Token == "" {
			return fmt.Errorf("token is required for token auth method")
		}
		i.client.SetToken(cfg.Token)
		i.logger.Info("authenticated with vault using token")
		return nil
	default:
		return fmt.Errorf("unsupported auth method: %s", cfg.AuthMethod)
	}
}

// authenticateKubernetes authenticates using Kubernetes service account
func (i *VaultInjector) authenticateKubernetes() error {
	// Read service account token
	jwtPath := os.Getenv("KUBERNETES_SERVICE_ACCOUNT_TOKEN_PATH")
	if jwtPath == "" {
		jwtPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	jwt, err := os.ReadFile(jwtPath)
	if err != nil {
		return fmt.Errorf("failed to read service account token: %w", err)
	}

	// Prepare auth request
	data := map[string]interface{}{
		"role": i.role,
		"jwt":  string(jwt),
	}

	// Authenticate
	secret, err := i.client.Logical().Write("auth/kubernetes/login", data)
	if err != nil {
		return fmt.Errorf("kubernetes auth failed: %w", err)
	}

	if secret == nil || secret.Auth == nil {
		return fmt.Errorf("no auth info returned from vault")
	}

	// Set token
	i.client.SetToken(secret.Auth.ClientToken)

	i.logger.Info("authenticated with vault using kubernetes",
		zap.String("role", i.role),
		zap.Duration("lease_duration", time.Duration(secret.Auth.LeaseDuration)*time.Second),
	)

	// TODO: Implement token renewal based on lease duration

	return nil
}

// Inject injects a token from Vault
func (i *VaultInjector) Inject(ctx context.Context, req *http.Request) error {
	start := time.Now()
	result := "success"
	defer func() {
		observability.TokenInjectionDuration.WithLabelValues("vault", result).Observe(time.Since(start).Seconds())
	}()

	// Check cache first
	cacheKey := i.path
	if cachedToken, ok := i.cache.Get(cacheKey); ok {
		req.Header.Set("Authorization", "Bearer "+cachedToken)
		observability.TokenCacheHits.WithLabelValues("vault", "hit").Inc()
		i.logger.Debug("using cached vault token", zap.String("host", req.Host))
		return nil
	}

	observability.TokenCacheHits.WithLabelValues("vault", "miss").Inc()

	// Read secret from Vault
	token, leaseDuration, err := i.readSecret(ctx)
	if err != nil {
		result = "read_failed"
		i.logger.Error("failed to read vault secret", zap.Error(err))
		return fmt.Errorf("failed to read vault secret: %w", err)
	}

	// Cache the token
	ttl := i.cacheTTL
	if leaseDuration > 0 {
		// Use lease duration if available, but cap at configured TTL
		leaseTTL := time.Duration(leaseDuration) * time.Second
		if leaseTTL < ttl {
			ttl = leaseTTL
		}
	}
	i.cache.Set(cacheKey, token, ttl)

	// Set Authorization header
	req.Header.Set("Authorization", "Bearer "+token)

	i.logger.Debug("vault token injected",
		zap.String("host", req.Host),
		zap.String("path", i.path),
		zap.Duration("ttl", ttl),
	)

	return nil
}

// readSecret reads a secret from Vault
func (i *VaultInjector) readSecret(ctx context.Context) (string, int64, error) {
	// Read secret
	secret, err := i.client.Logical().ReadWithContext(ctx, i.path)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read secret: %w", err)
	}

	if secret == nil {
		return "", 0, fmt.Errorf("secret not found at path: %s", i.path)
	}

	// Extract token from secret data
	var tokenValue string

	// Check if this is a KV v2 secret (has "data" wrapper)
	if data, ok := secret.Data["data"].(map[string]interface{}); ok {
		// KV v2
		if val, ok := data[i.field]; ok {
			tokenValue, ok = val.(string)
			if !ok {
				return "", 0, fmt.Errorf("field %s is not a string", i.field)
			}
		}
	} else {
		// KV v1 or other secret engine
		if val, ok := secret.Data[i.field]; ok {
			tokenValue, ok = val.(string)
			if !ok {
				return "", 0, fmt.Errorf("field %s is not a string", i.field)
			}
		}
	}

	if tokenValue == "" {
		return "", 0, fmt.Errorf("field %s not found in secret", i.field)
	}

	i.logger.Debug("vault secret read successfully",
		zap.String("path", i.path),
		zap.Int64("lease_duration", int64(secret.LeaseDuration)),
	)

	return tokenValue, int64(secret.LeaseDuration), nil
}

// Close closes the injector and clears cache
func (i *VaultInjector) Close() error {
	i.cache.Clear()
	return nil
}
