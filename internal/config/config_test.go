package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		envVars     map[string]string
		wantErr     bool
		errContains string
		validate    func(*testing.T, *Config)
	}{
		{
			name: "valid JWT configuration",
			config: `
server:
  reverseProxyPort: 8080
  forwardProxyPort: 8081
  readTimeout: "30s"
  writeTimeout: "30s"

reverseProxy:
  backend: "http://127.0.0.1:9090"
  verification:
    mode: "jwt"
    jwt:
      jwksUrl: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"
      audience: "my-api"
      requiredScopes: ["read:data"]
      cacheTtl: "5m"

forwardProxy:
  defaultMode:
    type: "passthrough"
  hostRules: []

observability:
  metrics:
    enabled: true
    port: 9090
    path: "/metrics"
  logging:
    level: "info"
    format: "json"
`,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 8080, cfg.Server.ReverseProxyPort)
				assert.Equal(t, 8081, cfg.Server.ForwardProxyPort)
				assert.Equal(t, Duration(30*time.Second), cfg.Server.ReadTimeout)
				assert.Equal(t, "http://127.0.0.1:9090", cfg.ReverseProxy.Backend)
				assert.Equal(t, "jwt", cfg.ReverseProxy.Verification.Mode)
				require.NotNil(t, cfg.ReverseProxy.Verification.JWT)
				assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", cfg.ReverseProxy.Verification.JWT.JWKSUrl)
				assert.Equal(t, "my-api", cfg.ReverseProxy.Verification.JWT.Audience)
				assert.Equal(t, Duration(5*time.Minute), cfg.ReverseProxy.Verification.JWT.CacheTTL)
			},
		},
		{
			name: "environment variable expansion",
			config: `
server:
  reverseProxyPort: 8080
  forwardProxyPort: 8081

reverseProxy:
  backend: "${BACKEND_URL}"
  verification:
    mode: "introspection"
    introspection:
      endpoint: "https://auth.example.com/introspect"
      clientId: "${CLIENT_ID}"
      clientSecret: "${CLIENT_SECRET}"

forwardProxy:
  defaultMode:
    type: "passthrough"

observability:
  logging:
    level: "info"
`,
			envVars: map[string]string{
				"BACKEND_URL":   "http://localhost:9090",
				"CLIENT_ID":     "test-client",
				"CLIENT_SECRET": "super-secret",
			},
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "http://localhost:9090", cfg.ReverseProxy.Backend)
				require.NotNil(t, cfg.ReverseProxy.Verification.Introspection)
				assert.Equal(t, "test-client", cfg.ReverseProxy.Verification.Introspection.ClientID)
				assert.Equal(t, "super-secret", cfg.ReverseProxy.Verification.Introspection.ClientSecret)
			},
		},
		{
			name: "invalid port",
			config: `
server:
  reverseProxyPort: 99999
  forwardProxyPort: 8081

reverseProxy:
  backend: "http://localhost:9090"
  verification:
    mode: "jwt"
    jwt:
      jwksUrl: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"
      audience: "my-api"

forwardProxy:
  defaultMode:
    type: "passthrough"

observability:
  logging:
    level: "info"
`,
			wantErr:     true,
			errContains: "reverseProxyPort must be between 1 and 65535",
		},
		{
			name: "duplicate ports on same bind address",
			config: `
server:
  reverseProxyPort: 8080
  reverseProxyBind: "0.0.0.0"
  forwardProxyPort: 8080
  forwardProxyBind: "0.0.0.0"

reverseProxy:
  backend: "http://localhost:9090"
  verification:
    mode: "jwt"
    jwt:
      jwksUrl: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"
      audience: "my-api"

forwardProxy:
  defaultMode:
    type: "passthrough"

observability:
  logging:
    level: "info"
`,
			wantErr:     true,
			errContains: "must be different",
		},
		{
			name: "valid loopback bind",
			config: `
server:
  reverseProxyPort: 8080
  reverseProxyBind: "0.0.0.0"
  forwardProxyPort: 8081
  forwardProxyBind: "127.0.0.1"

reverseProxy:
  backend: "http://localhost:9090"
  verification:
    mode: "jwt"
    jwt:
      jwksUrl: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"
      audience: "my-api"

forwardProxy:
  defaultMode:
    type: "passthrough"

observability:
  logging:
    level: "info"
`,
			validate: func(t *testing.T, cfg *Config) {
				assert.Equal(t, "0.0.0.0", cfg.Server.ReverseProxyBind)
				assert.Equal(t, "127.0.0.1", cfg.Server.ForwardProxyBind)
			},
		},
		{
			name: "invalid bind address",
			config: `
server:
  reverseProxyPort: 8080
  reverseProxyBind: "invalid-ip"
  forwardProxyPort: 8081

reverseProxy:
  backend: "http://localhost:9090"
  verification:
    mode: "jwt"
    jwt:
      jwksUrl: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"
      audience: "my-api"

forwardProxy:
  defaultMode:
    type: "passthrough"

observability:
  logging:
    level: "info"
`,
			wantErr:     true,
			errContains: "must be a valid IP address",
		},
		{
			name: "missing required JWT field",
			config: `
server:
  reverseProxyPort: 8080
  forwardProxyPort: 8081

reverseProxy:
  backend: "http://localhost:9090"
  verification:
    mode: "jwt"
    jwt:
      jwksUrl: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"

forwardProxy:
  defaultMode:
    type: "passthrough"

observability:
  logging:
    level: "info"
`,
			wantErr:     true,
			errContains: "audience is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			// Create temporary config file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			err := os.WriteFile(configPath, []byte(tt.config), 0644)
			require.NoError(t, err)

			// Load config
			cfg, err := Load(configPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		envVars map[string]string
		want    string
	}{
		{
			name:  "no env vars",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "single env var",
			input: "hello ${NAME}",
			envVars: map[string]string{
				"NAME": "world",
			},
			want: "hello world",
		},
		{
			name:  "multiple env vars",
			input: "${GREETING} ${NAME}!",
			envVars: map[string]string{
				"GREETING": "Hello",
				"NAME":     "World",
			},
			want: "Hello World!",
		},
		{
			name:  "env var not set",
			input: "hello ${NOTSET}",
			want:  "hello ${NOTSET}",
		},
		{
			name:  "mixed set and unset",
			input: "${SET} and ${NOTSET}",
			envVars: map[string]string{
				"SET": "value",
			},
			want: "value and ${NOTSET}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			got := expandEnvVars(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetDefaults(t *testing.T) {
	cfg := &Config{}

	setDefaults(cfg)

	assert.Equal(t, 8080, cfg.Server.ReverseProxyPort)
	assert.Equal(t, "0.0.0.0", cfg.Server.ReverseProxyBind)
	assert.Equal(t, 8081, cfg.Server.ForwardProxyPort)
	assert.Equal(t, "0.0.0.0", cfg.Server.ForwardProxyBind)
	assert.Equal(t, Duration(30*time.Second), cfg.Server.ReadTimeout)
	assert.Equal(t, Duration(30*time.Second), cfg.Server.WriteTimeout)
	assert.Equal(t, 9090, cfg.Observability.Metrics.Port)
	assert.Equal(t, "/metrics", cfg.Observability.Metrics.Path)
	assert.Equal(t, "info", cfg.Observability.Logging.Level)
	assert.Equal(t, "json", cfg.Observability.Logging.Format)
	assert.Equal(t, "klaviger", cfg.Observability.Tracing.ServiceName)
}

func TestSanitize(t *testing.T) {
	cfg := &Config{
		ReverseProxy: ReverseProxyConfig{
			Verification: VerificationConfig{
				Mode: "introspection",
				Introspection: &IntrospectionConfig{
					ClientSecret: "super-secret",
				},
			},
		},
		ForwardProxy: ForwardProxyConfig{
			HostRules: []HostRule{
				{
					Mode: InjectionMode{
						Type: "oauth",
						OAuth: &OAuthConfig{},
					},
				},
				{
					Mode: InjectionMode{
						Type: "vault",
						Vault: &VaultConfig{
							Token: "vault-token",
						},
					},
				},
			},
		},
	}

	sanitized := cfg.Sanitize()

	assert.Equal(t, "***REDACTED***", sanitized.ReverseProxy.Verification.Introspection.ClientSecret)
	assert.Equal(t, "***REDACTED***", sanitized.ForwardProxy.HostRules[1].Mode.Vault.Token)

	// Original should be unchanged
	assert.Equal(t, "super-secret", cfg.ReverseProxy.Verification.Introspection.ClientSecret)
}

func TestSanitizePreservesOAuthClientSecret(t *testing.T) {
	cfg := &Config{
		ForwardProxy: ForwardProxyConfig{
			HostRules: []HostRule{
				{
					HostPattern: "^beta(:|$)",
					Mode: InjectionMode{
						Type: "oauth",
						OAuth: &OAuthConfig{
							TokenURL:         "http://keycloak:8080/token",
							Audience:         "beta",
							ClientAuthMethod: "client_secret",
							ClientID:         "alpha",
							ClientSecret:     "my-secret-123",
						},
					},
				},
			},
		},
	}

	// Sanitize should redact the secret in the copy
	sanitized := cfg.Sanitize()
	assert.Equal(t, "***REDACTED***", sanitized.ForwardProxy.HostRules[0].Mode.OAuth.ClientSecret)

	// The original config must retain the real secret.
	// Before the fix, Sanitize() mutated the original because Go slice
	// shallow copies share the backing array. The loop replaced the OAuth
	// pointer in the shared slice element, corrupting the live config.
	assert.Equal(t, "my-secret-123", cfg.ForwardProxy.HostRules[0].Mode.OAuth.ClientSecret,
		"Sanitize() must not mutate the original config's OAuth ClientSecret")
}

func TestRedactSecret(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "env var reference",
			input: "${MY_SECRET}",
			want:  "${MY_SECRET}",
		},
		{
			name:  "actual secret",
			input: "super-secret-value",
			want:  "***REDACTED***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactSecret(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
