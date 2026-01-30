# Baseline Reference Deployment

This directory contains the unsecured baseline deployment used as a reference point for Klaviger examples.

## What This Shows

A simple Kubernetes deployment with:
- Four showme services (alpha, beta, gamma, delta), which will call each other based on the path
- No authentication or authorization

## Why This Matters

When comparing the `simple/` or `oauth-token-exchange/` examples, you can see exactly what changes are needed to add Klaviger security to an existing unsecured deployment.

See:
- `../simple/DELTA.md` - Changes for basic Klaviger integration
- `../oauth-token-exchange/DELTA.md` - Changes for OAuth token exchange

## Quick Deploy (Not Recommended for Production)

```bash
kubectl apply -f deployment.yaml
```
## Testing

Start port forwarding to e.g. the alpha service:
```bash
kubectl port-forward service/alpha 8080:80
```

Then in another terminal:

```bash
curl localhost:8080/beta/gamma/delta
```
