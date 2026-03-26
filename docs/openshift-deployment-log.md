# Klaviger on OpenShift: deployment log

This document records the first deployment of Klaviger as a sidecar
proxy on OpenShift, replacing the AuthBridge sidecar stack in the
`spiffe-demo` namespace. The goal is to demonstrate that a single
Klaviger container can replace the four-container AuthBridge pattern.

## Environment

- **Cluster:** OpenShift (apps.ocp-beta-test.nerc.mghpcc.org)
- **Namespace:** `spiffe-demo`
- **SPIRE:** deployed with CSI driver, trust domain
  `apps.ocp-beta-test.nerc.mghpcc.org`
- **Keycloak:** v26.5.6 with `--features=token-exchange,spiffe`
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
| Keycloak auth | client-registration + client credentials | `federated-jwt` via SPIFFE IdP |
| Runtime secrets | admin creds in ConfigMap + client secret | none |
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

### Kagenti AuthBridge webhook injection

**Problem:** The Kagenti AuthBridge mutating webhook injected
proxy-init, envoy-proxy, spiffe-helper, and client-registration into
the pod — defeating the purpose of using Klaviger instead.

**Solution:** Set `kagenti.io/inject: disabled` on pod template labels.
The webhook checks this value and skips injection. The
`kagenti.io/type: agent` label must remain on the pod template so the
Kagenti operator can discover the agent.

### Agent card endpoint blocked by JWT verification

**Problem:** Klaviger's reverse proxy required JWT authentication for
all inbound requests, including `/.well-known/agent-card.json`. This
blocked agent discovery by the Kagenti operator.

**Solution:** Added a `/.well-known/` path exclusion in
`internal/reverseproxy/proxy.go`. Requests matching this prefix are
proxied to the backend without authentication, following the same
pattern already used for `/health/` endpoints.

### Agent card signature verification

**Problem:** The Kagenti operator discovers the agent, creates an
AgentCard CR, and fetches the card successfully. However, signature
verification fails with `No signature verified via x5c chain
validation`.

**Status:** Open. This is an operator/SPIRE trust configuration issue,
not a Klaviger issue.

## Keycloak integration

### SPIFFE preview feature

The SPIFFE identity provider is available in Keycloak 26.5+ as a
preview feature. Enable it with `--features=spiffe` on the Keycloak
startup command. No nightly build required.

### How it works

1. A **SPIFFE Identity Provider** is configured in the realm, pointing
   to the SPIRE OIDC discovery provider's bundle endpoint
1. Clients are registered with `clientAuthenticatorType=federated-jwt`,
   bound to a specific SPIFFE ID via `jwt.credential.sub`
1. Klaviger uses `clientAuthMethod: "assertion"` — sends the JWT-SVID
   as a `client_assertion` in token exchange requests
1. Keycloak validates the JWT-SVID against the SPIRE trust bundle

**No secrets are stored or distributed at runtime.** The SPIFFE identity
is the credential.

### Keycloak setup commands

```bash
# Enable SPIFFE feature (one-time, add to Keycloak startup args)
--features=token-exchange,spiffe

# Create SPIFFE Identity Provider (one-time per realm)
kcadm create identity-provider/instances -r spiffe-demo \
  -s alias=spiffe -s providerId=spiffe \
  -s config='{"trustDomain":"spiffe://apps.ocp-beta-test.nerc.mghpcc.org",
    "bundleEndpoint":"https://spire-spiffe-oidc-discovery-provider.<ns>.svc.cluster.local/keys"}'

# Register a client (per agent)
kcadm create clients -r spiffe-demo \
  -s clientId=<agent-name> \
  -s serviceAccountsEnabled=true \
  -s clientAuthenticatorType=federated-jwt \
  -s attributes='{"jwt.credential.issuer":"spiffe",
    "jwt.credential.sub":"spiffe://<trust-domain>/ns/<ns>/sa/<sa>",
    "standard.token.exchange.enabled":"true"}'
```

### Fallback: `client_secret` (if SPIFFE feature unavailable)

For Keycloak versions without the SPIFFE preview feature, Klaviger
also supports `clientAuthMethod: "client_secret"` with `clientId` and
`clientSecret` fields in the OAuth config. The client secret can be
injected via env vars from a Kubernetes Secret using the `${VAR_NAME}`
config expansion syntax.

## Current state

The deployment is running with **2/2 containers** (agent + klaviger).

**Working:**

- Klaviger connects to SPIRE via CSI driver, obtains JWT-SVIDs
- Reverse proxy on 8180, forward proxy on 8181 (localhost)
- Agent card signed and served without authentication
- Kagenti operator discovers the agent (AgentCard CR created)
- AuthBridge webhook correctly skips injection
- Keycloak SPIFFE IdP + `federated-jwt` configured (no secrets)

**Partially working:**

- Agent card signature verification fails (operator trust config)

**Not yet tested:**

- End-to-end OAuth token exchange through Klaviger forward proxy
- Agent-to-agent communication through Klaviger
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

## Onboarding a new agent

### One-time setup (per cluster)

1. Build and push the Klaviger image
1. Apply the `klaviger-sidecar` SCC
1. Enable `--features=spiffe` on Keycloak
1. Create the SPIFFE Identity Provider in the Keycloak realm

### Per-agent steps

1. **Register Keycloak client** (one `kcadm` call, no secret needed)

   ```bash
   kcadm create clients -r spiffe-demo \
     -s clientId=<agent-name> \
     -s serviceAccountsEnabled=true \
     -s clientAuthenticatorType=federated-jwt \
     -s attributes='{"jwt.credential.issuer":"spiffe",
       "jwt.credential.sub":"spiffe://<trust-domain>/ns/<ns>/sa/<sa>",
       "standard.token.exchange.enabled":"true"}'
   ```

1. **Create Kubernetes manifests** — copy and adapt from
   `deploy/openshift/base/`:
   - `serviceaccount.yaml` — new SA name (determines SPIFFE ID)
   - `configmap.yaml` — Klaviger config with correct audience/host rules
   - `agentcard-configmap.yaml` — agent card with correct URL/name
   - `deployment.yaml` — update image, SA, labels
   - `service.yaml` — new service name

1. **Bind SCC** to the new ServiceAccount

   ```bash
   oc adm policy add-scc-to-user klaviger-sidecar \
     -z <sa-name> -n spiffe-demo
   ```

1. **Deploy**

   ```bash
   kustomize build deploy/openshift/<agent> \
     | oc apply -n spiffe-demo -f -
   ```

### Key labels for pod template

| Label | Value | Purpose |
| ----- | ----- | ------- |
| `kagenti.io/type` | `agent` | Operator discovery |
| `kagenti.io/inject` | `disabled` | Prevent AuthBridge injection |
| `kagenti.io/spire` | `enabled` | SPIFFE identity assignment |
| `kagenti.io/framework` | `Python`/`Go` | Metadata |
| `kagenti.io/workload-type` | `deployment` | Metadata |
| `protocol.kagenti.io/a2a` | `""` | Protocol marker |

## Next steps

1. Test end-to-end OAuth token exchange through Klaviger forward proxy
1. Test agent-to-agent communication through Klaviger
1. Resolve agent card signature verification (operator trust config)
1. Expand to additional agents once the pattern is validated
