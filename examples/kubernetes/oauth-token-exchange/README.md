# OAuth Token Exchange Example

Demonstrates OAuth 2.0 Token Exchange (RFC 8693) for fine-grained, service-specific token scoping with Klaviger.

## What This Demonstrates

- OAuth 2.0 token exchange (RFC 8693) with host-based routing
- Per-service, per-destination access control (different audience & scopes for different destinations)
- JWT verification

## Key Difference From Simple Example

- **Simple**: Direct K8s service account token injection (same token for all destinations)
- **OAuth Exchange**: Exchanges token for scoped access token (different token per destination)

Example: When alpha calls beta, it gets a token with `audience: "beta-service"` and `scope: "read write"`. When alpha calls gamma, it gets a different token with `audience: "gamma-service"` and `scope: "read"` (read-only).

## Quick Deploy

```bash
# 1. Deploy OAuth server (mock implementation just for demo)
kubectl apply -f oauth-server.yaml

# 2. Create service configurations
kubectl apply -f configmap.yaml

# 3. Deploy services with Klaviger sidecars
kubectl apply -f rbac.yaml
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

The response should be something like this:

```
alpha called with path /beta/gamma/delta, subject user@example.com, audience alpha-service, scopes read
beta called with path /gamma/delta, subject user@example.com, audience beta-service, scopes read write, actor system:serviceaccount:default:alpha
gamma called with path /delta, subject user@example.com, audience gamma-service, scopes read write, actor system:serviceaccount:default:beta
delta called with path /, subject user@example.com, audience delta-service, scopes read write, actor system:serviceaccount:default:gamma

```

This shows that each service in the sequence of calls receives a
different token, scoped just to that service, with the original
subject and the actor that requested the new token (which is the
service account of the service that requested the token excchange).

Note: the dummy oauth service used here doesn't have any restrictions
on the tokens that can be created as a proper service would. The key
point being demonstrated is the authenticated token exchange.

## What Changed From Baseline?

See [DELTA.md](./DELTA.md) for an annotated comparison showing exactly what changes from the unsecured baseline deployment.

