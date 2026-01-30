# Klaviger

A sidecar proxy for handling and verifying tokens, acting as both a reverse proxy (incoming request verification) and forward proxy (outgoing request token injection).

## Features

### Reverse Proxy
- **JWT Verification**: Verify tokens using JWKS with caching
- **Token Introspection**: Verify tokens using OAuth 2.0 introspection (RFC 7662)
- **Kubernetes Integration**: Verify tokens using SelfSubjectAccessReview

### Forward Proxy
- **Passthrough Mode**: No token modification
- **File-based Injection**: Inject tokens from files with automatic reload
- **OAuth Token Exchange**: Exchange tokens using RFC 8693
- **Vault Integration**: Retrieve tokens from HashiCorp Vault

### Observability
- **Prometheus Metrics**: Request counts, latency, verification/injection duration
- **Structured Logging**: JSON or console output with configurable levels
- **OpenTelemetry Tracing**: Distributed tracing with context propagation
- **Health Endpoints**: `/health/live` and `/health/ready`

## Quick Start

### Build
```bash
make build
```

### Run
```bash
./bin/klaviger --config examples/config.yaml
```

### Configuration

Example configuration:

```yaml
server:
  reverseProxyPort: 8080
  forwardProxyPort: 8081
  readTimeout: "30s"
  writeTimeout: "30s"

reverseProxy:
  backend: "http://127.0.0.1:9090"
  verification:
    mode: "jwt"  # jwt | introspection | k8s
    jwt:
      jwksUrl: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"
      audience: "my-api"
      requiredScopes: ["read:data"]
      cacheTtl: "5m"

forwardProxy:
  defaultMode:
    type: "passthrough"  # passthrough | file | oauth | vault
  hostRules:
    - hostPattern: "api.example.com"
      mode:
        type: "file"
        file:
          path: "/var/run/secrets/token"
          refreshInterval: "1m"

observability:
  metrics:
    enabled: true
    port: 9090
    path: "/metrics"
  logging:
    level: "info"  # debug | info | warn | error
    format: "json"  # json | console
```

## Verification Modes

### JWT (Local Verification)
Verifies JWT tokens using JWKS:
- Validates signature using public keys from JWKS endpoint
- Checks issuer, audience, expiration
- Validates required scopes
- Caches JWKS with configurable TTL

### Introspection
Verifies tokens using OAuth 2.0 introspection endpoint:
- Sends token to introspection endpoint
- Validates using client credentials
- Supports all standard introspection response fields

### Kubernetes
Verifies tokens using Kubernetes SelfSubjectAccessReview:
- Validates token is authenticated
- Checks RBAC permissions for specified resource/verb
- Works with service account tokens

## Injection Modes

### Passthrough
No modification to requests - useful as a default mode.

### File
Injects tokens from a file:
- Automatic reload on file changes (via fsnotify)
- Periodic refresh
- Verifies file permissions

### OAuth
Exchanges incoming token for a new token:
- RFC 8693 token exchange
- Token caching with TTL and expiration handling
- Automatic cache refresh
- Subject token extraction from incoming requests

### Vault
Retrieves tokens from HashiCorp Vault:
- Kubernetes or token authentication
- Lease-aware caching
- Support for KV v1 and KV v2 secret engines
- Configurable secret path and field

## Metrics

Available Prometheus metrics:

- `klaviger_reverse_proxy_requests_total`: Total reverse proxy requests
- `klaviger_reverse_proxy_request_duration_seconds`: Request duration
- `klaviger_token_verification_duration_seconds`: Token verification duration
- `klaviger_forward_proxy_requests_total`: Total forward proxy requests
- `klaviger_forward_proxy_request_duration_seconds`: Request duration
- `klaviger_token_injection_duration_seconds`: Token injection duration
- `klaviger_token_cache_hits_total`: Token cache hits
- `klaviger_jwks_cache_hits_total`: JWKS cache hits

## Development

### Testing
```bash
make test
```

### Linting
```bash
make lint
```

### Coverage
```bash
make test-coverage
```
