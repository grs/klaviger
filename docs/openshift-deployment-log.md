# Klaviger on OpenShift: deployment log

This document records the first deployment of Klaviger as a sidecar
proxy on OpenShift, replacing the AuthBridge sidecar stack in the
`spiffe-demo` namespace. The goal is to demonstrate that a single
Klaviger container can replace the four-container AuthBridge pattern.

## Environment

- **Cluster:** OpenShift (apps.ocp-beta-test.nerc.mghpcc.org)
- **Namespace:** `spiffe-demo`
- **SPIRE:** deployed with CSI driver, trust domain `demo.example.com`
- **Keycloak:** external route at
  `keycloak-spiffe-demo.apps.ocp-beta-test.nerc.mghpcc.org`
- **Branch:** `feature/openshift-deployment`

## What was deployed

A new deployment `summarizer-tech-klaviger` running alongside the
existing `summarizer-tech` (which uses AuthBridge). Both use the same
agent image (`kagenti-summarizer`), allowing direct comparison.

### AuthBridge vs Klaviger sidecar comparison

| Aspect | AuthBridge | Klaviger |
| ------ | ---------- | ------- |
| Init containers | `proxy-init` (iptables), `sign-agentcard` | `sign-agentcard` only |
| Sidecar containers | `envoy-proxy`, `spiffe-helper`, `client-registration` | `klaviger` only |
| Total containers | 4 + 2 init | 2 + 1 init |
| SCC required | `kagenti-authbridge` (privileged, NET_ADMIN) | `klaviger-sidecar` (minimal, no privileges) |
| Traffic interception | iptables redirect (transparent) | `HTTP_PROXY` env var (explicit) |
| SPIFFE integration | spiffe-helper writes token to file | direct Workload API via CSI |
| Keycloak auth | client-registration + client credentials | JWT-SVID as bearer token |
| ConfigMaps/Secrets | 5+ | 2 (klaviger config + unsigned agent card) |

### Manifest files

All files are under `deploy/openshift/base/`:

| File | Purpose |
| ---- | ------- |
| `kustomization.yaml` | Kustomize base, image tag management |
| `deployment.yaml` | Pod spec with agent + klaviger containers |
| `configmap.yaml` | Klaviger configuration (JWT verify, OAuth exchange) |
| `agentcard-configmap.yaml` | Unsigned agent card for signing |
| `service.yaml` | ClusterIP service, port 8080 → 8180 (klaviger) |
| `serviceaccount.yaml` | SA for SPIFFE identity |
| `scc.yaml` | Minimal SCC (CSI volumes, RunAsAny, no privileges) |
| `scc-rolebinding.yaml` | Binds SA to SCC |

## Issues encountered and resolved

### SCC (security context constraints)

**Problem:** OpenShift's `restricted-v2` SCC rejected the pod for two
reasons: `runAsUser: 1001` outside namespace UID range, and CSI volumes
not allowed.

**Solution:** Created a minimal `klaviger-sidecar` SCC that allows
`RunAsAny` UID and CSI volumes but requires no privileged access. Used
`oc adm policy add-scc-to-user` to bind it (the ClusterRole is
auto-created by this command, not by SCC creation alone).

### GHCR image visibility

**Problem:** The pushed image was private by default, causing
`ImagePullBackOff`.

**Solution:** Made the GHCR package public.

### Kagenti AuthBridge webhook injection

**Problem:** The Kagenti AuthBridge mutating webhook injected
proxy-init, envoy-proxy, spiffe-helper, and client-registration into
the pod — defeating the purpose of using Klaviger instead.

The webhook matches all pods in namespaces with `kagenti-enabled: true`
(which `spiffe-demo` has). Setting `kagenti.io/inject: disabled` as a
label or annotation did not prevent injection.

**Solution:** The webhook triggers on pods with the
`kagenti.io/type: agent` label. Removing this label from the **pod
template** (while keeping it on the Deployment metadata for operator
discovery) prevented injection. This is the key insight: the
Deployment-level labels are used by the Kagenti operator for discovery,
but the pod-level labels trigger the webhook.

### Agent card endpoint blocked by JWT verification

**Problem:** Klaviger's reverse proxy required JWT authentication for
all inbound requests, including `/.well-known/agent-card.json`. This
blocked agent discovery by the Kagenti operator.

**Solution:** Added a `/.well-known/` path exclusion in
`internal/reverseproxy/proxy.go`. Requests matching this prefix are
proxied to the backend without authentication, following the same
pattern already used for `/health/` endpoints.

## Current state

The deployment is running with **2/2 containers** (agent + klaviger).

**Working:**

- Klaviger connects to SPIRE via CSI driver, obtains JWT-SVIDs
- Reverse proxy listens on 8180, forwards to agent on 8000
- Forward proxy listens on 8181 (localhost only)
- Agent card is signed and served without authentication
- Kagenti operator discovers the agent

**Not yet tested:**

- OAuth token exchange with Keycloak (need to verify Keycloak accepts
  JWT-SVIDs directly as bearer tokens for token exchange)
- End-to-end agent-to-agent communication through Klaviger
- Inbound JWT verification (need a caller with valid Keycloak tokens)

## Build and deploy commands

```bash
# Build (requires Podman with --connection=rhel for x86_64)
make podman-build

# Push to GHCR
make podman-push

# Deploy to OpenShift (updates kustomize image tag to current git SHA)
make deploy-openshift

# Undeploy
make undeploy-openshift
```

The `DEV_TAG` defaults to the short git SHA. Override with:

```bash
make podman-build DEV_TAG=my-tag
make deploy-openshift DEV_TAG=my-tag
```

## Next steps

1. Investigate Keycloak compatibility with JWT-SVID bearer
   authentication for token exchange
1. Test end-to-end agent communication through Klaviger
1. If Keycloak requires client credentials, either pre-register a
   client or add client credentials support to Klaviger's OAuth injector
1. Expand to additional agents once the pattern is validated
