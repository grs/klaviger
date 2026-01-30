package util

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// CachedToken represents a cached token with expiration
type CachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// TokenCache caches tokens with TTL-based expiration
type TokenCache struct {
	cache   map[string]*CachedToken
	cacheMu sync.RWMutex
	logger  *zap.Logger
}

// NewTokenCache creates a new token cache
func NewTokenCache(logger *zap.Logger) *TokenCache {
	cache := &TokenCache{
		cache:  make(map[string]*CachedToken),
		logger: logger,
	}

	// Start cleanup goroutine
	go cache.cleanupExpired()

	return cache
}

// Get retrieves a token from cache if valid
func (c *TokenCache) Get(key string) (string, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	cached, ok := c.cache[key]
	if !ok {
		c.logger.Debug("token cache miss", zap.String("key", key))
		return "", false
	}

	// Check if expired
	if time.Now().After(cached.ExpiresAt) {
		c.logger.Debug("token cache expired", zap.String("key", key))
		return "", false
	}

	c.logger.Debug("token cache hit", zap.String("key", key))
	return cached.Token, true
}

// Set stores a token in cache with TTL
func (c *TokenCache) Set(key, token string, ttl time.Duration) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.cache[key] = &CachedToken{
		Token:     token,
		ExpiresAt: time.Now().Add(ttl),
	}

	c.logger.Debug("token cached",
		zap.String("key", key),
		zap.Duration("ttl", ttl),
	)
}

// Delete removes a token from cache
func (c *TokenCache) Delete(key string) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	delete(c.cache, key)
	c.logger.Debug("token removed from cache", zap.String("key", key))
}

// Clear removes all tokens from cache
func (c *TokenCache) Clear() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.cache = make(map[string]*CachedToken)
	c.logger.Debug("token cache cleared")
}

// cleanupExpired periodically removes expired tokens
func (c *TokenCache) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cacheMu.Lock()
		now := time.Now()
		for key, cached := range c.cache {
			if now.After(cached.ExpiresAt) {
				delete(c.cache, key)
				c.logger.Debug("expired token removed", zap.String("key", key))
			}
		}
		c.cacheMu.Unlock()
	}
}
