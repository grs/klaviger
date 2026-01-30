# Changes From Baseline

This document shows exactly what changes when you add Klaviger to a simple Kubernetes deployment.

## Baseline Reference

Unsecured deployment: `../baseline/deployment.yaml`

## Summary of Changes

1. Add Klaviger sidecar container to each pod
2. Add ServiceAccount per deployment
3. Change Service targetPort from 8080 → 8180
4. Add RBAC resources (new file: `rbac.yaml`)
5. Add environment variables to application container

---

## Change 1: Add Klaviger Sidecar

**Deployment spec changes:**

```diff
 spec:
+  serviceAccountName: alpha
   containers:
   - name: showme
     image: quay.io/gordons/showme:latest
     ports:
     - containerPort: 8080
       name: http
       protocol: TCP
     env:
     - name: NAME
       value: "alpha"
     - name: PORT
       value: "8080"
+    - name: BIND_ADDRESS
+      value: "127.0.0.1"
+    - name: HTTP_PROXY
+      value: "http://127.0.0.1:8181"
     securityContext:
       runAsNonRoot: true
       runAsUser: 1001
       allowPrivilegeEscalation: false
       capabilities:
         drop:
         - ALL
     resources:
       requests:
         memory: "32Mi"
         cpu: "50m"
       limits:
         memory: "128Mi"
         cpu: "200m"
+  - name: klaviger
+    image: quay.io/gordons/klaviger:latest
+    imagePullPolicy: Always
+    ports:
+    - containerPort: 8180
+      name: reverse-proxy
+      protocol: TCP
+    - containerPort: 8190
+      name: metrics
+      protocol: TCP
+    args:
+    - "--config"
+    - "/etc/klaviger/config.yaml"
+    securityContext:
+      runAsNonRoot: true
+      runAsUser: 1001
+      allowPrivilegeEscalation: false
+      capabilities:
+        drop:
+        - ALL
+    resources:
+      requests:
+        memory: "64Mi"
+        cpu: "100m"
+      limits:
+        memory: "256Mi"
+        cpu: "500m"
```

**What changed:**
- ✅ Added `serviceAccountName: alpha` - Provides unique identity for authentication
- ✅ Added `BIND_ADDRESS=127.0.0.1` - Restricts app to pod-internal only (can't be reached directly)
- ✅ Added `HTTP_PROXY=http://127.0.0.1:8181` - Routes outbound requests through Klaviger forward proxy
- ✅ Added entire `klaviger` sidecar container - Handles token injection (outbound) and verification (inbound)

---

## Change 2: Update Service TargetPort

```diff
 spec:
   selector:
     app: showme
     instance: alpha
   ports:
   - name: http
     port: 80
-    targetPort: 8080
+    targetPort: 8180
     protocol: TCP
   type: ClusterIP
```

**Why?**
External traffic must go through Klaviger's reverse proxy (port 8180) for token verification before reaching the application.

---

## Change 3: Add RBAC Resources

### New file: `rbac.yaml`

**Why?**
- ServiceAccount per deployment provides unique identity for each service
- Role grants permissions needed for Kubernetes token verification (SelfSubjectAccessReview)
- RoleBinding associates ServiceAccounts with the Role
- Only workloads running under a service account that has access to pods in this namespace can access the services