# Configuration Reference

Standalone configuration file examples for various Klaviger use cases.

## Available Configurations

### basic.yaml
Minimal configuration with passthrough mode. Good starting point.

**Use when:** You're just getting started or need a template.

**Key features:**
- Passthrough mode (no token modification)
- Basic server setup
- Metrics and logging enabled

---

### jwt-verification.yaml
JWT verification using JWKS endpoint.

**Use when:**
- You have an OAuth/OIDC provider with JWKS
- You want local token verification (no network call per request)
- You need scope validation
- Working with standard JWT tokens

**Key features:**
- JWKS-based JWT verification
- Issuer and audience validation
- Required scopes enforcement
- Optional bearer token for JWKS endpoint authentication
- Optional CA certificate for TLS verification

---

### kubernetes-auth.yaml
Kubernetes-native token verification and injection.

**Use when:**
- Running in Kubernetes
- Using service account tokens
- Want SelfSubjectAccessReview verification
- Services are all within the same cluster

**Key features:**
- Kubernetes SelfSubjectAccessReview for verification
- Service account token injection from file
- Optional JWT mode with Kubernetes JWKS endpoint
- Namespace-aware authorization

---

### oauth-exchange.yaml
OAuth 2.0 token exchange (RFC 8693) with host-based routing.

**Use when:**
- Need fine-grained token scoping
- Different destinations require different credentials
- Token transformation is required
- Want per-service, per-destination access control

**Key features:**
- Host-based routing with regex patterns
- Token exchange with custom audience and scope
- Token caching for performance
- Multiple host rules per service

---

### token-introspection.yaml
OAuth 2.0 token introspection (RFC 7662).

**Use when:**
- OAuth server doesn't support JWKS
- Need server-side validation
- Working with opaque tokens
- Want to check token revocation status

**Key features:**
- Remote token introspection
- Client credentials authentication
- Opaque token support
- Real-time validation

---

### vault-integration.yaml
HashiCorp Vault token retrieval.

**Use when:**
- Tokens are stored in Vault
- Need lease-aware token caching
- Using Kubernetes auth to Vault
- Managing secrets with Vault

**Key features:**
- Kubernetes auth to Vault
- Dynamic token retrieval
- Lease-aware caching
- Multiple Vault backends support

---

## Configuration Structure

All configs follow this structure:

```yaml
server:           # Proxy server settings (ports, bind addresses, TLS)
reverseProxy:     # Incoming request verification
  backend:        # Backend service URL
  verification:   # How to verify incoming tokens
forwardProxy:     # Outgoing request injection
  defaultMode:    # Default injection behavior
  hostRules:      # Per-destination overrides
observability:    # Metrics, logging, tracing
```

---

## Verification Modes

Choose how incoming tokens are verified:

### `jwt` - Local JWKS Verification
```yaml
verification:
  mode: "jwt"
  jwt:
    jwksUrl: "https://auth.example.com/.well-known/jwks.json"
    issuer: "https://auth.example.com"
    audience: "my-api"
    cacheTtl: "5m"
```

**Pros:** Fast (local verification), no network dependency after JWKS cached
**Cons:** Requires JWKS endpoint, doesn't detect revocation

---

### `introspection` - OAuth Introspection
```yaml
verification:
  mode: "introspection"
  introspection:
    endpoint: "https://oauth.example.com/introspect"
    clientId: "my-client"
    clientSecret: "${INTROSPECTION_SECRET}"
```

**Pros:** Supports opaque tokens, detects revocation
**Cons:** Network call per request (unless cached), higher latency

---

### `k8s` - Kubernetes SelfSubjectAccessReview
```yaml
verification:
  mode: "k8s"
  kubernetes:
    verb: "get"
    resource: "pods"
    apiGroup: ""
```

**Pros:** Native Kubernetes integration, RBAC enforcement
**Cons:** Kubernetes-only, network call to API server

---

## Injection Modes

Choose how outgoing tokens are injected:

### `passthrough` - No Modification
```yaml
mode:
  type: "passthrough"
```

**Use when:** Tokens already present, or no authentication needed

---

### `file` - Inject from File
```yaml
mode:
  type: "file"
  file:
    path: "/var/run/secrets/kubernetes.io/serviceaccount/token"
    refreshInterval: "1m"
```

**Use when:** Token stored in a file (e.g., Kubernetes service account)

---

### `oauth` - Token Exchange (RFC 8693)
```yaml
mode:
  type: "oauth"
  oauth:
    tokenUrl: "https://oauth.example.com/token"
    audience: "target-service"
    scope: "read write"
    cacheTtl: "5m"
```

**Use when:** Need scoped tokens, token transformation, fine-grained access control

---

### `vault` - Retrieve from Vault
```yaml
mode:
  type: "vault"
  vault:
    address: "https://vault.example.com"
    path: "secret/data/api-token"
    field: "token"
```

**Use when:** Secrets managed in HashiCorp Vault

---

## Host-Based Routing

Use host patterns to apply different injection modes per destination:

```yaml
forwardProxy:
  defaultMode:
    type: "file"  # Default for all hosts
  hostRules:
  - hostPattern: "api.example.com"
    mode:
      type: "oauth"
      oauth:
        audience: "api-service"
  - hostPattern: "^internal-.*"  # Regex pattern
    mode:
      type: "vault"
```

---

## Environment Variable Substitution

All string values support environment variable substitution:

```yaml
reverseProxy:
  verification:
    mode: "jwt"
    jwt:
      jwksUrl: "${JWKS_URL}"
      audience: "${SERVICE_NAME}"
```

Set environment variables before starting Klaviger:
```bash
export JWKS_URL="https://auth.example.com/.well-known/jwks.json"
export SERVICE_NAME="my-api"
```

---

## See Also

- Working Kubernetes examples: [../kubernetes/](../kubernetes/)
- Detailed verification mode guide: [../docs/verification-modes.md](../docs/verification-modes.md)
- Detailed injection mode guide: [../docs/injection-modes.md](../docs/injection-modes.md)
- Troubleshooting: [../docs/troubleshooting.md](../docs/troubleshooting.md)
- Main README: [../../README.md](../../README.md)
