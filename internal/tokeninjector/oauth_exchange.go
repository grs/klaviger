package tokeninjector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/observability"
	"github.com/grs/klaviger/internal/spiffe"
	"github.com/grs/klaviger/internal/util"
	"go.uber.org/zap"
)

// OAuthInjector injects tokens using OAuth token exchange (RFC 8693)
type OAuthInjector struct {
	tokenURL   string
	audience   string
	scope      string
	cacheTTL   time.Duration
	cache      *util.TokenCache
	httpClient *http.Client
	logger     *zap.Logger

	// SPIFFE JWT-SVID source for authentication (optional)
	jwtSource *spiffe.JWTSource

	// Kubernetes service account token for authentication (fallback)
	k8sTokenPath string
	k8sToken     string
	k8sTokenMu   sync.RWMutex
	k8sTokenTime time.Time
}

// tokenExchangeResponse represents OAuth 2.0 token exchange response
type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	IssuedType   string `json:"issued_token_type"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// NewOAuthInjector creates a new OAuth injector
func NewOAuthInjector(cfg *config.OAuthConfig, serverCfg *config.ServerConfig, logger *zap.Logger) (*OAuthInjector, error) {
	injector := &OAuthInjector{
		tokenURL: cfg.TokenURL,
		audience: cfg.Audience,
		scope:    cfg.Scope,
		cacheTTL: time.Duration(cfg.CacheTTL),
		cache:    util.NewTokenCache(logger.With(zap.String("component", "oauth_token_cache"))),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:       logger.With(zap.String("component", "oauth_injector")),
		k8sTokenPath: "/var/run/secrets/kubernetes.io/serviceaccount/token",
	}

	// Check if SPIFFE is enabled for client TLS
	if serverCfg.ClientTLS.Enabled && serverCfg.ClientTLS.SPIFFE != nil && serverCfg.ClientTLS.SPIFFE.Enabled {
		// Use JWT-SVID for authentication
		logger.Info("Configuring OAuth injector to use JWT-SVID for authentication",
			zap.String("socketPath", serverCfg.ClientTLS.SPIFFE.SocketPath),
		)

		ctx := context.Background()
		jwtSource, err := spiffe.NewJWTSource(
			ctx,
			serverCfg.ClientTLS.SPIFFE.SocketPath,
			[]string{cfg.TokenURL}, // Use token URL as audience
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create JWT-SVID source for OAuth authentication: %w", err)
		}
		injector.jwtSource = jwtSource
	} else {
		// Use Kubernetes service account token for authentication
		logger.Info("Configuring OAuth injector to use Kubernetes service account token for authentication",
			zap.String("tokenPath", injector.k8sTokenPath),
		)
	}

	return injector, nil
}

// Inject injects a token using OAuth exchange
func (i *OAuthInjector) Inject(ctx context.Context, req *http.Request) error {
	start := time.Now()
	result := "success"
	defer func() {
		observability.TokenInjectionDuration.WithLabelValues("oauth", result).Observe(time.Since(start).Seconds())
	}()

	// Extract subject token from incoming request
	subjectToken := extractToken(req)
	if subjectToken == "" {
		result = "no_subject_token"
		i.logger.Debug("no subject token found for exchange")
		// If no subject token, proceed without injection
		return nil
	}

	// Check cache first
	cacheKey := i.getCacheKey(subjectToken)
	if cachedToken, ok := i.cache.Get(cacheKey); ok {
		req.Header.Set("Authorization", "Bearer "+cachedToken)
		observability.TokenCacheHits.WithLabelValues("oauth", "hit").Inc()
		i.logger.Debug("using cached exchanged token", zap.String("host", req.Host))
		return nil
	}

	observability.TokenCacheHits.WithLabelValues("oauth", "miss").Inc()

	// Perform token exchange
	exchangedToken, expiresIn, err := i.exchangeToken(ctx, subjectToken)
	if err != nil {
		result = "exchange_failed"
		i.logger.Error("token exchange failed", zap.Error(err))
		return fmt.Errorf("token exchange failed: %w", err)
	}

	// Cache the exchanged token
	ttl := i.cacheTTL
	if expiresIn > 0 {
		// Use token expiration if available, but cap at configured TTL
		tokenTTL := time.Duration(expiresIn) * time.Second
		if tokenTTL < ttl {
			ttl = tokenTTL
		}
	}
	i.cache.Set(cacheKey, exchangedToken, ttl)

	// Set Authorization header
	req.Header.Set("Authorization", "Bearer "+exchangedToken)

	i.logger.Debug("token exchanged and injected",
		zap.String("host", req.Host),
		zap.Duration("ttl", ttl),
	)

	return nil
}

// getAuthToken returns the authentication token for OAuth requests
func (i *OAuthInjector) getAuthToken(ctx context.Context) (string, error) {
	// If SPIFFE JWT-SVID source is configured, use it
	if i.jwtSource != nil {
		token, err := i.jwtSource.FetchJWTSVID(ctx, []string{i.tokenURL})
		if err != nil {
			return "", fmt.Errorf("failed to fetch JWT-SVID for OAuth authentication: %w", err)
		}
		return token, nil
	}

	// Otherwise, use Kubernetes service account token
	return i.getK8sToken()
}

// getK8sToken reads the Kubernetes service account token
func (i *OAuthInjector) getK8sToken() (string, error) {
	// Check if we have a cached token (refresh every 5 minutes)
	i.k8sTokenMu.RLock()
	if i.k8sToken != "" && time.Since(i.k8sTokenTime) < 5*time.Minute {
		token := i.k8sToken
		i.k8sTokenMu.RUnlock()
		return token, nil
	}
	i.k8sTokenMu.RUnlock()

	// Read token from file
	tokenBytes, err := os.ReadFile(i.k8sTokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read Kubernetes service account token: %w", err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", fmt.Errorf("Kubernetes service account token is empty")
	}

	// Cache the token
	i.k8sTokenMu.Lock()
	i.k8sToken = token
	i.k8sTokenTime = time.Now()
	i.k8sTokenMu.Unlock()

	i.logger.Debug("Loaded Kubernetes service account token",
		zap.String("tokenPath", i.k8sTokenPath),
	)

	return token, nil
}

// exchangeToken performs RFC 8693 token exchange
func (i *OAuthInjector) exchangeToken(ctx context.Context, subjectToken string) (string, int64, error) {
	// Get authentication token (JWT-SVID or K8s SA token)
	authToken, err := i.getAuthToken(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get authentication token: %w", err)
	}

	// Prepare token exchange request (RFC 8693)
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	data.Set("subject_token", subjectToken)
	data.Set("subject_token_type", "urn:ietf:params:oauth:token-type:access_token")

	if i.audience != "" {
		data.Set("audience", i.audience)
	}

	if i.scope != "" {
		data.Set("scope", i.scope)
	}

	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", i.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Use bearer token authentication instead of basic auth
	httpReq.Header.Set("Authorization", "Bearer "+authToken)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	resp, err := i.httpClient.Do(httpReq)
	if err != nil {
		return "", 0, fmt.Errorf("exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("exchange endpoint returned status %d", resp.StatusCode)
	}

	// Parse response
	var tokenResp tokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("failed to decode exchange response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("no access token in exchange response")
	}

	i.logger.Debug("token exchange successful",
		zap.String("token_type", tokenResp.TokenType),
		zap.Int64("expires_in", tokenResp.ExpiresIn),
	)

	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}

// getCacheKey generates a cache key from the subject token
func (i *OAuthInjector) getCacheKey(subjectToken string) string {
	// Use first 16 characters of token as key (for privacy)
	if len(subjectToken) > 16 {
		return subjectToken[:16]
	}
	return subjectToken
}

// extractToken extracts the bearer token from request
func extractToken(req *http.Request) string {
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return ""
	}

	return parts[1]
}

// Close closes the injector and clears cache
func (i *OAuthInjector) Close() error {
	i.cache.Clear()
	if i.jwtSource != nil {
		return i.jwtSource.Close()
	}
	return nil
}
