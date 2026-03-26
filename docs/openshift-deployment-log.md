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
(which `spiffe-demo` has). It checks `kagenti.io/type: agent` on pod
labels to decide if a pod is eligible for injection.

**Solution:** Set `kagenti.io/inject: disabled` on pod template labels.
The webhook code in `pod_mutator.go` (line 122-127) checks for this
value and skips injection. The `kagenti.io/type: agent` label must
remain on the pod template so the Kagenti operator can discover the
agent and manage its AgentCard.

**Key code path** (from kagenti-webhook source):

1. Check `kagenti.io/type` — must be `agent` or `tool`, otherwise skip
1. Check `kagenti.io/inject` — if `disabled`, skip injection
1. Evaluate per-sidecar precedence chain

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
AgentCard CR, and fetches the card successfully. The card is signed by
the `sign-agentcard` init container using the pod's SPIFFE identity.
However, the operator fails signature verification with:

```text
No signature verified via x5c chain validation
```

The SPIFFE ID in the signing certificate is
`spiffe://...sa/summarizer-tech-klaviger` (based on the ServiceAccount
name). The operator's x5c chain validation does not trust this identity.

**Status:** Open. This is an operator/SPIRE trust configuration issue,
not a Klaviger issue. The operator needs to be configured to trust the
SPIFFE identity of the new ServiceAccount, or the SPIRE trust bundle
configuration needs to include it. The existing agents work because
their SPIFFE identities are already trusted.

**Operator log evidence:**

```text
Fetching A2A agent card  url=http://summarizer-tech-klaviger...8080/.well-known/agent-card.json
Signature verification failed  reason=SignatureInvalid  details=No signature verified via x5c chain validation
Identity binding is allowlist-only; SPIFFE trust bundle verification not yet available
```

The message "Identity binding is allowlist-only" suggests the operator
maintains an explicit allowlist of trusted identities rather than
trusting the full SPIRE trust bundle.

## Keycloak integration

### Upstream intent: `federated-jwt` (Keycloak nightly)

The Klaviger author's upstream SPIFFE example uses Keycloak's
**SPIFFE Identity Provider** (`providerId=spiffe`) with
`clientAuthenticatorType=federated-jwt`. This allows Keycloak to
validate JWT-SVIDs against the SPIRE trust bundle directly — no client
secrets needed at runtime. Clients are pre-registered once, bound to a
specific SPIFFE ID:

```bash
kcadm create clients -r demo \
  -s clientId=alpha \
  -s clientAuthenticatorType=federated-jwt \
  -s attributes='{"jwt.credential.issuer":"spiffe",
    "jwt.credential.sub":"spiffe://example.org/ns/default/sa/alpha"}'
```

The config uses `clientAuthMethod: "assertion"` which sends the JWT-SVID
as a `client_assertion` in the token exchange request body.

**Requirement:** Keycloak nightly (`quay.io/keycloak/keycloak:nightly`).
The `spiffe` identity provider and `federated-jwt` client authenticator
are not available in stable Keycloak (our cluster runs v26.5.5).

### Current workaround: `client_secret` (stable Keycloak)

Since our Keycloak doesn't have the SPIFFE provider, we pre-registered
a client with standard `client-secret` authentication:

```bash
# One-time registration via Keycloak admin API
curl -X POST "$KEYCLOAK_URL/admin/realms/spiffe-demo/clients" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"clientId":"summarizer-tech-klaviger",
       "serviceAccountsEnabled":true,
       "clientAuthenticatorType":"client-secret",
       "attributes":{"standard.token.exchange.enabled":"true"}}'
```

The client secret is stored in a Kubernetes Secret and injected as env
vars. Klaviger's config references them via `${OAUTH_CLIENT_ID}` and
`${OAUTH_CLIENT_SECRET}`. This required adding `client_secret` as a
new `clientAuthMethod` in Klaviger's OAuth injector code.

## Current state

The deployment is running with **2/2 containers** (agent + klaviger).

**Working:**

- Klaviger connects to SPIRE via CSI driver, obtains JWT-SVIDs
- Reverse proxy listens on 8180, forwards to agent on 8000
- Forward proxy listens on 8181 (localhost only)
- Agent card is signed and served without authentication
- Kagenti operator discovers the agent (AgentCard CR created)
- AuthBridge webhook correctly skips injection
- Keycloak client registered with `client_secret` auth for token
  exchange

**Partially working:**

- Agent card signature verification fails (operator trust config issue)

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

These steps are done once and shared by all Klaviger-based agents:

1. Build and push the Klaviger image (`make podman-build podman-push`)
1. Apply the `klaviger-sidecar` SCC (`oc apply -f deploy/openshift/base/scc.yaml`)
1. If using Keycloak nightly: create the SPIFFE identity provider in
   the realm (see `configure-keycloak.sh` in upstream examples)

### Per-agent steps

For each new agent deployed with Klaviger:

1. **Register Keycloak client**

   With stable Keycloak (`client_secret`):

   ```bash
   curl -X POST "$KEYCLOAK_URL/admin/realms/$REALM/clients" \
     -H "Authorization: Bearer $ADMIN_TOKEN" \
     -d '{"clientId":"<agent-name>",
          "serviceAccountsEnabled":true,
          "clientAuthenticatorType":"client-secret",
          "attributes":{"standard.token.exchange.enabled":"true"}}'
   ```

   With Keycloak nightly (`federated-jwt`):

   ```bash
   kcadm create clients -r $REALM \
     -s clientId=<agent-name> \
     -s clientAuthenticatorType=federated-jwt \
     -s attributes='{"jwt.credential.issuer":"spiffe",
       "jwt.credential.sub":"spiffe://<trust-domain>/ns/<ns>/sa/<sa>"}'
   ```

1. **Create Kubernetes Secret** (stable Keycloak only)

   ```bash
   oc create secret generic klaviger-client-secret-<agent> \
     -n spiffe-demo \
     --from-literal=client-id=<agent-name> \
     --from-literal=client-secret=<secret>
   ```

1. **Create Kubernetes manifests** — copy and adapt from
   `deploy/openshift/base/`:
   - `serviceaccount.yaml` — new SA name (determines SPIFFE ID)
   - `configmap.yaml` — Klaviger config with correct audience/host rules
   - `agentcard-configmap.yaml` — agent card with correct URL/name
   - `deployment.yaml` — update image, SA, labels, secret ref
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
1. Upgrade Keycloak to nightly to test `federated-jwt` auth
   (eliminates client secrets entirely)
1. Expand to additional agents once the pattern is validated
