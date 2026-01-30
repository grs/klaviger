# Kubernetes Examples

Complete, runnable examples showing how to secure Kubernetes applications with Klaviger.

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

**Changes from baseline:**
- +33 lines per service
- +71 lines shared RBAC
- No code changes

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

**Changes from baseline:**
- +171 lines per service (includes ConfigMap)
- +71 lines shared RBAC
- +220 lines OAuth server (one-time)
- No code changes

See [oauth-token-exchange/DELTA.md](oauth-token-exchange/DELTA.md) for detailed comparison.

---

## Comparison Matrix

| Feature | Baseline | Simple | OAuth Exchange |
|---------|----------|--------|----------------|
| **Authentication** | None | Kubernetes | OAuth + Kubernetes |
| **Token Type** | None | K8s SA token | JWT (exchanged) |
| **Scoping** | N/A | No | Yes (per-destination) |
| **Verification** | None | K8s API call | JWT (JWKS) |
| **Configuration** | None | Built-in | ConfigMap per service |
| **Overhead** | 0 lines | ~404 lines | ~904 lines |
| **Complexity** | Simple | Low | Medium |

---

## When to Use Each

### Use Simple When:
- ✅ Services are all in the same Kubernetes cluster
- ✅ Same permissions work for all destinations
- ✅ Want minimal configuration
- ✅ Getting started with Klaviger
- ✅ Kubernetes-native auth is sufficient

### Use OAuth Exchange When:
- ✅ Need different scopes for different destinations
- ✅ Want fine-grained audit trail (OAuth server logs)
- ✅ Need token transformation (claims upgrade/downgrade)
- ✅ Planning to integrate with real OAuth/OIDC provider
- ✅ Services span multiple clusters or environments

---

## Quick Deploy

### Simple
```bash
cd simple
kubectl apply -f rbac.yaml
kubectl apply -f deployment.yaml
kubectl exec -it deployment/alpha -c showme -- curl http://beta
```

### OAuth Exchange
```bash
cd oauth-token-exchange
kubectl apply -f rbac.yaml
kubectl apply -f oauth-server.yaml
kubectl apply -f configmap.yaml
kubectl apply -f deployment.yaml
kubectl exec -it deployment/alpha -c showme -- curl http://beta
```

---

## Architecture

All examples use the **sidecar pattern**:

```
Pod
┌────────────────────────────────────┐
│  ┌──────────┐    ┌─────────────┐  │
│  │ showme   │    │  klaviger   │  │
│  │ :8080    │◀──▶│ :8180 :8181 │  │
│  │ (app)    │    │  (sidecar)  │  │
│  └──────────┘    └─────────────┘  │
└────────────────────────────────────┘
```

**Key points:**
- Application binds to 127.0.0.1 (pod-internal only)
- External traffic goes to Klaviger reverse proxy (8180)
- Outbound traffic routed via HTTP_PROXY to Klaviger forward proxy (8181)
- No application code changes needed

See [../docs/architecture.md](../docs/architecture.md) for detailed diagrams.

---

## Common Commands

### Check Deployment Status
```bash
kubectl get pods -l app=showme
kubectl get svc -l app=showme
```

### View Logs
```bash
# Application logs
kubectl logs deployment/alpha -c showme

# Klaviger logs
kubectl logs deployment/alpha -c klaviger

# OAuth server logs (oauth-token-exchange only)
kubectl logs deployment/oauth-server
```

### Test Requests
```bash
# Alpha → Beta
kubectl exec -it deployment/alpha -c showme -- curl http://beta

# Beta → Gamma
kubectl exec -it deployment/beta -c showme -- curl http://gamma

# With verbose output
kubectl exec -it deployment/alpha -c showme -- curl -v http://beta
```

### Debug
```bash
# Check service account
kubectl get sa -l app=showme

# Check RBAC
kubectl get role,rolebinding -l app=showme

# Check ConfigMaps (oauth-token-exchange only)
kubectl get cm -l app=showme

# Describe pod
kubectl describe pod -l app=showme,instance=alpha
```

---

## Understanding DELTA Files

Each example includes a `DELTA.md` file showing exactly what changed from the baseline:

- **Side-by-side comparisons** of YAML changes
- **Line count analysis** showing overhead
- **Request flow diagrams** comparing before/after
- **Explanations** of why each change matters

Start here to understand the minimal changes needed to secure your application.

---

## Next Steps

1. **Start simple:** Deploy the [simple/](simple/) example
2. **See what changed:** Read [simple/DELTA.md](simple/DELTA.md)
3. **Test it:** Make requests between services
4. **Go advanced:** Try [oauth-token-exchange/](oauth-token-exchange/)
5. **Customize:** Browse [../configs/](../configs/) for configuration options

---

## See Also

- **Configuration reference:** [../configs/](../configs/)
- **Detailed docs:** [../docs/](../docs/)
- **Main README:** [../../README.md](../../README.md)
- **Troubleshooting:** [../docs/troubleshooting.md](../docs/troubleshooting.md)
