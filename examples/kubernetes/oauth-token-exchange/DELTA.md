# Changes From Baseline

This document shows exactly what changes when you add Klaviger with OAuth token exchange to a simple Kubernetes deployment.

## Baseline Reference

Unsecured deployment: `../baseline/deployment.yaml`

## Summary of Changes

1. Add Klaviger sidecar container to each pod
2. Add ServiceAccount per deployment
3. Change Service targetPort from 8080 → 8180
4. Add RBAC resources (new file: `rbac.yaml`)
5. Add environment variables to application container
6. **Add ConfigMap per service** with OAuth token exchange configuration (new)
7. **Mount ConfigMap as volume** in Klaviger container (new)
8. **Add OAuth server deployment** (new file: `oauth-server.yaml`)

---

## New Components (vs. Simple Example)

### OAuth Server

Mock token exchange service

- Accepts token exchange requests at `/token`
- Validates incoming Kubernetes service account tokens
- Issues scoped JWT tokens with requested audience and scope
- Provides JWKS endpoint at `/.well-known/jwks.json`

### ConfigMap Per Service

Each service gets a ConfigMap with custom OAuth configuration:

- Defines host-based routing rules
- Specifies which audience/scope to request for each destination
- Allows per-service, per-destination access control

---

## Change (vs Simple Example): Add explicit ConfigMap for each service

The OAuth example adds ConfigMap volume mounting to the Klaviger sidecar:

```diff
   - name: klaviger
     image: quay.io/gordons/klaviger:latest
     imagePullPolicy: Always
     ports:
     - containerPort: 8180
       name: reverse-proxy
       protocol: TCP
     - containerPort: 8190
       name: metrics
       protocol: TCP
     args:
     - "--config"
     - "/etc/klaviger/config.yaml"
+    volumeMounts:
+    - name: config
+      mountPath: /etc/klaviger
     securityContext:
       runAsNonRoot: true
       # ...
+  volumes:
+  - name: config
+    configMap:
+      name: klaviger-config-alpha
```

Each service has a different ConfigMap (alpha, beta, gamma, delta) with service-specific OAuth routing rules

---

## Security Benefits (vs. Simple Example)

What you get for the additional complexity:

✅ **Least privilege**: Each request gets minimal required permissions
✅ **Token scoping**: Different audiences and scopes per destination
✅ **Flexibility**: Change scopes without redeploying applications
✅ **Standard OAuth**: Compatible with real OAuth/OIDC providers
