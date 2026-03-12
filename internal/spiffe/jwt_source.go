package spiffe

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/grs/klaviger/internal/observability"
	"github.com/spiffe/go-spiffe/v2/svid/jwtsvid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.uber.org/zap"
)

// JWTSource manages JWT-SVID credentials from the Workload API
type JWTSource struct {
	jwtSource  *workloadapi.JWTSource
	socketPath string
	audience   []string
	logger     *zap.Logger
	mu         sync.RWMutex
	lastFetch  time.Time
	cachedJWT  string
	cacheExpiry time.Time
}

// NewJWTSource creates a new JWT-SVID source
func NewJWTSource(ctx context.Context, socketPath string, audience []string, logger *zap.Logger) (*JWTSource, error) {
	logger.Info("Initializing SPIFFE JWT source",
		zap.String("socketPath", socketPath),
		zap.Strings("audience", audience),
	)

	// Create a timeout context for initialization (10 seconds)
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Create JWT source
	jwtSource, err := workloadapi.NewJWTSource(
		initCtx,
		workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)),
	)
	if err != nil {
		observability.SPIFFEConnectionErrors.WithLabelValues("init").Inc()
		logger.Error("Failed to create SPIFFE JWT source - SPIRE agent may not be accessible",
			zap.Error(err),
			zap.String("socketPath", socketPath),
		)
		return nil, fmt.Errorf("failed to create JWT source: %w", err)
	}

	s := &JWTSource{
		jwtSource:  jwtSource,
		socketPath: socketPath,
		audience:   audience,
		logger:     logger,
		lastFetch:  time.Now(),
	}

	logger.Info("SPIFFE JWT source initialized")

	return s, nil
}

// FetchJWTSVID fetches a JWT-SVID with the specified audience
func (s *JWTSource) FetchJWTSVID(ctx context.Context, audience []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if we have a cached JWT that's still valid
	if s.cachedJWT != "" && time.Now().Before(s.cacheExpiry) {
		s.logger.Debug("Using cached JWT-SVID",
			zap.Time("expiry", s.cacheExpiry),
		)
		return s.cachedJWT, nil
	}

	// Fetch new JWT-SVID
	s.logger.Debug("Fetching new JWT-SVID",
		zap.Strings("audience", audience),
	)

	// Build params - first audience is primary, rest are extra
	params := jwtsvid.Params{
		Audience: audience[0],
	}
	if len(audience) > 1 {
		params.ExtraAudiences = audience[1:]
	}

	jwtSVID, err := s.jwtSource.FetchJWTSVID(ctx, params)
	if err != nil {
		observability.SPIFFEJWTFetchTotal.WithLabelValues("error").Inc()
		observability.SPIFFEConnectionErrors.WithLabelValues("fetch").Inc()
		return "", fmt.Errorf("failed to fetch JWT-SVID: %w", err)
	}

	s.logger.Info("JWT-SVID fetched successfully",
		zap.String("spiffeID", jwtSVID.ID.String()),
		zap.Time("expiry", jwtSVID.Expiry),
		zap.Strings("audience", audience),
	)

	observability.SPIFFEJWTFetchTotal.WithLabelValues("success").Inc()
	s.lastFetch = time.Now()

	// Cache the JWT, but refresh before it expires (with 10% buffer)
	tokenLifetime := time.Until(jwtSVID.Expiry)
	refreshBuffer := time.Duration(float64(tokenLifetime) * 0.1)
	s.cacheExpiry = jwtSVID.Expiry.Add(-refreshBuffer)
	s.cachedJWT = jwtSVID.Marshal()

	// Update expiry metric
	expirySeconds := time.Until(jwtSVID.Expiry).Seconds()
	observability.SPIFFEJWTExpirySeconds.Set(expirySeconds)

	s.logger.Debug("JWT-SVID cached",
		zap.Time("expiry", jwtSVID.Expiry),
		zap.Time("refreshAt", s.cacheExpiry),
	)

	return s.cachedJWT, nil
}

// Close closes the SPIFFE JWT source
func (s *JWTSource) Close() error {
	s.logger.Info("Closing SPIFFE JWT source")
	if s.jwtSource != nil {
		return s.jwtSource.Close()
	}
	return nil
}
