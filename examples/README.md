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

## Transformation Rules

KubeMirror supports transformation rules that modify resources during mirroring. This enables environment-specific configurations, security hardening, and dynamic value generation.

### Transformation Examples

The repository includes comprehensive transformation examples in `transform-configmap.yaml` and `transform-secret.yaml`. These demonstrate:

1. **Static Value Transformation** - Replace values with constants
2. **Template-Based Transformation** - Generate dynamic values using Go templates
3. **Merge Transformation** - Add labels, annotations, or map entries
4. **Delete Transformation** - Remove sensitive or environment-specific fields
5. **Multi-Rule Transformations** - Combine multiple transformation types

### Quick Start with Transformations

```bash
# Apply transformation examples
kubectl apply -f examples/transform-configmap.yaml
kubectl apply -f examples/transform-secret.yaml

# Verify transformed ConfigMap
kubectl get configmap app-config-template -n namespace-2 -o yaml

# Check that the API_URL was transformed for namespace-2
kubectl get configmap app-config-template -n namespace-2 \
  -o jsonpath='{.data.API_URL}'
# Expected: https://namespace-2.api.example.com
```

### Transformation Rule Types

#### 1. Value Rules (Static Replacement)

Replace a field with a static value:

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      - path: data.LOG_LEVEL
        value: "error"
```

#### 2. Template Rules (Dynamic Generation)

Use Go templates with context variables:

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      - path: data.API_URL
        template: "https://{{.TargetNamespace}}.api.example.com"
```

**Available template variables:**
- `.TargetNamespace` - Namespace where mirror is created
- `.SourceNamespace` - Original resource namespace
- `.SourceName` - Original resource name
- `.TargetName` - Mirror resource name
- `.Labels` - Map of source labels
- `.Annotations` - Map of source annotations

**Template functions:**
- `upper` - Convert to uppercase
- `lower` - Convert to lowercase
- `replace` - String replacement: `{{replace .TargetNamespace "-" "_"}}`
- `trimPrefix` - Remove prefix: `{{trimPrefix .TargetNamespace "namespace-"}}`
- `trimSuffix` - Remove suffix
- `hasPrefix` - Check for prefix
- `hasSuffix` - Check for suffix
- `default` - Provide fallback: `{{default "fallback" .OptionalField}}`

#### 3. Merge Rules (Add Entries)

Merge additional entries into maps (labels, annotations, data):

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      - path: metadata.labels
        merge:
          environment: "production"
          managed-by: "kubemirror"
```

#### 4. Delete Rules (Remove Fields)

Remove sensitive or unnecessary fields:

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      - path: data.DEBUG_MODE
        delete: true
      - path: data.ADMIN_PASSWORD
        delete: true
```

### Array Indexing

Transform specific elements in arrays using bracket notation `[index]`:

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      # Update first container's image
      - path: spec.template.spec.containers[0].image
        template: "registry.{{.TargetNamespace}}.example.com/app:v1"

      # Update second environment variable
      - path: spec.template.spec.containers[0].env[1].value
        template: "postgres://{{.TargetNamespace}}-db.svc.cluster.local:5432"

      # Update nested arrays (container → env vars → value)
      - path: spec.template.spec.containers[0].env[2].value
        value: "production"

      # Update volume ConfigMap reference
      - path: spec.template.spec.volumes[0].configMap.name
        template: "{{.TargetNamespace}}-config"
```

**Common use cases for array indexing:**
- Container images: `spec.template.spec.containers[0].image`
- Environment variables: `spec.template.spec.containers[0].env[N].value`
- Volume mounts: `spec.template.spec.containers[0].volumeMounts[N].mountPath`
- Init containers: `spec.template.spec.initContainers[0].image`
- Volume references: `spec.template.spec.volumes[N].configMap.name`
- Resource limits: `spec.template.spec.containers[0].resources.limits.memory`

**Important notes:**
- Array indexes are zero-based (`[0]` is the first element)
- Index must be within array bounds or transformation will fail
- Use with strict mode to catch out-of-bounds errors
- See `transform-deployment.yaml` for comprehensive Deployment examples

### Namespace Patterns

Apply transformation rules conditionally based on target namespace patterns using glob-style matching.

#### Basic Pattern Matching

Limit a rule to specific namespaces using the `namespacePattern` field:

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      # Only apply to preprod namespaces
      - path: data.GRAPHQL_HOST
        value: "https://preprod.example.com/v1/graphql"
        namespacePattern: "preprod-*"

      # Only apply to production namespaces
      - path: data.GRAPHQL_HOST
        value: "https://api.example.com/v1/graphql"
        namespacePattern: "prod-*"
```

#### Supported Pattern Syntax

- `*` - Matches zero or more characters
- `?` - Matches exactly one character
- No pattern or empty pattern - Matches all namespaces

**Examples:**
- `preprod-*` - Matches `preprod-api`, `preprod-worker`, `preprod-db`
- `*-staging` - Matches `app-staging`, `api-staging`
- `prod-*-v?` - Matches `prod-api-v1`, `prod-db-v2`
- `namespace-?` - Matches `namespace-1`, `namespace-2` (single digit only)

#### Pattern Matching Rules

1. **Rules without patterns** apply to **all namespaces**
2. **Rules with patterns** only apply when the pattern matches the target namespace
3. **Multiple rules with different patterns** can coexist - each is evaluated independently
4. **Pattern matching is case-sensitive**

#### Example: Environment-Specific Configuration

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      # Global rule - applies to all namespaces
      - path: data.APP_NAME
        value: "my-app"

      # Preprod configuration
      - path: data.LOG_LEVEL
        value: "debug"
        namespacePattern: "preprod-*"

      - path: data.DATABASE_URL
        template: "postgres://{{.TargetNamespace}}.db.preprod.example.com:5432"
        namespacePattern: "preprod-*"

      # Production configuration
      - path: data.LOG_LEVEL
        value: "error"
        namespacePattern: "prod-*"

      - path: data.DATABASE_URL
        template: "postgres://{{.TargetNamespace}}.db.prod.example.com:5432"
        namespacePattern: "prod-*"
```

In this example:
- `data.APP_NAME` is set in **all** mirrored namespaces
- `data.LOG_LEVEL` is `debug` in preprod namespaces, `error` in prod namespaces
- `data.DATABASE_URL` is environment-specific based on the namespace pattern

#### Combining Patterns with Templates

Namespace patterns work seamlessly with template rules:

```yaml
rules:
  # Apply different API endpoints based on namespace
  - path: data.API_ENDPOINT
    template: "https://{{.TargetNamespace}}.api.preprod.com"
    namespacePattern: "preprod-*"

  - path: data.API_ENDPOINT
    template: "https://{{.TargetNamespace}}.api.example.com"
    namespacePattern: "prod-*"
```

#### Pattern Verification

```bash
# Verify preprod pattern matching
kubectl get configmap app-config-pattern -n preprod-api \
  -o jsonpath='{.data.GRAPHQL_HOST}'
# Expected: https://preprod.example.com/v1/graphql

kubectl get configmap app-config-pattern -n prod-api \
  -o jsonpath='{.data.GRAPHQL_HOST}'
# Expected: https://api.example.com/v1/graphql

# Verify multi-pattern configuration
kubectl get configmap app-config-multipattern -n namespace-2 \
  -o jsonpath='{.data.ENVIRONMENT}'
# Expected: development

kubectl get configmap app-config-multipattern -n preprod-api \
  -o jsonpath='{.data.ENVIRONMENT}'
# Expected: preproduction
```

### Strict Mode

By default, transformation errors are logged but don't block mirroring. Enable strict mode to fail mirroring on transformation errors:

```yaml
annotations:
  kubemirror.raczylo.com/transform-strict: "true"
  kubemirror.raczylo.com/transform: |
    rules:
      - path: data.CRITICAL_VALUE
        value: "must-succeed"
```

### Transformation Verification

```bash
# Check static value transformation
kubectl get configmap app-config-static -n namespace-2 \
  -o jsonpath='{.data.LOG_LEVEL}'
# Expected: error

# Check template transformation
kubectl get configmap app-config-template -n namespace-3 \
  -o jsonpath='{.data.API_URL}'
# Expected: https://namespace-3.api.example.com

# Check merge transformation (labels should include new entries)
kubectl get configmap app-config-merge -n namespace-2 -o yaml | grep -A 5 labels
# Should include: environment: production, managed-by: kubemirror

# Check delete transformation (fields should be removed)
kubectl get configmap app-config-delete -n namespace-2 -o yaml | grep DEBUG_MODE
# Should return nothing (field deleted)

# Check Secret transformations
kubectl get secret database-credentials -n namespace-2 \
  -o jsonpath='{.data.DB_HOST}' | base64 -d
# Expected: namespace-2.postgres.svc.cluster.local
```

### Common Transformation Patterns

#### Environment-Specific Configuration

```yaml
rules:
  - path: data.LOG_LEVEL
    template: |
      {{- if hasPrefix .TargetNamespace "prod-" -}}
      error
      {{- else if hasPrefix .TargetNamespace "staging-" -}}
      warn
      {{- else -}}
      debug
      {{- end }}
```

#### Namespace-Based Service Discovery

```yaml
rules:
  - path: data.DATABASE_HOST
    template: "postgres.{{.TargetNamespace}}.svc.cluster.local"

  - path: data.REDIS_HOST
    template: "redis.{{.TargetNamespace}}.svc.cluster.local"
```

#### Security Hardening

```yaml
rules:
  # Remove development credentials
  - path: data.DEV_API_KEY
    delete: true

  # Set production encryption
  - path: data.ENCRYPTION_STRENGTH
    value: "AES-256"

  # Add security labels
  - path: metadata.labels
    merge:
      security-tier: "high"
      encrypted: "true"
```

### Troubleshooting Transformations

```bash
# View transformation errors in controller logs
kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror | grep -i transform

# Check if strict mode is blocking mirroring
kubectl get events --all-namespaces | grep -i "transformation.*failed"

# Verify transformation annotation is valid YAML
kubectl get configmap <name> -n <namespace> \
  -o jsonpath='{.metadata.annotations.kubemirror\.raczylo\.com/transform}' | yq eval -
```

### Performance Considerations

- **Rule Limit**: Maximum 50 rules per resource (configurable)
- **Rule Size**: Maximum 10KB of YAML per resource (configurable)
- **Template Timeout**: 100ms per template execution (configurable)
- **Overhead**: <1ms average transformation time per mirror

### Security Notes

1. **Template Sandboxing**: Templates execute in a sandboxed environment with no file, network, or command access
2. **Timeout Protection**: Template execution is strictly time-limited to prevent DoS
3. **Size Limits**: Rules have size limits to prevent resource exhaustion
4. **No Code Execution**: Templates use predefined safe functions only
