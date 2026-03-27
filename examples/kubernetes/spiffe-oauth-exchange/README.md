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
 --repo https://spiffe.github.io/helm-charts-hardened \
 --set global.spire.jwtIssuer="https://spire-spiffe-oidc-discovery-provider.spire.svc.cluster.local"
 --set spire-server.defaultJwtSvidTTL="5m"
```

```bash
# 2. Deploy && configure keycloak server
kubectl apply -f keycloak.yaml
./configure-keycloak.sh

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
alpha called with path /beta/gamma/delta, subject 50d0295d-0974-4dd5-8ecc-c3b805f0c82e, audience alpha,delta,gamma,beta,account, scopes email delta profile alpha beta gamma
beta called with path /gamma/delta, subject 50d0295d-0974-4dd5-8ecc-c3b805f0c82e, audience beta, scopes email delta profile alpha beta gamma
gamma called with path /delta, subject 50d0295d-0974-4dd5-8ecc-c3b805f0c82e, audience gamma, scopes email delta profile alpha beta gamma
delta called with path /, subject 50d0295d-0974-4dd5-8ecc-c3b805f0c82e, audience delta, scopes email delta profile alpha beta gamma
```

However if you use the keycloak SPI from
https://github.com/redhat-et/keycloak-act-claim-spi/, which you can do
using the image in keycloak-act-claims.yaml instead of keycloak.yaml
above, then the actor claim is included and you get something like:

```
alpha called with path /beta/gamma/delta, subject 8df125d3-a690-40d1-b37b-d83a43dd3142, audience gamma,beta,delta,alpha,account, scopes alpha delta gamma beta email profile
beta called with path /gamma/delta, subject 8df125d3-a690-40d1-b37b-d83a43dd3142, audience beta, scopes alpha delta gamma beta email profile, actor spiffe://example.org/ns/default/sa/alpha
gamma called with path /delta, subject 8df125d3-a690-40d1-b37b-d83a43dd3142, audience gamma, scopes alpha delta gamma beta email profile, actor spiffe://example.org/ns/default/sa/beta
delta called with path /, subject 8df125d3-a690-40d1-b37b-d83a43dd3142, audience delta, scopes alpha delta gamma beta email profile, actor spiffe://example.org/ns/default/sa/gamma
```