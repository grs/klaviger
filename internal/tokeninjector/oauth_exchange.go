package tokeninjector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	tokenURL         string
	audience         string
	scope            string
	cacheTTL         time.Duration
	cache            *util.TokenCache
	httpClient       *http.Client
	logger           *zap.Logger
	clientAuthMethod string // "header" or "assertion"
	includeActorToken bool   // Send auth token as actor_token per RFC 8693

	// SPIFFE JWT-SVID source for authentication (optional)
	jwtSource    *spiffe.JWTSource
	jwtAudience  []string // Audience for JWT-SVID requests

	// Client token for authentication (fallback when SPIFFE not enabled)
	k8sTokenPath string // Path to client token file
	k8sToken     string // Cached client token
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
	// Set k8s token path from config or use default
	k8sTokenPath := cfg.ClientTokenPath
	if k8sTokenPath == "" {
		k8sTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	// Set client auth method from config or use default
	clientAuthMethod := cfg.ClientAuthMethod
	if clientAuthMethod == "" {
		clientAuthMethod = "header"
	}

	// Determine if actor token should be included (default: true)
	includeActorToken := true
	if cfg.IncludeActorToken != nil {
		includeActorToken = *cfg.IncludeActorToken
	}

	injector := &OAuthInjector{
		tokenURL:          cfg.TokenURL,
		audience:          cfg.Audience,
		scope:             cfg.Scope,
		cacheTTL:          time.Duration(cfg.CacheTTL),
		cache:             util.NewTokenCache(logger.With(zap.String("component", "oauth_token_cache"))),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:            logger.With(zap.String("component", "oauth_injector")),
		k8sTokenPath:      k8sTokenPath,
		clientAuthMethod:  clientAuthMethod,
		includeActorToken: includeActorToken,
	}

	// Check if SPIFFE is enabled at server level
	if serverCfg.SPIFFE != nil && serverCfg.SPIFFE.Enabled {
		// Determine JWT-SVID audience
		jwtAudience := serverCfg.SPIFFE.JWTAudience
		if len(jwtAudience) == 0 {
			// Fall back to token URL if no JWT audience configured
			jwtAudience = []string{cfg.TokenURL}
		}

		// Use JWT-SVID for authentication
		logger.Info("Configuring OAuth injector to use JWT-SVID for authentication",
			zap.String("socketPath", serverCfg.SPIFFE.SocketPath),
			zap.String("clientAuthMethod", clientAuthMethod),
			zap.Strings("jwtAudience", jwtAudience),
			zap.Bool("includeActorToken", includeActorToken),
		)

		ctx := context.Background()
		jwtSource, err := spiffe.NewJWTSource(
			ctx,
			serverCfg.SPIFFE.SocketPath,
			jwtAudience,
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create JWT-SVID source for OAuth authentication: %w", err)
		}
		injector.jwtSource = jwtSource
		injector.jwtAudience = jwtAudience
	} else {
		// Use client token file for authentication
		logger.Info("Configuring OAuth injector to use client token for authentication",
			zap.String("tokenPath", k8sTokenPath),
			zap.String("clientAuthMethod", clientAuthMethod),
			zap.Bool("includeActorToken", includeActorToken),
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
		token, err := i.jwtSource.FetchJWTSVID(ctx, i.jwtAudience)
		if err != nil {
			return "", fmt.Errorf("failed to fetch JWT-SVID for OAuth authentication: %w", err)
		}
		return token, nil
	}

	// Otherwise, use client token from file
	return i.getK8sToken()
}

// getK8sToken reads the client authentication token from file
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
		return "", fmt.Errorf("failed to read client authentication token from %s: %w", i.k8sTokenPath, err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", fmt.Errorf("client authentication token is empty")
	}

	// Cache the token
	i.k8sTokenMu.Lock()
	i.k8sToken = token
	i.k8sTokenTime = time.Now()
	i.k8sTokenMu.Unlock()

	i.logger.Debug("Loaded client authentication token",
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

	// Include actor token per RFC 8693 if enabled
	if i.includeActorToken {
		data.Set("actor_token", authToken)
		data.Set("actor_token_type", "urn:ietf:params:oauth:token-type:jwt")
	}

	// Add client authentication based on configured method
	if i.clientAuthMethod == "assertion" {
		// Use client_assertion in request body
		data.Set("client_assertion", authToken)

		// Set client_assertion_type based on whether SPIFFE is enabled
		if i.jwtSource != nil {
			data.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-spiffe")
		} else {
			data.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
		}
	}

	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", i.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Set Authorization header if using header-based authentication
	if i.clientAuthMethod == "header" {
		httpReq.Header.Set("Authorization", "Bearer "+authToken)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	resp, err := i.httpClient.Do(httpReq)
	if err != nil {
		return "", 0, fmt.Errorf("exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Read response body for error details
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			i.logger.Error("exchange endpoint returned error, failed to read response body",
				zap.Int("status", resp.StatusCode),
				zap.Error(err),
			)
			return "", 0, fmt.Errorf("exchange endpoint returned status %d", resp.StatusCode)
		}

		// Try to parse as OAuth error response
		var oauthErr struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
			ErrorURI         string `json:"error_uri,omitempty"`
		}

		bodyStr := string(bodyBytes)
		if json.Unmarshal(bodyBytes, &oauthErr) == nil && oauthErr.Error != "" {
			// Successfully parsed OAuth error response
			i.logger.Error("token exchange failed with OAuth error",
				zap.Int("status", resp.StatusCode),
				zap.String("error", oauthErr.Error),
				zap.String("error_description", oauthErr.ErrorDescription),
				zap.String("error_uri", oauthErr.ErrorURI),
			)
			if oauthErr.ErrorDescription != "" {
				return "", 0, fmt.Errorf("exchange endpoint returned status %d: %s - %s",
					resp.StatusCode, oauthErr.Error, oauthErr.ErrorDescription)
			}
			return "", 0, fmt.Errorf("exchange endpoint returned status %d: %s",
				resp.StatusCode, oauthErr.Error)
		}

		// Not a standard OAuth error, log raw response
		i.logger.Error("token exchange failed",
			zap.Int("status", resp.StatusCode),
			zap.String("response_body", bodyStr),
		)
		return "", 0, fmt.Errorf("exchange endpoint returned status %d: %s",
			resp.StatusCode, bodyStr)
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
