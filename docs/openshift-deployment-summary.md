# Klaviger on OpenShift: summary for upstream

We deployed Klaviger as a sidecar on OpenShift alongside an existing
AuthBridge-based agent (`summarizer-tech`) in the `spiffe-demo`
namespace. Same agent image, different sidecar — allows direct
comparison.

## Results

**Pod structure:** 2 containers + 1 init (down from 4 + 2 init with
AuthBridge). No iptables, no privileged SCC, no runtime secrets.

### Resource usage (measured on OpenShift)

Both deployments run the same `kagenti-summarizer` agent image.
Only the sidecar differs.

| Metric | AuthBridge | Klaviger (Alpine) | Reduction |
| ------ | ---------- | ----------------- | --------- |
| CPU | 203m | 3m | 98% |
| Memory | 164Mi | 77Mi | 53% |
| Image size | ~200MB (4 images) | 60MB | 70% |
| Containers | 4 + 2 init | 2 + 1 init | |
| Runtime secrets | yes | none | |

We also built a UBI-based image (200MB, 86Mi memory) for environments
that require Red Hat certified base images. The Alpine image
(`Dockerfile.alpine`) is the default since it produces a smaller,
stripped static binary with `-ldflags='-s -w'`.

**What works:**

- Klaviger connects to SPIRE via CSI driver, obtains JWT-SVIDs
- Reverse proxy (8180) and forward proxy (8181) running
- Agent card signed by SPIFFE identity, served via `/.well-known/`
  without auth
- Kagenti operator discovers the agent
- Keycloak `federated-jwt` with SPIFFE IdP — no client secrets

**What's pending:**

- End-to-end token exchange test (Keycloak config in progress)
- Agent card signature verification (operator trust config issue)

## Code changes

Branch: `feature/openshift-deployment`

1. **`internal/reverseproxy/proxy.go`** — added `/.well-known/` path
   exclusion from JWT verification (agent card discovery needs to be
   unauthenticated)

1. **`internal/tokeninjector/oauth_exchange.go`** — added
   `client_secret` auth method as fallback for Keycloak instances
   without the SPIFFE preview feature

1. **`internal/config/config.go`** — added `ClientID`/`ClientSecret`
   fields to `OAuthConfig`

1. **`deployments/Dockerfile.alpine`** — Alpine-based image (60MB vs
   200MB UBI), static binary with stripped symbols

1. **`Makefile`** — added `podman-build`, `podman-push`,
   `deploy-openshift` targets with git SHA-based image tagging

1. **`deploy/openshift/base/`** — Kustomize manifests for OpenShift
   deployment

## Keycloak SPIFFE feature

Your upstream example uses `federated-jwt` with Keycloak nightly. We
found that the SPIFFE identity provider is available in **stable
Keycloak 26.5+** as a preview feature — just needs
`--features=spiffe` on startup. No nightly build required.

We enabled it on our Keycloak (v26.5.6), created the SPIFFE Identity
Provider pointing to our SPIRE OIDC discovery provider, and registered
the client with `federated-jwt`. The `clientAuthMethod: "assertion"`
config works as designed — JWT-SVID sent as `client_assertion`.

## OpenShift-specific findings

1. **SCC:** Created a minimal `klaviger-sidecar` SCC (CSI volumes +
   RunAsAny UID, no privileges). Much simpler than the
   `kagenti-authbridge` SCC which needs privileged + NET_ADMIN for
   iptables.

1. **Kagenti webhook:** Set `kagenti.io/inject: disabled` on pod
   labels to prevent AuthBridge injection while keeping
   `kagenti.io/type: agent` for operator discovery.

1. **Dockerfile:** Two variants — `Dockerfile` (UBI, OpenShift
   certified) and `Dockerfile.alpine` (Alpine, 70% smaller). Both
   use UID 1001 and group 0 for OpenShift compatibility.

## What's next

1. End-to-end token exchange and agent-to-agent communication test
1. Resolve agent card signature verification with operator team
1. Onboard additional agents
