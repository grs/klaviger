package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/observability"
	"github.com/grs/klaviger/internal/util"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// JWTVerifier verifies JWT tokens using JWKS
type JWTVerifier struct {
	issuer         string
	audience       string
	requiredScopes []string
	jwksCache      *util.JWKSCache
	logger         *zap.Logger
}

// NewJWTVerifier creates a new JWT verifier
func NewJWTVerifier(cfg *config.JWTConfig, logger *zap.Logger) (*JWTVerifier, error) {
	cache := util.NewJWKSCache(
		cfg.JWKSUrl,
		cfg.BearerTokenFile,
		cfg.CAFile,
		time.Duration(cfg.CacheTTL),
		logger.With(zap.String("component", "jwks_cache")),
	)

	return &JWTVerifier{
		issuer:         cfg.Issuer,
		audience:       cfg.Audience,
		requiredScopes: cfg.RequiredScopes,
		jwksCache:      cache,
		logger:         logger.With(zap.String("component", "jwt_verifier")),
	}, nil
}

// Verify verifies a JWT token
func (v *JWTVerifier) Verify(ctx context.Context, tokenString string) (*Claims, error) {
	// Start tracing span
	tracer := otel.Tracer("auth")
	ctx, span := tracer.Start(ctx, "jwt-verify")
	defer span.End()

	span.SetAttributes(
		attribute.String("auth.method", "jwt"),
		attribute.String("auth.issuer", v.issuer),
	)

	start := time.Now()
	result := "success"
	defer func() {
		observability.TokenVerificationDuration.WithLabelValues("jwt", result).Observe(time.Since(start).Seconds())
	}()

	// Parse token without verification first to get the key ID
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Get JWKS
		keySet, err := v.jwksCache.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get JWKS: %w", err)
		}

		// Get key ID from token header
		kidInterface, ok := token.Header["kid"]
		if !ok {
			return nil, fmt.Errorf("token does not have kid header")
		}

		kid, ok := kidInterface.(string)
		if !ok {
			return nil, fmt.Errorf("kid header is not a string")
		}

		// Look up key in JWKS
		key, ok := keySet.LookupKeyID(kid)
		if !ok {
			return nil, fmt.Errorf("key with kid %s not found in JWKS", kid)
		}

		// Convert JWK to crypto public key
		var pubKey interface{}
		if err := key.Raw(&pubKey); err != nil {
			return nil, fmt.Errorf("failed to get public key: %w", err)
		}

		return pubKey, nil
	})

	if err != nil {
		result = "error"
		v.logger.Debug("token verification failed", zap.Error(err))
		return nil, fmt.Errorf("failed to verify token: %w", err)
	}

	if !token.Valid {
		result = "invalid"
		return nil, fmt.Errorf("token is not valid")
	}

	// Extract claims
	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		result = "error"
		return nil, fmt.Errorf("failed to extract claims")
	}

	claims := &Claims{
		Extra: make(map[string]interface{}),
	}

	// Extract standard claims
	if sub, ok := mapClaims["sub"].(string); ok {
		claims.Subject = sub
	}

	if iss, ok := mapClaims["iss"].(string); ok {
		claims.Issuer = iss
	}

	// Extract audience (can be string or array)
	if aud, ok := mapClaims["aud"].(string); ok {
		claims.Audience = []string{aud}
	} else if auds, ok := mapClaims["aud"].([]interface{}); ok {
		for _, a := range auds {
			if audStr, ok := a.(string); ok {
				claims.Audience = append(claims.Audience, audStr)
			}
		}
	}

	// Extract time claims
	if exp, ok := mapClaims["exp"].(float64); ok {
		claims.ExpiresAt = int64(exp)
	}

	if iat, ok := mapClaims["iat"].(float64); ok {
		claims.IssuedAt = int64(iat)
	}

	if nbf, ok := mapClaims["nbf"].(float64); ok {
		claims.NotBefore = int64(nbf)
	}

	// Extract scopes (can be space-separated string or array)
	if scope, ok := mapClaims["scope"].(string); ok {
		claims.Scopes = strings.Split(scope, " ")
	} else if scopes, ok := mapClaims["scopes"].([]interface{}); ok {
		for _, s := range scopes {
			if scopeStr, ok := s.(string); ok {
				claims.Scopes = append(claims.Scopes, scopeStr)
			}
		}
	}

	// Store all claims in Extra
	for k, v := range mapClaims {
		claims.Extra[k] = v
	}

	// Validate issuer
	if claims.Issuer != v.issuer {
		result = "invalid_issuer"
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", v.issuer, claims.Issuer)
	}

	// Validate audience
	validAudience := false
	for _, aud := range claims.Audience {
		if aud == v.audience {
			validAudience = true
			break
		}
	}
	if !validAudience {
		result = "invalid_audience"
		return nil, fmt.Errorf("invalid audience: expected %s, got %v", v.audience, claims.Audience)
	}

	// Validate required scopes
	if len(v.requiredScopes) > 0 {
		scopeMap := make(map[string]bool)
		for _, scope := range claims.Scopes {
			scopeMap[scope] = true
		}

		for _, requiredScope := range v.requiredScopes {
			if !scopeMap[requiredScope] {
				result = "missing_scope"
				return nil, fmt.Errorf("missing required scope: %s", requiredScope)
			}
		}
	}

	v.logger.Debug("token verified successfully",
		zap.String("subject", claims.Subject),
		zap.Strings("scopes", claims.Scopes),
	)

	return claims, nil
}
