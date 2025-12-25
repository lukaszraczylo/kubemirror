# Transformation Rules Design

## Overview

Transformation rules allow users to modify resources during mirroring. Rules are specified in the `kubemirror.raczylo.com/transform` annotation as YAML.

## Annotation Format

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "prod-*"
    kubemirror.raczylo.com/transform: |
      rules:
        - path: data.LOG_LEVEL
          value: "error"
        - path: data.API_URL
          template: "https://{{.TargetNamespace}}.api.example.com"
        - path: metadata.labels
          merge:
            environment: "production"
            managed-by: "kubemirror"
        - path: data.DEBUG_MODE
          delete: true
```

## Rule Types

### 1. Static Value (`value`)
Set a field to a static value, replacing existing content.

```yaml
- path: data.LOG_LEVEL
  value: "error"
```

### 2. Template Value (`template`)
Use Go templates with context variables.

**Available template variables:**
- `.TargetNamespace` - Target namespace name
- `.SourceNamespace` - Source namespace name
- `.SourceName` - Source resource name
- `.TargetName` - Target resource name (usually same as source)
- `.Labels` - Map of source labels
- `.Annotations` - Map of source annotations

```yaml
- path: data.API_URL
  template: "https://{{.TargetNamespace}}.api.example.com"

- path: metadata.annotations.namespace-specific
  template: "Mirrored from {{.SourceNamespace}}/{{.SourceName}}"
```

### 3. Map Merge (`merge`)
Merge additional key-value pairs into an existing map. If the map doesn't exist, it's created.

```yaml
- path: metadata.labels
  merge:
    environment: "production"
    tier: "frontend"
```

### 4. Field Deletion (`delete`)
Remove a field from the resource.

```yaml
- path: data.DEBUG_MODE
  delete: true

- path: metadata.annotations.internal-only
  delete: true
```

## Path Syntax

Paths use dot notation to traverse the resource structure:
- `data.KEY` - Data field in ConfigMap/Secret
- `metadata.labels.LABEL_KEY` - Specific label
- `metadata.annotations.ANNOTATION_KEY` - Specific annotation
- `spec.replicas` - Spec field
- `spec.template.spec.containers[0].image` - Array indexing

## Template Functions

Custom template functions available:

- `{{ upper .TargetNamespace }}` - Uppercase
- `{{ lower .TargetNamespace }}` - Lowercase
- `{{ replace .TargetNamespace "-" "_" }}` - String replacement
- `{{ trimPrefix .TargetNamespace "prod-" }}` - Remove prefix
- `{{ trimSuffix .TargetNamespace "-app" }}` - Remove suffix
- `{{ default "fallback" .Labels.optional }}` - Default value

## Security Considerations

1. **Template Sandboxing**: Templates are executed in a sandboxed environment
2. **Path Validation**: Paths must be valid JSONPath expressions
3. **No External Access**: Templates cannot access files, network, or execute commands
4. **Resource Limits**: Maximum template execution time: 100ms
5. **Size Limits**: Maximum transformation rule size: 10KB

## Examples

### Environment-Specific Configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "dev-*,staging-*,prod-*"
    kubemirror.raczylo.com/transform: |
      rules:
        # Set log level based on namespace prefix
        - path: data.LOG_LEVEL
          template: |
            {{- if hasPrefix .TargetNamespace "prod-" -}}
            error
            {{- else if hasPrefix .TargetNamespace "staging-" -}}
            warn
            {{- else -}}
            debug
            {{- end }}

        # Namespace-specific API URL
        - path: data.API_URL
          template: "https://{{.TargetNamespace}}.api.example.com"

        # Add environment label
        - path: metadata.labels
          merge:
            environment: "{{ trimPrefix .TargetNamespace (regexFind `^[^-]+` .TargetNamespace) }}"
```

### Secret with Dynamic Values

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: database-config
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "app-*"
    kubemirror.raczylo.com/transform: |
      rules:
        # Database host varies by namespace
        - path: data.DB_HOST
          template: "{{ .TargetNamespace }}.postgres.svc.cluster.local"

        # Remove admin password in non-admin namespaces
        - path: data.ADMIN_PASSWORD
          delete: true
```

## Error Handling

Transformation errors are non-fatal by default:
- Invalid path: Log warning, skip transformation
- Template error: Log warning, skip transformation
- Type mismatch: Log warning, skip transformation

To make errors fatal (block mirroring):
```yaml
kubemirror.raczylo.com/transform-strict: "true"
```

## Performance

- Rules are parsed once and cached
- Template compilation is cached
- Average overhead: <1ms per mirror creation
- Maximum rules per resource: 50
