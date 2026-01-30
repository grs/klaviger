package forwardproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/grs/klaviger/internal/config"
	"github.com/grs/klaviger/internal/observability"
	"github.com/grs/klaviger/internal/spiffe"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

// Proxy is the forward proxy
type Proxy struct {
	router       *Router
	client       *http.Client
	spiffeSource *spiffe.Source
	logger       *zap.Logger
}

// New creates a new forward proxy
func New(cfg *config.ForwardProxyConfig, serverCfg *config.ServerConfig, logger *zap.Logger) (*Proxy, error) {
	router, err := NewRouter(cfg, serverCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	// Create HTTP transport
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	var spiffeSource *spiffe.Source

	// Configure client TLS if enabled
	if serverCfg.ClientTLS.Enabled {
		if serverCfg.ClientTLS.SPIFFE != nil && serverCfg.ClientTLS.SPIFFE.Enabled {
			// Use SPIFFE for client TLS
			ctx := context.Background()
			src, err := spiffe.NewSource(ctx, serverCfg.ClientTLS.SPIFFE.SocketPath, "client", logger)
			if err != nil {
				return nil, fmt.Errorf("failed to create SPIFFE source for client TLS: %w", err)
			}
			spiffeSource = src

			// Get client TLS config from SPIFFE source
			tlsConfig, err := src.GetClientTLSConfig()
			if err != nil {
				src.Close()
				return nil, fmt.Errorf("failed to get client TLS config from SPIFFE: %w", err)
			}
			transport.TLSClientConfig = tlsConfig
		} else {
			// Use file-based client TLS
			tlsConfig := &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: serverCfg.ClientTLS.InsecureSkipVerify,
			}

			// Load client certificate if provided
			if serverCfg.ClientTLS.CertFile != "" && serverCfg.ClientTLS.KeyFile != "" {
				cert, err := tls.LoadX509KeyPair(serverCfg.ClientTLS.CertFile, serverCfg.ClientTLS.KeyFile)
				if err != nil {
					return nil, fmt.Errorf("failed to load client certificate: %w", err)
				}
				tlsConfig.Certificates = []tls.Certificate{cert}
			}

			// Load CA certificate if provided
			if serverCfg.ClientTLS.CAFile != "" {
				caCert, err := os.ReadFile(serverCfg.ClientTLS.CAFile)
				if err != nil {
					return nil, fmt.Errorf("failed to read CA certificate: %w", err)
				}
				caCertPool := x509.NewCertPool()
				if !caCertPool.AppendCertsFromPEM(caCert) {
					return nil, fmt.Errorf("failed to parse CA certificate")
				}
				tlsConfig.RootCAs = caCertPool
			}

			transport.TLSClientConfig = tlsConfig
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		// Don't follow redirects automatically
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Proxy{
		router:       router,
		client:       client,
		spiffeSource: spiffeSource,
		logger:       logger.With(zap.String("component", "forward_proxy")),
	}, nil
}

// Handler returns the HTTP handler
func (p *Proxy) Handler() http.Handler {
	return http.HandlerFunc(p.handleRequest)
}

// handleRequest handles both HTTP and HTTPS (CONNECT) requests
func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method == http.MethodConnect {
		p.handleConnect(w, r, start)
	} else {
		p.handleHTTP(w, r, start)
	}
}

// handleHTTP handles regular HTTP proxy requests
func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request, start time.Time) {
	// Start tracing span
	tracer := otel.Tracer("forward-proxy")
	ctx, span := tracer.Start(r.Context(), "forward-proxy-http")
	defer span.End()

	span.SetAttributes(
		attribute.String("http.method", r.Method),
		attribute.String("http.host", r.Host),
		attribute.String("http.url", r.URL.String()),
	)

	status := "200"
	defer func() {
		duration := time.Since(start)
		observability.ForwardProxyRequestsTotal.WithLabelValues("default", "http", status).Inc()
		observability.ForwardProxyRequestDuration.WithLabelValues("default", status).Observe(duration.Seconds())

		// Add status to span
		span.SetAttributes(attribute.String("http.status", status))
	}()

	// Get injector for this host
	injector, mode := p.router.GetInjector(r.Host)
	span.SetAttributes(attribute.String("injection.mode", mode))

	// Inject token (pass traced context)
	if err := injector.Inject(ctx, r); err != nil {
		span.SetStatus(codes.Error, "token injection failed")
		span.RecordError(err)
		p.logger.Error("failed to inject token",
			zap.Error(err),
			zap.String("host", r.Host),
		)
		status = "500"
		http.Error(w, "Failed to inject token", http.StatusInternalServerError)
		return
	}

	// Remove hop-by-hop headers
	removeHopByHopHeaders(r.Header)

	// Clear RequestURI - it's set by the server but must be empty for client requests
	r.RequestURI = ""

	// Make the request
	resp, err := p.client.Do(r)
	if err != nil {
		p.logger.Error("failed to forward request",
			zap.Error(err),
			zap.String("host", r.Host),
		)
		status = "502"
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	status = fmt.Sprintf("%d", resp.StatusCode)
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)

	p.logger.Debug("request forwarded",
		zap.String("host", r.Host),
		zap.String("method", r.Method),
		zap.String("mode", mode),
		zap.Int("status", resp.StatusCode),
	)
}

// handleConnect handles HTTPS CONNECT requests for tunneling
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request, start time.Time) {
	status := "200"
	defer func() {
		duration := time.Since(start)
		observability.ForwardProxyRequestsTotal.WithLabelValues("default", "connect", status).Inc()
		observability.ForwardProxyRequestDuration.WithLabelValues("default", status).Observe(duration.Seconds())
	}()

	p.logger.Debug("CONNECT request",
		zap.String("host", r.Host),
	)

	// Establish connection to the destination
	destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		p.logger.Error("failed to connect to destination",
			zap.Error(err),
			zap.String("host", r.Host),
		)
		status = "502"
		http.Error(w, "Failed to connect to destination", http.StatusBadGateway)
		return
	}
	defer destConn.Close()

	// Hijack the client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		p.logger.Error("hijacking not supported")
		status = "500"
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		p.logger.Error("failed to hijack connection", zap.Error(err))
		status = "500"
		http.Error(w, "Failed to hijack connection", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established
	bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	bufrw.Flush()

	// Note: For CONNECT tunnels, we cannot inject tokens into the encrypted traffic
	// Token injection only works for plain HTTP requests
	p.logger.Debug("CONNECT tunnel established, token injection not supported for encrypted traffic",
		zap.String("host", r.Host),
	)

	// Bidirectional copy
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		io.Copy(destConn, bufrw)
		cancel()
	}()

	go func() {
		io.Copy(clientConn, destConn)
		cancel()
	}()

	<-ctx.Done()

	p.logger.Debug("CONNECT tunnel closed",
		zap.String("host", r.Host),
	)
}

// removeHopByHopHeaders removes hop-by-hop headers
func removeHopByHopHeaders(h http.Header) {
	// Connection-specific headers
	h.Del("Connection")
	h.Del("Keep-Alive")
	h.Del("Proxy-Authenticate")
	h.Del("Proxy-Authorization")
	h.Del("TE")
	h.Del("Trailer")
	h.Del("Transfer-Encoding")
	h.Del("Upgrade")
}

// Close closes the proxy and all injectors
func (p *Proxy) Close() error {
	var firstErr error

	if err := p.router.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	if p.spiffeSource != nil {
		if err := p.spiffeSource.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
