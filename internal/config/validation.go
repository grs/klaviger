package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// Validate validates the configuration
func Validate(cfg *Config) error {
	if err := validateServer(&cfg.Server); err != nil {
		return fmt.Errorf("server config: %w", err)
	}

	if err := validateReverseProxy(&cfg.ReverseProxy); err != nil {
		return fmt.Errorf("reverse proxy config: %w", err)
	}

	if err := validateForwardProxy(&cfg.ForwardProxy); err != nil {
		return fmt.Errorf("forward proxy config: %w", err)
	}

	if err := validateObservability(&cfg.Observability); err != nil {
		return fmt.Errorf("observability config: %w", err)
	}

	return nil
}

// validateServer validates server configuration
func validateServer(cfg *ServerConfig) error {
	if cfg.ReverseProxyPort < 1 || cfg.ReverseProxyPort > 65535 {
		return fmt.Errorf("reverseProxyPort must be between 1 and 65535")
	}

	if cfg.ReverseProxyBind != "" {
		if ip := net.ParseIP(cfg.ReverseProxyBind); ip == nil {
			return fmt.Errorf("reverseProxyBind must be a valid IP address")
		}
	}

	if cfg.ForwardProxyPort < 1 || cfg.ForwardProxyPort > 65535 {
		return fmt.Errorf("forwardProxyPort must be between 1 and 65535")
	}

	if cfg.ForwardProxyBind != "" {
		if ip := net.ParseIP(cfg.ForwardProxyBind); ip == nil {
			return fmt.Errorf("forwardProxyBind must be a valid IP address")
		}
	}

	if cfg.ReverseProxyPort == cfg.ForwardProxyPort && cfg.ReverseProxyBind == cfg.ForwardProxyBind {
		return fmt.Errorf("reverseProxyPort and forwardProxyPort must be different when using the same bind address")
	}

	if cfg.ReadTimeout <= 0 {
		return fmt.Errorf("readTimeout must be positive")
	}

	if cfg.WriteTimeout <= 0 {
		return fmt.Errorf("writeTimeout must be positive")
	}

	if cfg.TLS.Enabled {
		if err := validateTLS(&cfg.TLS); err != nil {
			return fmt.Errorf("tls: %w", err)
		}
	}

	if cfg.ClientTLS.Enabled {
		if err := validateClientTLS(&cfg.ClientTLS); err != nil {
			return fmt.Errorf("clientTls: %w", err)
		}
	}

	return nil
}

// validateTLS validates TLS configuration
func validateTLS(cfg *TLSConfig) error {
	// Check mutual exclusivity between file-based and SPIFFE
	fileBasedConfig := cfg.CertFile != "" || cfg.KeyFile != ""
	spiffeConfig := cfg.SPIFFE != nil && cfg.SPIFFE.Enabled

	if fileBasedConfig && spiffeConfig {
		return fmt.Errorf("cannot use both file-based certificates and SPIFFE")
	}

	if !fileBasedConfig && !spiffeConfig {
		return fmt.Errorf("either file-based certificates or SPIFFE must be configured")
	}

	if fileBasedConfig {
		if cfg.CertFile == "" {
			return fmt.Errorf("certFile is required when using file-based TLS")
		}
		if cfg.KeyFile == "" {
			return fmt.Errorf("keyFile is required when using file-based TLS")
		}
		// Check if files exist
		if _, err := os.Stat(cfg.CertFile); err != nil {
			return fmt.Errorf("certFile does not exist: %w", err)
		}
		if _, err := os.Stat(cfg.KeyFile); err != nil {
			return fmt.Errorf("keyFile does not exist: %w", err)
		}
	}

	if spiffeConfig {
		if err := validateSPIFFE(cfg.SPIFFE); err != nil {
			return fmt.Errorf("spiffe: %w", err)
		}
	}

	return nil
}

// validateClientTLS validates client TLS configuration
func validateClientTLS(cfg *ClientTLSConfig) error {
	// Check mutual exclusivity between file-based and SPIFFE
	fileBasedConfig := cfg.CertFile != "" || cfg.KeyFile != "" || cfg.CAFile != ""
	spiffeConfig := cfg.SPIFFE != nil && cfg.SPIFFE.Enabled

	if fileBasedConfig && spiffeConfig {
		return fmt.Errorf("cannot use both file-based certificates and SPIFFE")
	}

	if fileBasedConfig {
		if cfg.CertFile != "" || cfg.KeyFile != "" {
			if cfg.CertFile == "" {
				return fmt.Errorf("certFile is required when keyFile is specified")
			}
			if cfg.KeyFile == "" {
				return fmt.Errorf("keyFile is required when certFile is specified")
			}
			// Check if files exist
			if _, err := os.Stat(cfg.CertFile); err != nil {
				return fmt.Errorf("certFile does not exist: %w", err)
			}
			if _, err := os.Stat(cfg.KeyFile); err != nil {
				return fmt.Errorf("keyFile does not exist: %w", err)
			}
		}
		if cfg.CAFile != "" {
			if _, err := os.Stat(cfg.CAFile); err != nil {
				return fmt.Errorf("caFile does not exist: %w", err)
			}
		}
	}

	if spiffeConfig {
		if err := validateSPIFFE(cfg.SPIFFE); err != nil {
			return fmt.Errorf("spiffe: %w", err)
		}
	}

	return nil
}

// validateSPIFFE validates SPIFFE configuration
func validateSPIFFE(cfg *SPIFFEConfig) error {
	if !cfg.Enabled {
		return nil
	}

	if cfg.SocketPath == "" {
		return fmt.Errorf("socketPath is required")
	}

	// Validate socket path format (must be unix://)
	if !strings.HasPrefix(cfg.SocketPath, "unix://") {
		return fmt.Errorf("socketPath must start with 'unix://' (got: %s)", cfg.SocketPath)
	}

	// Validate SPIFFE IDs if provided
	for i, id := range cfg.AcceptedSPIFFEIDs {
		if !strings.HasPrefix(id, "spiffe://") {
			return fmt.Errorf("acceptedSpiffeIds[%d] must start with 'spiffe://' (got: %s)", i, id)
		}
	}

	return nil
}

// validateReverseProxy validates reverse proxy configuration
func validateReverseProxy(cfg *ReverseProxyConfig) error {
	if cfg.Backend == "" {
		return fmt.Errorf("backend is required")
	}

	// Validate backend URL
	u, err := url.Parse(cfg.Backend)
	if err != nil {
		return fmt.Errorf("invalid backend URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("backend URL must use http or https scheme")
	}
	if u.Host == "" {
		return fmt.Errorf("backend URL must have a host")
	}

	return validateVerification(&cfg.Verification)
}

// validateVerification validates verification configuration
func validateVerification(cfg *VerificationConfig) error {
	switch cfg.Mode {
	case "jwt":
		if cfg.JWT == nil {
			return fmt.Errorf("jwt config is required when mode is 'jwt'")
		}
		return validateJWT(cfg.JWT)
	case "introspection":
		if cfg.Introspection == nil {
			return fmt.Errorf("introspection config is required when mode is 'introspection'")
		}
		return validateIntrospection(cfg.Introspection)
	case "k8s", "kubernetes":
		if cfg.Kubernetes == nil {
			return fmt.Errorf("kubernetes config is required when mode is 'k8s'")
		}
		return validateKubernetes(cfg.Kubernetes)
	default:
		return fmt.Errorf("invalid verification mode: %s (must be jwt, introspection, or k8s)", cfg.Mode)
	}
}

// validateJWT validates JWT configuration
func validateJWT(cfg *JWTConfig) error {
	if cfg.JWKSUrl == "" {
		return fmt.Errorf("jwksUrl is required")
	}

	// Validate JWKS URL
	u, err := url.Parse(cfg.JWKSUrl)
	if err != nil {
		return fmt.Errorf("invalid jwksUrl: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("jwksUrl must use http or https scheme")
	}

	if cfg.Issuer == "" {
		return fmt.Errorf("issuer is required")
	}

	if cfg.Audience == "" {
		return fmt.Errorf("audience is required")
	}

	if cfg.CacheTTL <= 0 {
		return fmt.Errorf("cacheTtl must be positive")
	}

	return nil
}

// validateIntrospection validates introspection configuration
func validateIntrospection(cfg *IntrospectionConfig) error {
	if cfg.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	// Validate endpoint URL
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint must use http or https scheme")
	}

	if cfg.ClientID == "" {
		return fmt.Errorf("clientId is required")
	}

	if cfg.ClientSecret == "" {
		return fmt.Errorf("clientSecret is required")
	}

	return nil
}

// validateKubernetes validates Kubernetes configuration
func validateKubernetes(cfg *KubernetesConfig) error {
	if cfg.Verb == "" {
		return fmt.Errorf("verb is required")
	}

	if cfg.Resource == "" {
		return fmt.Errorf("resource is required")
	}

	// Verb validation (common Kubernetes verbs)
	validVerbs := map[string]bool{
		"get": true, "list": true, "watch": true, "create": true,
		"update": true, "patch": true, "delete": true, "deletecollection": true,
	}
	if !validVerbs[cfg.Verb] {
		return fmt.Errorf("invalid verb: %s", cfg.Verb)
	}

	return nil
}

// validateForwardProxy validates forward proxy configuration
func validateForwardProxy(cfg *ForwardProxyConfig) error {
	if err := validateInjectionMode("defaultMode", &cfg.DefaultMode); err != nil {
		return err
	}

	// Validate host rules
	hostPatterns := make(map[string]bool)
	for i, rule := range cfg.HostRules {
		if rule.HostPattern == "" {
			return fmt.Errorf("hostRules[%d].hostPattern is required", i)
		}

		// Check for duplicate patterns
		if hostPatterns[rule.HostPattern] {
			return fmt.Errorf("duplicate hostPattern: %s", rule.HostPattern)
		}
		hostPatterns[rule.HostPattern] = true

		// Validate pattern is valid regex
		if _, err := regexp.Compile(rule.HostPattern); err != nil {
			return fmt.Errorf("hostRules[%d].hostPattern is invalid regex: %w", i, err)
		}

		if err := validateInjectionMode(fmt.Sprintf("hostRules[%d].mode", i), &rule.Mode); err != nil {
			return err
		}
	}

	return nil
}

// validateInjectionMode validates injection mode configuration
func validateInjectionMode(context string, mode *InjectionMode) error {
	switch mode.Type {
	case "passthrough":
		// No additional config required
	case "file":
		if mode.File == nil {
			return fmt.Errorf("%s: file config is required when type is 'file'", context)
		}
		if mode.File.Path == "" {
			return fmt.Errorf("%s: file.path is required", context)
		}
		if mode.File.RefreshInterval <= 0 {
			return fmt.Errorf("%s: file.refreshInterval must be positive", context)
		}
	case "oauth":
		if mode.OAuth == nil {
			return fmt.Errorf("%s: oauth config is required when type is 'oauth'", context)
		}
		return validateOAuth(context, mode.OAuth)
	case "vault":
		if mode.Vault == nil {
			return fmt.Errorf("%s: vault config is required when type is 'vault'", context)
		}
		return validateVault(context, mode.Vault)
	default:
		return fmt.Errorf("%s: invalid type: %s (must be passthrough, file, oauth, or vault)", context, mode.Type)
	}

	return nil
}

// validateOAuth validates OAuth configuration
func validateOAuth(context string, cfg *OAuthConfig) error {
	if cfg.TokenURL == "" {
		return fmt.Errorf("%s: oauth.tokenUrl is required", context)
	}

	// Validate token URL
	u, err := url.Parse(cfg.TokenURL)
	if err != nil {
		return fmt.Errorf("%s: invalid oauth.tokenUrl: %w", context, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s: oauth.tokenUrl must use http or https scheme", context)
	}

	if cfg.CacheTTL <= 0 {
		return fmt.Errorf("%s: oauth.cacheTtl must be positive", context)
	}

	return nil
}

// validateVault validates Vault configuration
func validateVault(context string, cfg *VaultConfig) error {
	if cfg.Address == "" {
		return fmt.Errorf("%s: vault.address is required", context)
	}

	// Validate address URL
	u, err := url.Parse(cfg.Address)
	if err != nil {
		return fmt.Errorf("%s: invalid vault.address: %w", context, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s: vault.address must use http or https scheme", context)
	}

	if cfg.Path == "" {
		return fmt.Errorf("%s: vault.path is required", context)
	}

	if cfg.Field == "" {
		return fmt.Errorf("%s: vault.field is required", context)
	}

	if cfg.AuthMethod == "" {
		return fmt.Errorf("%s: vault.authMethod is required", context)
	}

	switch cfg.AuthMethod {
	case "kubernetes":
		if cfg.Role == "" {
			return fmt.Errorf("%s: vault.role is required for kubernetes auth", context)
		}
	case "token":
		if cfg.Token == "" {
			return fmt.Errorf("%s: vault.token is required for token auth", context)
		}
	default:
		return fmt.Errorf("%s: invalid vault.authMethod: %s (must be kubernetes or token)", context, cfg.AuthMethod)
	}

	if cfg.CacheTTL <= 0 {
		return fmt.Errorf("%s: vault.cacheTtl must be positive", context)
	}

	return nil
}

// validateObservability validates observability configuration
func validateObservability(cfg *ObservabilityConfig) error {
	// Validate metrics
	if cfg.Metrics.Enabled {
		if cfg.Metrics.Port < 1 || cfg.Metrics.Port > 65535 {
			return fmt.Errorf("metrics.port must be between 1 and 65535")
		}
		if cfg.Metrics.Path == "" {
			return fmt.Errorf("metrics.path is required when metrics are enabled")
		}
	}

	// Validate tracing
	if cfg.Tracing.Enabled {
		if cfg.Tracing.Endpoint == "" {
			return fmt.Errorf("tracing.endpoint is required when tracing is enabled")
		}
		if cfg.Tracing.ServiceName == "" {
			return fmt.Errorf("tracing.serviceName is required when tracing is enabled")
		}
	}

	// Validate logging
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLevels[cfg.Logging.Level] {
		return fmt.Errorf("invalid logging.level: %s (must be debug, info, warn, or error)", cfg.Logging.Level)
	}

	validFormats := map[string]bool{
		"json": true, "console": true,
	}
	if !validFormats[cfg.Logging.Format] {
		return fmt.Errorf("invalid logging.format: %s (must be json or console)", cfg.Logging.Format)
	}

	return nil
}
