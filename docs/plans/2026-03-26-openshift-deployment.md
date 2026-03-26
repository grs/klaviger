# OpenShift Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps
> use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy Klaviger as a single sidecar replacement for AuthBridge
alongside an existing `summarizer-tech` agent in the `spiffe-demo`
namespace on OpenShift.

**Architecture:** A new deployment `summarizer-tech-klaviger` runs the
same `kagenti-summarizer` agent image with a single Klaviger sidecar
instead of the 4-container AuthBridge stack (proxy-init, spiffe-helper,
client-registration, envoy-proxy). Klaviger handles both inbound JWT
verification (reverse proxy on port 8180) and outbound OAuth token
exchange (forward proxy on port 8181). SPIFFE identity is obtained
directly from the SPIRE Workload API via CSI driver.

**Tech Stack:** Go, Podman, OpenShift, SPIRE, Keycloak, Kustomize

---

## File Structure

### New files

| File | Responsibility |
|------|---------------|
| `deploy/openshift/base/deployment.yaml` | Deployment for summarizer-tech-klaviger |
| `deploy/openshift/base/configmap.yaml` | Klaviger config for the agent |
| `deploy/openshift/base/service.yaml` | Service exposing port 8180 |
| `deploy/openshift/base/serviceaccount.yaml` | ServiceAccount for SPIFFE ID |
| `deploy/openshift/base/kustomization.yaml` | Kustomize base |

### Modified files

| File | Change |
|------|--------|
| `Makefile` | Add `podman-build`, `podman-push`, `deploy-openshift` targets with `DEV_TAG` |

---

## Task 1: Update Makefile with Podman and deploy targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add build variables for container and deploy**

Add after existing build variables (line 6):

```makefile
# Container build variables
CONTAINER_ENGINE ?= podman
PODMAN_CONNECTION ?= rhel
REGISTRY ?= ghcr.io/pavelanni
IMAGE_NAME ?= klaviger
GIT_SHA := $(shell git rev-parse --short HEAD)
GIT_DIRTY := $(shell git diff --quiet 2>/dev/null || echo '-dirty')
DEV_TAG ?= $(GIT_SHA)$(GIT_DIRTY)
FULL_IMAGE := $(REGISTRY)/$(IMAGE_NAME):$(DEV_TAG)

# Deploy variables
DEPLOY_NAMESPACE ?= spiffe-demo
DEPLOY_DIR = deploy/openshift
```

- [ ] **Step 2: Add podman-build target**

```makefile
# Podman build (remote, for x86_64)
podman-build:
	@echo "Building $(FULL_IMAGE)..."
	$(CONTAINER_ENGINE) --connection=$(PODMAN_CONNECTION) build \
		-t $(FULL_IMAGE) \
		-f deployments/Dockerfile .
	@echo "Build complete: $(FULL_IMAGE)"
```

- [ ] **Step 3: Add podman-push target**

```makefile
# Push to GHCR
podman-push:
	@echo "Pushing $(FULL_IMAGE)..."
	$(CONTAINER_ENGINE) --connection=$(PODMAN_CONNECTION) push $(FULL_IMAGE)
	@echo "Push complete"
```

- [ ] **Step 4: Add deploy-openshift target**

```makefile
# Deploy to OpenShift
deploy-openshift:
	@echo "=== Deploying to OpenShift with tag $(DEV_TAG) ==="
	cd $(DEPLOY_DIR)/base && \
		kustomize edit set image $(REGISTRY)/$(IMAGE_NAME):$(DEV_TAG)
	kustomize build $(DEPLOY_DIR)/base | oc apply -n $(DEPLOY_NAMESPACE) -f -
	@echo "Deployed with image tag: $(DEV_TAG)"

# Undeploy from OpenShift
undeploy-openshift:
	kustomize build $(DEPLOY_DIR)/base | oc delete -n $(DEPLOY_NAMESPACE) -f - --ignore-not-found
```

- [ ] **Step 5: Update help target**

Add the new targets to the help output.

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -s -m "feat: add Podman build and OpenShift deploy targets

Assisted-By: Claude (Anthropic AI) <noreply@anthropic.com>"
```

---

## Task 2: Create ServiceAccount and Service

**Files:**
- Create: `deploy/openshift/base/serviceaccount.yaml`
- Create: `deploy/openshift/base/service.yaml`

- [ ] **Step 1: Create ServiceAccount**

The ServiceAccount name determines the SPIFFE ID via the `agents`
ClusterSPIFFEID template:
`spiffe://<trust-domain>/ns/spiffe-demo/sa/summarizer-tech-klaviger`.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: summarizer-tech-klaviger
  labels:
    app.kubernetes.io/name: summarizer-tech-klaviger
    app.kubernetes.io/component: agent
    kagenti.io/type: agent
```

- [ ] **Step 2: Create Service**

Service targets port 8180 (Klaviger reverse proxy) instead of 8000
(agent directly). Port is 8080 to match existing service conventions.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: summarizer-tech-klaviger
  labels:
    app.kubernetes.io/name: summarizer-tech-klaviger
    app.kubernetes.io/component: agent
spec:
  selector:
    app.kubernetes.io/name: summarizer-tech-klaviger
  ports:
  - name: http
    port: 8080
    targetPort: 8180
    protocol: TCP
  type: ClusterIP
```

- [ ] **Step 3: Commit**

```bash
git add deploy/openshift/base/serviceaccount.yaml \
        deploy/openshift/base/service.yaml
git commit -s -m "feat: add ServiceAccount and Service for Klaviger deployment

Assisted-By: Claude (Anthropic AI) <noreply@anthropic.com>"
```

---

## Task 3: Create Klaviger ConfigMap

**Files:**
- Create: `deploy/openshift/base/configmap.yaml`

- [ ] **Step 1: Create Klaviger configuration**

Maps the existing AuthBridge behavior to Klaviger config. Key details
from the running cluster:

- Keycloak issuer:
  `https://keycloak-spiffe-demo.apps.ocp-beta-test.nerc.mghpcc.org/realms/spiffe-demo`
- Token URL:
  `https://keycloak-spiffe-demo.apps.ocp-beta-test.nerc.mghpcc.org/realms/spiffe-demo/protocol/openid-connect/token`
- SPIRE socket: CSI driver at `/spiffe-workload-api`
  (address `unix:///spiffe-workload-api/spire-agent.sock`)
- Agent listens on port 8000

The outbound host rules mirror `authbridge-agent-routes`:
- `summarizer-*` services → audience `summarizer-service-aud`
- `reviewer-*` services → audience `reviewer-service-aud`

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: klaviger-config-summarizer-tech
  labels:
    app.kubernetes.io/name: summarizer-tech-klaviger
data:
  config.yaml: |
    server:
      reverseProxyPort: 8180
      reverseProxyBind: "0.0.0.0"
      forwardProxyPort: 8181
      forwardProxyBind: "127.0.0.1"
      readTimeout: "30s"
      writeTimeout: "30s"
      tls:
        enabled: false
      spiffe:
        enabled: true
        socketPath: "unix:///spiffe-workload-api/spire-agent.sock"

    reverseProxy:
      backend: "http://127.0.0.1:8000"
      verification:
        mode: "jwt"
        jwt:
          jwksUrl: "https://keycloak-spiffe-demo.apps.ocp-beta-test.nerc.mghpcc.org/realms/spiffe-demo/protocol/openid-connect/certs"
          issuer: "https://keycloak-spiffe-demo.apps.ocp-beta-test.nerc.mghpcc.org/realms/spiffe-demo"
          audience: "summarizer-tech-klaviger"
          cacheTtl: "5m"

    forwardProxy:
      defaultMode:
        type: "passthrough"
      hostRules:
      - hostPattern: "^summarizer-"
        mode:
          type: "oauth"
          oauth:
            tokenUrl: "https://keycloak-spiffe-demo.apps.ocp-beta-test.nerc.mghpcc.org/realms/spiffe-demo/protocol/openid-connect/token"
            audience: "summarizer-service-aud"
            scope: "openid"
            cacheTtl: "5m"
      - hostPattern: "^reviewer-"
        mode:
          type: "oauth"
          oauth:
            tokenUrl: "https://keycloak-spiffe-demo.apps.ocp-beta-test.nerc.mghpcc.org/realms/spiffe-demo/protocol/openid-connect/token"
            audience: "reviewer-service-aud"
            scope: "openid"
            cacheTtl: "5m"

    observability:
      metrics:
        enabled: true
        port: 8190
        path: "/metrics"
      tracing:
        enabled: false
      logging:
        level: "debug"
        format: "json"
```

- [ ] **Step 2: Commit**

```bash
git add deploy/openshift/base/configmap.yaml
git commit -s -m "feat: add Klaviger ConfigMap for summarizer-tech

Assisted-By: Claude (Anthropic AI) <noreply@anthropic.com>"
```

---

## Task 4: Create Deployment

**Files:**
- Create: `deploy/openshift/base/deployment.yaml`

- [ ] **Step 1: Create the Deployment manifest**

Two containers: the agent (same image as existing summarizer-tech) and
the Klaviger sidecar. No init containers needed.

Key differences from AuthBridge deployment:
- No proxy-init (no iptables, no privileged SCC)
- No spiffe-helper (Klaviger talks to SPIRE directly)
- No client-registration (Klaviger uses JWT-SVID directly)
- No envoy-proxy (Klaviger is the proxy)
- Agent uses `HTTP_PROXY` env var for outbound traffic

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: summarizer-tech-klaviger
  labels:
    app.kubernetes.io/name: summarizer-tech-klaviger
    app.kubernetes.io/component: agent
    kagenti.io/type: agent
    kagenti.io/framework: Python
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: summarizer-tech-klaviger
      kagenti.io/type: agent
  template:
    metadata:
      labels:
        app.kubernetes.io/name: summarizer-tech-klaviger
        app.kubernetes.io/component: agent
        kagenti.io/type: agent
        kagenti.io/framework: Python
    spec:
      serviceAccountName: summarizer-tech-klaviger
      containers:
      # --- Agent container (same image as existing summarizer-tech) ---
      - name: agent
        image: ghcr.io/redhat-et/zero-trust-agent-demo/kagenti-summarizer:dev
        imagePullPolicy: Always
        ports:
        - name: http
          containerPort: 8000
          protocol: TCP
        env:
        - name: PORT
          value: "8000"
        - name: HOST
          value: "0.0.0.0"
        - name: HTTP_PROXY
          value: "http://127.0.0.1:8181"
        - name: HTTPS_PROXY
          value: "http://127.0.0.1:8181"
        - name: NO_PROXY
          value: "127.0.0.1,localhost"
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "http://otel-collector.kagenti-system.svc.cluster.local:8335"
        - name: UV_CACHE_DIR
          value: "/app/.cache/uv"
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 500m
            memory: 1Gi
        volumeMounts:
        - name: cache
          mountPath: /app/.cache
      # --- Klaviger sidecar ---
      - name: klaviger
        image: ghcr.io/pavelanni/klaviger:latest  # replaced by kustomize
        imagePullPolicy: IfNotPresent
        ports:
        - name: proxy
          containerPort: 8180
          protocol: TCP
        - name: metrics
          containerPort: 8190
          protocol: TCP
        args:
        - "--config"
        - "/etc/klaviger/config.yaml"
        securityContext:
          runAsNonRoot: true
          runAsUser: 1001
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
        resources:
          requests:
            cpu: 100m
            memory: 64Mi
          limits:
            cpu: 500m
            memory: 256Mi
        volumeMounts:
        - name: klaviger-config
          mountPath: /etc/klaviger
          readOnly: true
        - name: spire-agent-socket
          mountPath: /spiffe-workload-api
          readOnly: true
      volumes:
      - name: cache
        emptyDir: {}
      - name: klaviger-config
        configMap:
          name: klaviger-config-summarizer-tech
      - name: spire-agent-socket
        csi:
          driver: csi.spiffe.io
          readOnly: true
```

- [ ] **Step 2: Commit**

```bash
git add deploy/openshift/base/deployment.yaml
git commit -s -m "feat: add Deployment for summarizer-tech with Klaviger sidecar

Assisted-By: Claude (Anthropic AI) <noreply@anthropic.com>"
```

---

## Task 5: Create Kustomization

**Files:**
- Create: `deploy/openshift/base/kustomization.yaml`

- [ ] **Step 1: Create kustomization.yaml**

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: spiffe-demo

resources:
- serviceaccount.yaml
- configmap.yaml
- deployment.yaml
- service.yaml

images:
- name: ghcr.io/pavelanni/klaviger
  newTag: latest
```

- [ ] **Step 2: Commit**

```bash
git add deploy/openshift/base/kustomization.yaml
git commit -s -m "feat: add Kustomization for OpenShift deployment

Assisted-By: Claude (Anthropic AI) <noreply@anthropic.com>"
```

---

## Task 6: Build and push Klaviger image

- [ ] **Step 1: Build the image**

```bash
make podman-build
```

Expected: image built as `ghcr.io/pavelanni/klaviger:<git-sha>`

- [ ] **Step 2: Push to GHCR**

```bash
make podman-push
```

Expected: image pushed successfully

- [ ] **Step 3: Verify image is accessible**

```bash
podman --connection=rhel pull ghcr.io/pavelanni/klaviger:<git-sha>
```

---

## Task 7: Deploy and verify

- [ ] **Step 1: Deploy to OpenShift**

```bash
make deploy-openshift
```

- [ ] **Step 2: Verify pod starts**

```bash
oc get pods -n spiffe-demo -l app.kubernetes.io/name=summarizer-tech-klaviger
```

Expected: 2/2 Running

- [ ] **Step 3: Check Klaviger logs**

```bash
oc logs -n spiffe-demo deployment/summarizer-tech-klaviger -c klaviger
```

Expected: SPIFFE source initialized, reverse/forward proxy started

- [ ] **Step 4: Check agent logs**

```bash
oc logs -n spiffe-demo deployment/summarizer-tech-klaviger -c agent
```

Expected: agent started on port 8000

- [ ] **Step 5: Investigate Keycloak compatibility**

Test whether Keycloak accepts JWT-SVID for token exchange. If not,
determine what authentication method is needed and adjust the Klaviger
config or code accordingly.

---

## Open Issues

1. **Keycloak compatibility**: The existing AuthBridge registers as a
   Keycloak client (client credentials flow). Klaviger uses JWT-SVID
   directly as a bearer token for token exchange. We need to verify
   Keycloak accepts this. If not, options:
   - Pre-register a Keycloak client for `summarizer-tech-klaviger`
   - Add client credentials support to Klaviger's OAuth injector
   - Configure Keycloak to accept SPIFFE JWT-SVIDs

2. **Agent card signing**: The existing deployment uses a
   `sign-agentcard` init container. The Klaviger deployment omits this
   for now. If agent card signing is needed, it can be added as an init
   container independently of Klaviger.

3. **Audience value**: The `reverseProxy.verification.jwt.audience`
   value `summarizer-tech-klaviger` is a placeholder. The correct
   audience depends on how tokens are issued for this deployment.
