# KubeMirror Examples

This directory contains example manifests for testing KubeMirror functionality.

## Overview

The examples create 5 namespaces with various resources to demonstrate different mirroring scenarios:

### Namespace Structure

- **namespace-1**: Source namespace containing:
  - `shared-credentials` Secret → mirrors to ALL namespaces
  - `database-credentials` Secret → mirrors to namespace-3 and namespace-4
  - `local-secret` Secret → NO mirroring (stays local)
  - `app-config` ConfigMap → mirrors to ALL namespaces
  - `nginx-config` ConfigMap → mirrors to namespace-2 and namespace-5

- **namespace-2**: Traefik middleware source namespace containing:
  - `compression` Middleware → mirrors to namespace-4 and namespace-5
  - `rate-limit` Middleware → mirrors to ALL namespaces
  - `headers` Middleware → mirrors to namespace-3 only

- **namespace-3**: Target namespace (receives mirrors)
- **namespace-4**: Target namespace (receives mirrors + Traefik middleware)
- **namespace-5**: Target namespace (receives mirrors + Traefik middleware)

## Prerequisites

1. KubeMirror controller must be deployed and running
2. Traefik CRDs must be installed (for middleware examples)

```bash
# Install official Traefik CRDs (latest)
kubectl apply -f https://raw.githubusercontent.com/traefik/traefik/master/docs/content/reference/dynamic-configuration/kubernetes-crd-definition-v1.yml
```

**Note:** If you don't want to test Traefik middleware mirroring, you can skip the CRD installation and just exclude `traefik-middleware.yaml` from your apply command.

## Quick Start

Apply all examples using kustomize:

```bash
# Apply all examples
kubectl apply -k examples/

# Or apply individually
kubectl apply -f examples/namespaces.yaml
kubectl apply -f examples/source-secret.yaml
kubectl apply -f examples/source-configmap.yaml
kubectl apply -f examples/traefik-middleware.yaml
```

## Verification

### Check Namespaces

```bash
# List all example namespaces
kubectl get namespaces -l app=kubemirror-example

# Verify allow-mirrors label
kubectl get namespaces -l kubemirror.raczylo.com/allow-mirrors=true
```

### Check Mirrored Secrets

```bash
# Check shared-credentials (should exist in all namespaces)
kubectl get secret shared-credentials -n namespace-1
kubectl get secret shared-credentials -n namespace-2
kubectl get secret shared-credentials -n namespace-3
kubectl get secret shared-credentials -n namespace-4
kubectl get secret shared-credentials -n namespace-5

# Check database-credentials (only in namespace-3 and namespace-4)
kubectl get secret database-credentials -n namespace-3
kubectl get secret database-credentials -n namespace-4

# Check local-secret (should ONLY exist in namespace-1)
kubectl get secret local-secret -n namespace-1
kubectl get secret local-secret -n namespace-2 # Should NOT exist
```

### Check Mirrored ConfigMaps

```bash
# Check app-config (should exist in all namespaces)
kubectl get configmap app-config --all-namespaces

# Check nginx-config (only in namespace-2 and namespace-5)
kubectl get configmap nginx-config -n namespace-2
kubectl get configmap nginx-config -n namespace-5
```

### Check Mirrored Traefik Middlewares

```bash
# Check compression middleware (should be in namespace-4 and namespace-5)
kubectl get middleware compression -n namespace-2
kubectl get middleware compression -n namespace-4
kubectl get middleware compression -n namespace-5

# Check rate-limit middleware (should be in all namespaces)
kubectl get middleware rate-limit --all-namespaces

# Check headers middleware (should be in namespace-3)
kubectl get middleware headers -n namespace-3
```

### Check Mirror Ownership

Verify that mirrored resources have the correct ownership labels:

```bash
# Check labels on a mirrored secret
kubectl get secret shared-credentials -n namespace-3 -o yaml | grep -A 5 labels

# Should include:
# kubemirror.raczylo.com/mirrored: "true"
# kubemirror.raczylo.com/source-namespace: namespace-1
# kubemirror.raczylo.com/source-name: shared-credentials
```

## Testing Update Propagation

Test that updates to source resources propagate to mirrors:

```bash
# Update the shared-credentials secret
kubectl patch secret shared-credentials -n namespace-1 \
  --type='json' \
  -p='[{"op": "replace", "path": "/data/password", "value": "'$(echo -n "new-password" | base64)'"}]'

# Wait a few seconds, then verify the change propagated
kubectl get secret shared-credentials -n namespace-3 -o jsonpath='{.data.password}' | base64 -d
# Should output: new-password
```

## Testing Deletion Behavior

Test that deleting source resources deletes mirrors:

```bash
# Delete a source secret
kubectl delete secret database-credentials -n namespace-1

# Wait a few seconds, verify mirrors are also deleted
kubectl get secret database-credentials -n namespace-3 # Should not exist
kubectl get secret database-credentials -n namespace-4 # Should not exist
```

Test that deleting a mirror recreates it (if source still exists):

```bash
# Delete a mirrored resource
kubectl delete secret shared-credentials -n namespace-4

# Wait a few seconds, verify it's recreated
kubectl get secret shared-credentials -n namespace-4 # Should exist again
```

## Cleanup

Remove all examples:

```bash
# Delete all resources
kubectl delete -k examples/

# Or delete individually
kubectl delete -f examples/traefik-middleware.yaml
kubectl delete -f examples/source-configmap.yaml
kubectl delete -f examples/source-secret.yaml
kubectl delete -f examples/namespaces.yaml
```

## Troubleshooting

### View KubeMirror Logs

```bash
# View controller logs
kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror -f
```

### Check Controller Events

```bash
# View events in a specific namespace
kubectl get events -n namespace-3 --sort-by='.lastTimestamp'

# Look for mirror-related events
kubectl get events --all-namespaces | grep -i mirror
```

### Verify Controller is Running

```bash
# Check controller deployment
kubectl get deployment -n kubemirror-system

# Check controller pods
kubectl get pods -n kubemirror-system
```

### Common Issues

1. **Mirrors not created**: Ensure target namespaces have the `kubemirror.raczylo.com/allow-mirrors: "true"` label
2. **Updates not propagating**: Check controller logs for errors or rate limiting
3. **Traefik resources not mirroring**: Ensure Traefik CRDs are installed in the cluster
4. **Permission errors**: Verify the controller has proper RBAC permissions

## Advanced Examples

### Mirror to All Except Specific Namespaces

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: almost-all
  namespace: namespace-1
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all"
    kubemirror.raczylo.com/excluded-namespaces: "namespace-3"
  labels:
    kubemirror.raczylo.com/enabled: "true"
data:
  key: dmFsdWU=  # "value" in base64
```

### Pattern-Based Mirroring

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: namespace-1
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all"
    kubemirror.raczylo.com/namespace-pattern: "app-.*"
  labels:
    kubemirror.raczylo.com/enabled: "true"
data:
  config: "value"
```
