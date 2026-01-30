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
export TOKEN=$(kubectl create token alpha --duration=1h)
curl --header "Authorization: Bearer $TOKEN" localhost:8080/beta/gamma/delta
```

You can try without the token or with tokens created from
serviceaccounts in other namespaces to verify they cannot access the
services.

## What Changed From Baseline?

See [DELTA.md](./DELTA.md) for an annotated comparison showing exactly what changes from the unsecured baseline deployment.
