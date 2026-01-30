package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/observability"
	"go.uber.org/zap"
)

// IntrospectionVerifier verifies tokens using OAuth introspection endpoint
type IntrospectionVerifier struct {
	endpoint     string
	clientID     string
	clientSecret string
	httpClient   *http.Client
	logger       *zap.Logger
}

// introspectionResponse represents the OAuth 2.0 introspection response
type introspectionResponse struct {
	Active    bool     `json:"active"`
	Scope     string   `json:"scope,omitempty"`
	ClientID  string   `json:"client_id,omitempty"`
	Username  string   `json:"username,omitempty"`
	TokenType string   `json:"token_type,omitempty"`
	Exp       int64    `json:"exp,omitempty"`
	Iat       int64    `json:"iat,omitempty"`
	Nbf       int64    `json:"nbf,omitempty"`
	Sub       string   `json:"sub,omitempty"`
	Aud       string   `json:"aud,omitempty"`
	Iss       string   `json:"iss,omitempty"`
	Jti       string   `json:"jti,omitempty"`
}

// NewIntrospectionVerifier creates a new introspection verifier
func NewIntrospectionVerifier(cfg *config.IntrospectionConfig, logger *zap.Logger) (*IntrospectionVerifier, error) {
	return &IntrospectionVerifier{
		endpoint:     cfg.Endpoint,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger.With(zap.String("component", "introspection_verifier")),
	}, nil
}

// Verify verifies a token using introspection
func (v *IntrospectionVerifier) Verify(ctx context.Context, token string) (*Claims, error) {
	start := time.Now()
	result := "success"
	defer func() {
		observability.TokenVerificationDuration.WithLabelValues("introspection", result).Observe(time.Since(start).Seconds())
	}()

	// Prepare introspection request
	data := url.Values{}
	data.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, "POST", v.endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		result = "error"
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set basic auth with client credentials
	req.SetBasicAuth(v.clientID, v.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	resp, err := v.httpClient.Do(req)
	if err != nil {
		result = "error"
		v.logger.Debug("introspection request failed", zap.Error(err))
		return nil, fmt.Errorf("introspection request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		result = "error"
		return nil, fmt.Errorf("introspection endpoint returned status %d", resp.StatusCode)
	}

	// Parse response
	var introspectionResp introspectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&introspectionResp); err != nil {
		result = "error"
		return nil, fmt.Errorf("failed to decode introspection response: %w", err)
	}

	// Check if token is active
	if !introspectionResp.Active {
		result = "inactive"
		return nil, fmt.Errorf("token is not active")
	}

	// Build claims from introspection response
	claims := &Claims{
		Subject:   introspectionResp.Sub,
		Issuer:    introspectionResp.Iss,
		ExpiresAt: introspectionResp.Exp,
		IssuedAt:  introspectionResp.Iat,
		NotBefore: introspectionResp.Nbf,
		Extra:     make(map[string]interface{}),
	}

	// Parse audience (can be space-separated or comma-separated)
	if introspectionResp.Aud != "" {
		auds := strings.Fields(introspectionResp.Aud)
		if len(auds) == 0 {
			auds = strings.Split(introspectionResp.Aud, ",")
		}
		for i := range auds {
			auds[i] = strings.TrimSpace(auds[i])
		}
		claims.Audience = auds
	}

	// Parse scopes (space-separated)
	if introspectionResp.Scope != "" {
		claims.Scopes = strings.Fields(introspectionResp.Scope)
	}

	// Add additional fields to Extra
	if introspectionResp.Username != "" {
		claims.Extra["username"] = introspectionResp.Username
	}
	if introspectionResp.ClientID != "" {
		claims.Extra["client_id"] = introspectionResp.ClientID
	}
	if introspectionResp.TokenType != "" {
		claims.Extra["token_type"] = introspectionResp.TokenType
	}

	v.logger.Debug("token introspection successful",
		zap.String("subject", claims.Subject),
		zap.Strings("scopes", claims.Scopes),
	)

	return claims, nil
}
