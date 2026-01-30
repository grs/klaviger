package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Reverse proxy metrics
	ReverseProxyRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "klaviger_reverse_proxy_requests_total",
			Help: "Total number of reverse proxy requests",
		},
		[]string{"status"},
	)

	ReverseProxyRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "klaviger_reverse_proxy_request_duration_seconds",
			Help:    "Reverse proxy request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"status"},
	)

	TokenVerificationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "klaviger_token_verification_duration_seconds",
			Help:    "Token verification duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"mode", "result"},
	)

	// Forward proxy metrics
	ForwardProxyRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "klaviger_forward_proxy_requests_total",
			Help: "Total number of forward proxy requests",
		},
		[]string{"host_pattern", "injection_mode", "status"},
	)

	ForwardProxyRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "klaviger_forward_proxy_request_duration_seconds",
			Help:    "Forward proxy request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"host_pattern", "status"},
	)

	TokenInjectionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "klaviger_token_injection_duration_seconds",
			Help:    "Token injection duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"mode", "result"},
	)

	TokenCacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "klaviger_token_cache_hits_total",
			Help: "Total number of token cache hits",
		},
		[]string{"mode", "hit"},
	)

	JWKSCacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "klaviger_jwks_cache_hits_total",
			Help: "Total number of JWKS cache hits",
		},
		[]string{"hit"},
	)

	// SPIFFE metrics
	SPIFFECertificateRotations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "klaviger_spiffe_certificate_rotations_total",
			Help: "Total number of SPIFFE certificate rotations",
		},
		[]string{"type"}, // "server" or "client"
	)

	SPIFFECertificateExpirySeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "klaviger_spiffe_certificate_expiry_seconds",
			Help: "Seconds until SPIFFE certificate expires",
		},
		[]string{"type"},
	)

	SPIFFEConnectionErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "klaviger_spiffe_connection_errors_total",
			Help: "Total number of SPIFFE Workload API connection errors",
		},
		[]string{"operation"}, // "init", "update", "fetch"
	)

	SPIFFEJWTFetchTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "klaviger_spiffe_jwt_fetch_total",
			Help: "Total number of JWT-SVID fetch operations",
		},
		[]string{"status"}, // "success" or "error"
	)

	SPIFFEJWTExpirySeconds = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "klaviger_spiffe_jwt_expiry_seconds",
			Help: "Seconds until JWT-SVID expires",
		},
	)
)
