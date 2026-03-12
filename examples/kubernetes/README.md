# Kubernetes Examples

Complete, runnable examples showing how to secure Kubernetes
applications with Klaviger, requiring only configuration changes to
YAML (no code changes to the services themselves).

## Examples

### [baseline/](baseline/)
**Unsecured reference deployment**

Shows a simple 4-service application (alpha, beta, gamma, delta) with no authentication or authorization. Use this as a baseline to compare what changes when you add Klaviger.

**Status:** ⚠️ Insecure (for reference only)

---

### [simple/](simple/)
**Basic Klaviger integration**

Adds Klaviger sidecars with Kubernetes-native authentication to the baseline deployment.

**Features:**
- Service account token injection
- Kubernetes SelfSubjectAccessReview verification
- Same token for all destinations
- Minimal configuration

See [simple/DELTA.md](simple/DELTA.md) for detailed comparison.

---

### [oauth-token-exchange/](oauth-token-exchange/)
**OAuth 2.0 Token Exchange (RFC 8693)**

Advanced example demonstrating fine-grained access control with per-destination token scoping.

**Features:**
- OAuth token exchange with custom audience/scope
- Host-based routing (different credentials per destination)
- Token caching for performance
- JWT verification with Kubernetes JWKS (alpha) and OAuth JWKS (beta/gamma/delta)
- Mock OAuth server included

See [oauth-token-exchange/DELTA.md](oauth-token-exchange/DELTA.md) for detailed comparison.

---

### [spiffe-oauth-exchange/](spiffe-oauth-exchange/)
**SPIFFE Identity + OAuth 2.0 Token Exchange**

Advanced zero-trust example using SPIFFE cryptographic identities with OAuth token exchange.

**Features:**
- SPIFFE/SPIRE integration for platform-agnostic identity
- OAuth token exchange with SPIFFE SVIDs used for authorisation
