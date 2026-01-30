package util

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"go.uber.org/zap"
)

// JWKSCache caches JWKS keys with TTL-based refresh
type JWKSCache struct {
	url             string
	bearerTokenFile string
	caFile          string
	cache           jwk.Set
	cacheMu         sync.RWMutex
	ttl             time.Duration
	lastFetch       time.Time
	logger          *zap.Logger
}

// NewJWKSCache creates a new JWKS cache
func NewJWKSCache(url string, bearerTokenFile string, caFile string, ttl time.Duration, logger *zap.Logger) *JWKSCache {
	return &JWKSCache{
		url:             url,
		bearerTokenFile: bearerTokenFile,
		caFile:          caFile,
		ttl:             ttl,
		logger:          logger,
	}
}

// Get returns the cached JWKS, fetching if necessary
func (c *JWKSCache) Get(ctx context.Context) (jwk.Set, error) {
	c.cacheMu.RLock()
	if c.cache != nil && time.Since(c.lastFetch) < c.ttl {
		defer c.cacheMu.RUnlock()
		c.logger.Debug("JWKS cache hit")
		return c.cache, nil
	}
	c.cacheMu.RUnlock()

	// Cache miss or expired, fetch new keys
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// Double-check in case another goroutine fetched while we were waiting
	if c.cache != nil && time.Since(c.lastFetch) < c.ttl {
		c.logger.Debug("JWKS cache hit (after lock)")
		return c.cache, nil
	}

	c.logger.Debug("JWKS cache miss, fetching", zap.String("url", c.url))

	// Fetch JWKS with optional bearer token and/or CA certificate
	var set jwk.Set
	var err error
	if c.bearerTokenFile != "" || c.caFile != "" {
		// Fetch with custom HTTP client (for auth or custom CA)
		httpClient, err := c.createHTTPClient()
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP client: %w", err)
		}
		set, err = jwk.Fetch(ctx, c.url, jwk.WithHTTPClient(httpClient))
	} else {
		// Fetch without authentication or custom CA
		set, err = jwk.Fetch(ctx, c.url)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	c.cache = set
	c.lastFetch = time.Now()

	c.logger.Info("JWKS fetched successfully", zap.String("url", c.url))
	return c.cache, nil
}

// createHTTPClient creates an HTTP client with optional Authorization header and CA certificate
func (c *JWKSCache) createHTTPClient() (*http.Client, error) {
	transport := &http.Transport{}

	// Configure TLS if CA file is provided
	if c.caFile != "" {
		caCert, err := os.ReadFile(c.caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from %s: %w", c.caFile, err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", c.caFile)
		}

		transport.TLSClientConfig = &tls.Config{
			RootCAs: caCertPool,
		}

		c.logger.Debug("Loaded CA certificate for JWKS", zap.String("caFile", c.caFile))
	}

	client := &http.Client{
		Transport: &bearerTokenTransport{
			Base:            transport,
			BearerTokenFile: c.bearerTokenFile,
			Logger:          c.logger,
		},
		Timeout: 10 * time.Second,
	}
	return client, nil
}

// bearerTokenTransport adds Bearer token to HTTP requests
type bearerTokenTransport struct {
	Base            http.RoundTripper
	BearerTokenFile string
	Logger          *zap.Logger
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.BearerTokenFile != "" {
		// Read token from file
		tokenBytes, err := os.ReadFile(t.BearerTokenFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read bearer token from file %s: %w", t.BearerTokenFile, err)
		}
		token := strings.TrimSpace(string(tokenBytes))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	return t.Base.RoundTrip(req)
}

// Clear clears the cache
func (c *JWKSCache) Clear() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cache = nil
	c.lastFetch = time.Time{}
	c.logger.Debug("JWKS cache cleared")
}
