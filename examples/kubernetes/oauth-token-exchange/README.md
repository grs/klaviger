# OAuth Token Exchange Example

Demonstrates OAuth 2.0 Token Exchange (RFC 8693) for fine-grained, service-specific token scoping with Klaviger.

## What This Demonstrates

- OAuth 2.0 token exchange (RFC 8693) with host-based routing
- Per-service, per-destination access control (different audience & scopes for different destinations)
- JWT verification

## Key Difference From Simple Example

- **Simple**: Direct K8s service account token injection (same token for all destinations)
- **OAuth Exchange**: Exchanges token for scoped access token (different token per destination)

## Quick Deploy

```bash
# 1. Deploy && configure keycloak server
kubectl apply -f keycloak.yaml
./configure-keycloak.sh

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
and also the keycloak service:
```bash
kubectl port-forward service/keycloak 8888:80
```

Then in another terminal:

```bash
 export TOKEN=$(curl -s -X POST "http://localhost:8888/realms/demo/protocol/openid-connect/token" -H "Host: keycloak" -H "Content-Type: application/x-www-form-urlencoded" -d "client_id=demo-client" -d "username=demouser" -d "password=demopass" -d "grant_type=password" | jq -r .access_token)
 curl --header "Authorization: Bearer $TOKEN" localhost:8080/beta/gamma/delta
```

By default, Keycloak does not yet include the actor in the returned token so you should see something like the following:

```
alpha called with path /beta/gamma/delta, subject 76df2bb9-296a-4e27-8f00-37151aba17cf, audience delta,gamma,beta,alpha,account, scopes profile beta delta gamma email alpha
beta called with path /gamma/delta, subject 76df2bb9-296a-4e27-8f00-37151aba17cf, audience beta, scopes profile beta delta gamma email alpha
gamma called with path /delta, subject 76df2bb9-296a-4e27-8f00-37151aba17cf, audience gamma, scopes profile beta delta gamma email alpha
delta called with path /, subject 76df2bb9-296a-4e27-8f00-37151aba17cf, audience delta, scopes profile beta delta gamma email alpha
```

This shows that each service in the sequence of calls receives a
different token, with the audience scoped just to that service, and the original
subject.

However if you use the keycloak SPI from
https://github.com/redhat-et/keycloak-act-claim-spi/, which you can do
using the image in keycloak-act-claims.yaml instead of keycloak.yaml
above, then the actor claim is included and you get something like:

```
alpha called with path /beta/gamma/delta, subject d839780d-b7f1-4768-8604-8f143efb10bd, audience gamma,beta,alpha,delta,account, scopes profile delta email beta alpha gamma
beta called with path /gamma/delta, subject d839780d-b7f1-4768-8604-8f143efb10bd, audience beta, scopes profile delta email beta alpha gamma, actor system:serviceaccount:default:alpha
gamma called with path /delta, subject d839780d-b7f1-4768-8604-8f143efb10bd, audience gamma, scopes profile delta email beta alpha gamma, actor system:serviceaccount:default:beta
delta called with path /, subject d839780d-b7f1-4768-8604-8f143efb10bd, audience delta, scopes profile delta email beta alpha gamma, actor system:serviceaccount:default:gamma
```

This shows the actor in each token.

## What Changed From Baseline?

See [DELTA.md](./DELTA.md) for an annotated comparison showing exactly what changes from the unsecured baseline deployment.

