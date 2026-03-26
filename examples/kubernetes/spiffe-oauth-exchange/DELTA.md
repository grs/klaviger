# Changes as compared with oauth token exchange

The SPIRE server and agent must be installed on the cluster.

## Change 1: Mount SPIRE Workload API Socket

```diff
 spec:
   serviceAccountName: alpha
   containers:
   - name: showme
     # ... (application container)

   - name: klaviger
     image: quay.io/gordons/klaviger:latest
     volumeMounts:
     - name: config
       mountPath: /etc/klaviger
+    - name: spire-agent-socket
+      mountPath: /run/spire/sockets
+      readOnly: true
     # ... (other klaviger config)

   volumes:
   - name: config
     configMap:
       name: klaviger-config-alpha
+  - name: spire-agent-socket
+    hostPath:
+      path: /run/spire/sockets
+      type: DirectoryOrCreate
```

## Change 2: ConfigMap Uses SPIFFE Workload API

### spiffe-oauth-exchange (SPIFFE Workload API):

```diff
    server:
      reverseProxyPort: 8180
      reverseProxyBind: "0.0.0.0"
      forwardProxyPort: 8181
      forwardProxyBind: "127.0.0.1"
      readTimeout: "30s"
      writeTimeout: "30s"
      tls:
        enabled: false
+     spiffe:
+       enabled: true
+       socketPath: "unix:///run/spire/agent-sockets/spire-agent.sock"
+       jwtAudience:
+         - "http://keycloak/realms/demo"
```
