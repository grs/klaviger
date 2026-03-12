package spiffe

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/grs/klaviger/internal/observability"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.uber.org/zap"
)

// Source manages X.509 SPIFFE credentials from the Workload API
type Source struct {
	x509Source *workloadapi.X509Source
	socketPath string
	certType   string // "server" or "client"
	logger     *zap.Logger
	mu         sync.RWMutex
	lastUpdate time.Time
}

// NewSource creates a new X.509 SPIFFE source
func NewSource(ctx context.Context, socketPath string, certType string, logger *zap.Logger) (*Source, error) {
	logger.Info("Initializing SPIFFE X.509 source",
		zap.String("socketPath", socketPath),
		zap.String("certType", certType),
	)

	s := &Source{
		socketPath: socketPath,
		certType:   certType,
		logger:     logger,
		lastUpdate: time.Now(),
	}

	// Create X.509 source with callback for updates
	x509Source, err := workloadapi.NewX509Source(
		ctx,
		workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)),
	)
	if err != nil {
		observability.SPIFFEConnectionErrors.WithLabelValues("init").Inc()
		return nil, fmt.Errorf("failed to create X.509 source: %w", err)
	}

	s.x509Source = x509Source

	// Log initial SVID information
	svid, err := x509Source.GetX509SVID()
	if err != nil {
		observability.SPIFFEConnectionErrors.WithLabelValues("init").Inc()
		x509Source.Close()
		return nil, fmt.Errorf("failed to get initial X.509 SVID: %w", err)
	}

	logger.Info("SPIFFE X.509 source initialized",
		zap.String("spiffeID", svid.ID.String()),
		zap.Time("expiry", svid.Certificates[0].NotAfter),
		zap.String("certType", certType),
	)

	// Update metrics
	expirySeconds := time.Until(svid.Certificates[0].NotAfter).Seconds()
	observability.SPIFFECertificateExpirySeconds.WithLabelValues(certType).Set(expirySeconds)

	// Start background update monitoring
	go s.monitorUpdates(ctx)

	return s, nil
}

// monitorUpdates monitors for certificate updates
func (s *Source) monitorUpdates(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			svid, err := s.x509Source.GetX509SVID()
			if err != nil {
				s.logger.Error("Failed to get X.509 SVID during monitoring",
					zap.Error(err),
					zap.String("certType", s.certType),
				)
				continue
			}

			// Check if certificate has been updated
			s.mu.Lock()
			lastUpdate := s.lastUpdate
			s.mu.Unlock()

			certNotBefore := svid.Certificates[0].NotBefore
			if certNotBefore.After(lastUpdate) {
				s.onUpdate(svid.ID, svid.Certificates[0].NotAfter)
			}

			// Update expiry metric
			expirySeconds := time.Until(svid.Certificates[0].NotAfter).Seconds()
			observability.SPIFFECertificateExpirySeconds.WithLabelValues(s.certType).Set(expirySeconds)
		}
	}
}

// onUpdate handles certificate rotation events
func (s *Source) onUpdate(id spiffeid.ID, expiry time.Time) {
	s.mu.Lock()
	s.lastUpdate = time.Now()
	s.mu.Unlock()

	s.logger.Info("SPIFFE certificate rotated",
		zap.String("spiffeID", id.String()),
		zap.Time("expiry", expiry),
		zap.String("certType", s.certType),
	)

	observability.SPIFFECertificateRotations.WithLabelValues(s.certType).Inc()

	// Update expiry metric
	expirySeconds := time.Until(expiry).Seconds()
	observability.SPIFFECertificateExpirySeconds.WithLabelValues(s.certType).Set(expirySeconds)
}

// GetServerTLSConfig returns a TLS config for server-side use
func (s *Source) GetServerTLSConfig(trustDomain string, acceptedIDs []string) (*tls.Config, error) {
	s.logger.Debug("Creating server TLS config",
		zap.String("trustDomain", trustDomain),
		zap.Strings("acceptedIDs", acceptedIDs),
	)

	var authorizer tlsconfig.Authorizer

	if len(acceptedIDs) > 0 {
		// Create authorizer for specific SPIFFE IDs
		ids := make([]spiffeid.ID, 0, len(acceptedIDs))
		for _, idStr := range acceptedIDs {
			id, err := spiffeid.FromString(idStr)
			if err != nil {
				return nil, fmt.Errorf("invalid SPIFFE ID %s: %w", idStr, err)
			}
			ids = append(ids, id)
		}
		authorizer = tlsconfig.AuthorizeOneOf(ids...)
	} else if trustDomain != "" {
		// Authorize any ID from the trust domain
		td, err := spiffeid.TrustDomainFromString(trustDomain)
		if err != nil {
			return nil, fmt.Errorf("invalid trust domain %s: %w", trustDomain, err)
		}
		authorizer = tlsconfig.AuthorizeMemberOf(td)
	} else {
		// No authorization - accept any valid SPIFFE ID
		authorizer = tlsconfig.AuthorizeAny()
	}

	tlsConfig := tlsconfig.MTLSServerConfig(s.x509Source, s.x509Source, authorizer)
	return tlsConfig, nil
}

// GetClientTLSConfig returns a TLS config for client-side use
func (s *Source) GetClientTLSConfig() (*tls.Config, error) {
	s.logger.Debug("Creating client TLS config")

	// For client-side, we authorize any valid SPIFFE ID from the trust bundle
	tlsConfig := tlsconfig.MTLSClientConfig(s.x509Source, s.x509Source, tlsconfig.AuthorizeAny())
	return tlsConfig, nil
}

// Close closes the SPIFFE source
func (s *Source) Close() error {
	s.logger.Info("Closing SPIFFE X.509 source", zap.String("certType", s.certType))
	if s.x509Source != nil {
		return s.x509Source.Close()
	}
	return nil
}
