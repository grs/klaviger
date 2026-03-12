# Kubernetes Simple Example

Secure service-to-service communication in Kubernetes using Klaviger sidecars with Kubernetes-native token verification.

## What This Demonstrates

- Sidecar proxy pattern
- Automatic token injection from service accounts
- Token verification using Kubernetes SelfSubjectAccessReview
- Zero application code changes

## Quick Deploy

```bash
kubectl apply -f rbac.yaml
kubectl apply -f deployment.yaml
```

## Testing


Start port forwarding to e.g. the alpha service:
```bash
kubectl port-forward service/alpha 8080:80
```

Then in another terminal:

```bash
export TOKEN=$(kubectl create token testing --duration=1h)
curl --header "Authorization: Bearer $TOKEN" localhost:8080/beta/gamma/delta
```

You should see something like:

```
alpha called with path /beta/gamma/delta, subject system:serviceaccount:default:testing, audience https://kubernetes.default.svc.cluster.local
beta called with path /gamma/delta, subject system:serviceaccount:default:alpha, audience https://kubernetes.default.svc.cluster.local
gamma called with path /delta, subject system:serviceaccount:default:beta, audience https://kubernetes.default.svc.cluster.local
delta called with path /, subject system:serviceaccount:default:gamma, audience https://kubernetes.default.svc.cluster.local
```

This shows that the serviceaccount token of each service is used when making requests to another service.

You can try without the token or with tokens created from
serviceaccounts in other namespaces to verify they cannot access the
services.

## What Changed From Baseline?

See [DELTA.md](./DELTA.md) for an annotated comparison showing exactly what changes from the unsecured baseline deployment.
