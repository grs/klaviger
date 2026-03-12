# SPIFFE OAuth Token Exchange Example

Demonstrates Klaviger with SPIFFE identities and OAuth 2.0 token exchange


## Quick Deploy

I used the non-production-use setup from
https://spiffe.io/docs/latest/spire-helm-charts-hardened-about/installation/#quick-start

```bash
# 1. Deploy SPIRE infrastructure

helm upgrade --install --create-namespace -n spire spire-crds spire-crds \
 --repo https://spiffe.github.io/helm-charts-hardened/

helm upgrade --install -n spire spire spire \
 --repo https://spiffe.github.io/helm-charts-hardened
```

```bash
# 2. Deploy OAuth server
kubectl apply -f oauth-server.yaml

# 3. Deploy application services
kubectl apply -f rbac.yaml
kubectl apply -f configmap.yaml
kubectl apply -f deployment.yaml
```

## Testing

Start port forwarding to the alpha service:
```bash
kubectl port-forward service/alpha 8080:80
```

and also the dummy oauth service:
```bash
kubectl port-forward service/alpha 8888:80
```

Then in another terminal:

```bash
export TOKEN=$(curl -s "http://localhost:8888/test-token?sub=user@example.com&aud=alpha-service" | jq -r .access_token)
curl --header "Authorization: Bearer $TOKEN" localhost:8080/beta/gamma/delta
```

This should show something like the following where the actor is
identified by a SPIFFE identifier:

```
alpha called with path /beta/gamma/delta, subject user@example.com, audience alpha-service, scopes read
beta called with path /gamma/delta, subject user@example.com, audience beta-service, scopes read write, actor spiffe://example.org/ns/default/sa/alpha
gamma called with path /delta, subject user@example.com, audience gamma-service, scopes read write, actor spiffe://example.org/ns/default/sa/beta
delta called with path /, subject user@example.com, audience delta-service, scopes read write, actor spiffe://example.org/ns/default/sa/gamma
```