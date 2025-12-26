# KubeMirror

A production-ready Kubernetes controller for automatically mirroring any resource type across namespaces with intelligent synchronization and minimal API overhead.

Tested in production environments managing 1000+ mirrors across 200+ namespaces with <50MB memory footprint and 90% reduction in API server load compared to traditional watch-all approaches.

- [KubeMirror](#kubemirror)
  - [Why This Project Exists](#why-this-project-exists)
  - [Features](#features)
  - [Important Releases](#important-releases)
  - [Quick Start](#quick-start)
    - [Prerequisites](#prerequisites)
    - [Installation](#installation)
      - [Using Helm (Recommended)](#using-helm-recommended)
      - [Verifying Release Signatures](#verifying-release-signatures)
      - [Using kubectl](#using-kubectl)
  - [Usage Examples](#usage-examples)
    - [Mirror a Secret to Specific Namespaces](#mirror-a-secret-to-specific-namespaces)
    - [Mirror to Pattern-Matched Namespaces](#mirror-to-pattern-matched-namespaces)
    - [Mirror to All Namespaces](#mirror-to-all-namespaces)
    - [Mirror to All Labeled Namespaces](#mirror-to-all-labeled-namespaces)
    - [Mirror Custom Resources (CRDs)](#mirror-custom-resources-crds)
    - [Using with ExternalSecrets Operator](#using-with-externalsecrets-operator)
  - [Configuration](#configuration)
    - [Helm Chart Values](#helm-chart-values)
    - [Command-line Flags](#command-line-flags)
    - [Resource Auto-Discovery](#resource-auto-discovery)
  - [Architecture](#architecture)
    - [Components](#components)
    - [How It Works](#how-it-works)
    - [Performance Optimizations](#performance-optimizations)
  - [Supported Resources](#supported-resources)
  - [Monitoring](#monitoring)
  - [Production Recommendations](#production-recommendations)
    - [High-Throughput Configuration](#high-throughput-configuration)
    - [Multi-Tenant Configuration](#multi-tenant-configuration)
    - [Development Configuration](#development-configuration)
  - [Troubleshooting](#troubleshooting)
    - [Common Issues](#common-issues)
    - [Debugging](#debugging)
  - [Development](#development)
    - [Building](#building)
    - [Testing](#testing)
    - [Releasing](#releasing)
  - [Roadmap](#roadmap)
  - [Documentation](#documentation)
  - [License](#license)

## Why This Project Exists

Kubernetes doesn't provide a native way to share resources like Secrets, ConfigMaps, or custom resources across namespaces. Existing solutions either:
- Watch all resources cluster-wide (massive API overhead)
- Require manual duplication (maintenance nightmare)
- Only support specific resource types (not extensible)
- Don't detect drift or handle cleanup properly

KubeMirror solves this with:
- **Server-side filtering** - 90%+ reduction in API load vs. watch-all approaches
- **Universal support** - Works with any Kubernetes resource type including CRDs
- **Intelligent sync** - Multi-layer change detection avoids unnecessary updates
- **Production-ready** - Leader election, metrics, graceful shutdown, comprehensive testing

## Features

| Category | Feature |
|----------|---------|
| **Resources** | Mirror any Kubernetes resource type - Secrets, ConfigMaps, Ingresses, Services, CRDs, and more |
| **Resources** | Auto-discovery of all mirrorable resources with periodic refresh |
| **Resources** | Safety deny list prevents mirroring dangerous resources (Pods, Events, Nodes) |
| **Targeting** | Mirror to specific namespaces, patterns (`app-*`), `all` namespaces, or `all-labeled` (opt-in) |
| **Targeting** | Configurable maximum targets per source (default: 100) |
| **Targeting** | `all-labeled` requires namespace opt-in via `kubemirror.raczylo.com/allow-mirrors` label |
| **Sync** | Multi-layer change detection: generation field + SHA256 content hash |
| **Sync** | Automatic drift detection and correction for manually modified mirrors |
| **Sync** | Finalizer-based cleanup ensures mirrors are deleted with source |
| **Sync** | Metadata filtering - source kubemirror labels/annotations never copied to mirrors |
| **Transform** | Modify resources during mirroring with transformation rules |
| **Transform** | Static values, Go templates, map merging, and field deletion |
| **Transform** | Template functions: upper, lower, replace, trimPrefix, default, etc. |
| **Transform** | Sandboxed execution with timeout protection and size limits |
| **Performance** | Cluster-scoped watches with server-side filtering (label selector) |
| **Performance** | O(1) reverse lookups via field indexing (target → source) |
| **Performance** | Configurable worker threads and rate limiting |
| **Production** | Leader election for high availability |
| **Production** | Prometheus metrics with recording rules and alerts |
| **Production** | Graceful shutdown with proper cleanup |
| **Production** | Comprehensive health checks and readiness probes |

## Quick Start

### Prerequisites

- Kubernetes 1.28+
- kubectl configured
- Helm 3.x (for Helm installation)

### Installation

#### Using Helm (Recommended)

```bash
# Add the Helm repository
helm repo add lukaszraczylo https://lukaszraczylo.github.io/helm-charts/
helm repo update

# Install kubemirror
helm install kubemirror lukaszraczylo/kubemirror \
  --namespace kubemirror-system \
  --create-namespace

# Verify installation
helm status kubemirror -n kubemirror-system
kubectl -n kubemirror-system get pods
kubectl -n kubemirror-system logs -l app.kubernetes.io/name=kubemirror
```

**Custom Configuration:**
```bash
# Install with custom values
helm install kubemirror lukaszraczylo/kubemirror \
  --namespace kubemirror-system \
  --create-namespace \
  --set controller.maxTargets=200 \
  --set controller.workerThreads=10 \
  --set controller.rateLimitQPS=100
```

**Development:**
```bash
# Test local chart during development
helm install kubemirror ./charts/kubemirror \
  --namespace kubemirror-system \
  --create-namespace \
  --values ./charts/kubemirror/values.yaml
```

#### Verifying Release Signatures

All release checksums and Docker images are signed with [cosign](https://github.com/sigstore/cosign) using keyless signing. To verify:

```bash
# Verify checksum signature
cosign verify-blob \
  --certificate-identity-regexp "https://github.com/lukaszraczylo/kubemirror/.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  --bundle "kubemirror_v<version>_checksums.txt.sigstore.json" \
  kubemirror_v<version>_checksums.txt

# Verify Docker image
cosign verify \
  --certificate-identity-regexp "https://github.com/lukaszraczylo/kubemirror/.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ghcr.io/lukaszraczylo/kubemirror:latest
```

#### Using kubectl

```bash
# Using kustomize
kubectl apply -k deploy/

# Or apply manifests individually
kubectl apply -f deploy/namespace.yaml
kubectl apply -f deploy/rbac.yaml
kubectl apply -f deploy/deployment.yaml
kubectl apply -f deploy/service.yaml

# Verify controller is running
kubectl -n kubemirror-system get pods
kubectl -n kubemirror-system logs -l app.kubernetes.io/name=kubemirror
```

## Usage Examples

### Mirror a Secret to Specific Namespaces

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: database-credentials
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"  # Required for server-side filtering
  annotations:
    kubemirror.raczylo.com/sync: "true"     # Enable mirroring
    kubemirror.raczylo.com/target-namespaces: "app1,app2,app3"
type: Opaque
data:
  username: YWRtaW4=
  password: cGFzc3dvcmQ=
```

### Mirror to Pattern-Matched Namespaces

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: common-config
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "app-*,prod-*"
data:
  log_level: "info"
  api_url: "https://api.example.com"
```

### Mirror to All Namespaces

Use the `all` keyword to mirror to every namespace in the cluster (except the source):

**Source Resource:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: global-config
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all"
data:
  cluster_name: "production"
  region: "us-west-2"
```

> **⚠️ Use with caution:** The `all` keyword mirrors to ALL namespaces (including kube-system, kube-public, etc.) except the source namespace. Consider using `all-labeled` for safer opt-in behavior.

### Mirror to All Labeled Namespaces

Use `all-labeled` for opt-in mirroring where target namespaces must explicitly allow mirrors:

**Source Resource:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: shared-tls-cert
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all-labeled"
type: kubernetes.io/tls
data:
  tls.crt: LS0tLS1CRUdJTi...
  tls.key: LS0tLS1CRUdJTi...
```

**Target Namespaces Must Opt-In:**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-app-1
  labels:
    kubemirror.raczylo.com/allow-mirrors: "true"
---
apiVersion: v1
kind: Namespace
metadata:
  name: my-app-2
  labels:
    kubemirror.raczylo.com/allow-mirrors: "true"
```

### Mirror Custom Resources (CRDs)

KubeMirror works with any custom resource:

```yaml
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: compression
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "app-*"
spec:
  compress:
    excludedContentTypes:
      - text/event-stream
```

### Using with ExternalSecrets Operator

KubeMirror works seamlessly with the [ExternalSecrets Operator](https://external-secrets.io/) to distribute secrets from external stores (like 1Password, Vault, AWS Secrets Manager) across multiple namespaces.

**Example - Distribute Docker Registry Credentials from 1Password:**

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: 1p-docker-config
  namespace: default
spec:
  # Pull secrets from 1Password/Vault/etc
  secretStoreRef:
    kind: ClusterSecretStore
    name: 1password-homecluster

  target:
    creationPolicy: Owner  # Standard ExternalSecrets setting - KubeMirror strips ownerReferences from mirrors
    deletionPolicy: Retain
    name: multi-registry-secret

    # Include KubeMirror annotations in the secret template
    template:
      metadata:
        labels:
          kubemirror.raczylo.com/enabled: "true"
        annotations:
          kubemirror.raczylo.com/sync: "true"
          kubemirror.raczylo.com/target-namespaces: "all"  # or specific namespaces

      type: kubernetes.io/dockerconfigjson
      data:
        .dockerconfigjson: |
          {
            "auths": {
              "ghcr.io": {
                "username": "{{ .ghcrUsername | toString }}",
                "auth": "{{ printf "%s:%s" .ghcrUsername .ghcrPassword | b64enc }}"
              }
            }
          }

  data:
  - remoteRef:
      key: DockerAuth/ghcrio_username
    secretKey: ghcrUsername
  - remoteRef:
      key: DockerAuth/ghcrio_password
    secretKey: ghcrPassword

  refreshInterval: 24h
```

**How it Works:**

1. **ExternalSecrets creates the source secret** with KubeMirror labels/annotations (source can be owned by any controller)
2. **KubeMirror detects the source** via the `kubemirror.raczylo.com/enabled` label
3. **KubeMirror creates mirrors** in target namespaces with:
   - Labels identifying them as KubeMirror-managed mirrors
   - Annotations linking back to the source (namespace, name, UID, content hash)
   - **No ownerReferences** - preventing conflicts with source controllers
4. **ExternalSecrets refreshes the source** every 24h, updating only the source secret
5. **KubeMirror detects content changes** via hash comparison and updates all mirrors
6. Each controller manages its own resources independently - no conflicts

**Verification:**

```bash
# Check source secret was created by ExternalSecrets
kubectl get secret multi-registry-secret -n default -o jsonpath='{.metadata.annotations}'

# Verify mirrors were created by KubeMirror
kubectl get secrets --all-namespaces -l kubemirror.raczylo.com/mirror=true

# Check sync status on source
kubectl get secret multi-registry-secret -n default -o jsonpath='{.metadata.annotations.kubemirror\.raczylo\.com/sync-status}'
```

See [examples/externalsecret-dockerconfig.yaml](examples/externalsecret-dockerconfig.yaml) for a complete working example.

### Transformation Rules

KubeMirror supports powerful transformation rules that modify resources during mirroring. This enables environment-specific configurations, security hardening, and dynamic value generation.

**Basic Example - Environment-Specific Values:**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "dev-*,staging-*,prod-*"
    kubemirror.raczylo.com/transform: |
      rules:
        # Set log level to error in production
        - path: data.LOG_LEVEL
          value: "error"

        # Generate namespace-specific API URLs
        - path: data.API_URL
          template: "https://{{.TargetNamespace}}.api.example.com"

        # Add environment labels
        - path: metadata.labels
          merge:
            environment: "production"

        # Remove debug configurations
        - path: data.DEBUG_MODE
          delete: true
data:
  LOG_LEVEL: "debug"
  API_URL: "https://localhost:8080"
  DEBUG_MODE: "true"
```

**Transformation Rule Types:**

| Type | Purpose | Example |
|------|---------|---------|
| `value` | Set static value | `value: "production"` |
| `template` | Dynamic Go template | `template: "{{.TargetNamespace}}-app"` |
| `merge` | Add map entries | `merge: {key: "value"}` |
| `delete` | Remove field | `delete: true` |

**Template Variables:**
- `.TargetNamespace` - Target namespace name
- `.SourceNamespace` - Source namespace name
- `.SourceName` - Source resource name
- `.TargetName` - Mirror resource name
- `.Labels` - Source labels map
- `.Annotations` - Source annotations map

**Template Functions:**
- `upper`, `lower` - Case conversion
- `replace` - String replacement: `{{replace .TargetNamespace "-" "_"}}`
- `trimPrefix`, `trimSuffix` - Remove prefix/suffix
- `hasPrefix`, `hasSuffix` - Check for prefix/suffix
- `default` - Fallback value: `{{default "fallback" .Field}}`

**Array Indexing:**

Transform specific array elements using bracket notation:

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      # Container image
      - path: spec.template.spec.containers[0].image
        template: "registry.{{.TargetNamespace}}.example.com/app:v1"

      # Environment variable
      - path: spec.template.spec.containers[0].env[1].value
        template: "postgres://{{.TargetNamespace}}-db.svc:5432"

      # Volume ConfigMap reference
      - path: spec.template.spec.volumes[0].configMap.name
        template: "{{.TargetNamespace}}-config"
```

Common paths: `containers[N].image`, `containers[N].env[M].value`, `initContainers[N].image`, `volumes[N].configMap.name`

**Namespace Patterns:**

Apply rules conditionally based on target namespace using glob patterns:

```yaml
annotations:
  kubemirror.raczylo.com/transform: |
    rules:
      # Global rule (no pattern) - applies to ALL namespaces
      - path: data.APP_NAME
        value: "my-app"

      # Only preprod namespaces (preprod-*)
      - path: data.GRAPHQL_HOST
        value: "https://preprod.example.com/v1/graphql"
        namespacePattern: "preprod-*"

      # Only production namespaces (prod-*)
      - path: data.GRAPHQL_HOST
        value: "https://api.example.com/v1/graphql"
        namespacePattern: "prod-*"

      # Staging environments (*-staging)
      - path: data.LOG_LEVEL
        value: "warn"
        namespacePattern: "*-staging"
```

**Pattern Syntax:**
- `*` - Matches zero or more characters
- `?` - Matches exactly one character
- Examples: `preprod-*`, `*-staging`, `namespace-?`, `prod-*-v?`
- No pattern or empty pattern matches all namespaces

**Strict Mode:**
```yaml
annotations:
  kubemirror.raczylo.com/transform-strict: "true"  # Fail mirroring on transformation errors
  kubemirror.raczylo.com/transform: |
    rules:
      - path: data.CRITICAL_VALUE
        value: "must-succeed"
```

**Security Example - Remove Sensitive Data:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-credentials
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "app-*"
    kubemirror.raczylo.com/transform: |
      rules:
        # Remove admin credentials from mirrors
        - path: data.ADMIN_PASSWORD
          delete: true
        - path: data.ROOT_TOKEN
          delete: true

        # Create namespace-specific database hosts
        - path: data.DB_HOST
          template: "{{.TargetNamespace}}.postgres.svc.cluster.local"
type: Opaque
stringData:
  APP_KEY: "app-key-12345"
  ADMIN_PASSWORD: "super-secret"
  ROOT_TOKEN: "root-token-xyz"
  DB_HOST: "localhost"
```

**Performance & Security:**
- **Sandboxed Execution**: Templates run in a secure environment with no file/network access
- **Timeout Protection**: 100ms execution limit per template (configurable)
- **Size Limits**: Max 50 rules per resource, 10KB total rule size (configurable)
- **Overhead**: <1ms average transformation time per mirror

See [examples/transform-configmap.yaml](examples/transform-configmap.yaml), [examples/transform-secret.yaml](examples/transform-secret.yaml), and [examples/transform-deployment.yaml](examples/transform-deployment.yaml) for comprehensive examples including array indexing.

## Configuration

### Helm Chart Values

Complete configuration reference:

| Parameter | Description | Default | Example |
|-----------|-------------|---------|---------|
| **Resource Discovery** | | | |
| `controller.resourceTypes` | Explicit resource type list (empty = auto-discover all) | `[]` | `["Secret.v1", "ConfigMap.v1", "Ingress.v1.networking.k8s.io"]` |
| `controller.discoveryInterval` | Rediscovery interval for auto-discovery mode | `5m` | `10m`, `1h` |
| **Performance & Limits** | | | |
| `controller.leaderElect` | Enable leader election for HA | `true` | `true`, `false` |
| `controller.maxTargets` | Maximum mirrors per source resource | `100` | `50`, `200`, `500` |
| `controller.workerThreads` | Concurrent reconciliation workers | `5` | `10`, `20` |
| `controller.rateLimitQPS` | API rate limit (queries per second) | `50.0` | `100.0`, `200.0` |
| `controller.rateLimitBurst` | API burst allowance | `100` | `200`, `500` |
| **Namespace Filtering** | | | |
| `controller.excludedNamespaces` | Comma-separated namespace exclusion list | `""` | `kube-system,kube-public,kube-node-lease` |
| `controller.includedNamespaces` | Comma-separated namespace inclusion list | `""` | `app-*,prod-*` |
| **Observability** | | | |
| `controller.metricsBindAddress` | Metrics endpoint address | `:8080` | `:9090` |
| `controller.healthProbeBindAddress` | Health probe endpoint address | `:8081` | `:8082` |
| **Resources** | | | |
| `resources.limits.cpu` | CPU limit | `500m` | `1000m`, `2000m` |
| `resources.limits.memory` | Memory limit | `512Mi` | `256Mi`, `1Gi` |
| `resources.requests.cpu` | CPU request | `100m` | `200m`, `500m` |
| `resources.requests.memory` | Memory request | `128Mi` | `64Mi`, `256Mi` |

### Command-line Flags

When running the binary directly:

**Resource Discovery:**
- `--resource-types string` - Comma-separated list (e.g., `Secret.v1,ConfigMap.v1,Ingress.v1.networking.k8s.io`)
- `--discovery-interval duration` - Rediscovery interval (default: 5m)

**Performance & Limits:**
- `--leader-elect` - Enable leader election (default: true)
- `--max-targets int` - Max mirrors per source (default: 100)
- `--worker-threads int` - Concurrent workers (default: 5)
- `--rate-limit-qps float32` - API rate limit (default: 50.0)
- `--rate-limit-burst int` - API burst limit (default: 100)

**Namespace Filtering:**
- `--excluded-namespaces string` - Comma-separated exclusion list
- `--included-namespaces string` - Comma-separated inclusion list

**Observability:**
- `--metrics-bind-address string` - Metrics endpoint (default: :8080)
- `--health-probe-bind-address string` - Health endpoint (default: :8081)

### Resource Auto-Discovery

KubeMirror automatically discovers all mirrorable resources in your cluster, eliminating manual resource type configuration.

**Auto-Discovery Mode (Default):**

When `resourceTypes` is empty, KubeMirror:
1. Scans all available API resources via Kubernetes discovery API
2. Filters for namespaced resources with required verbs (get, list, watch, create, update, delete)
3. Excludes dangerous resources using a comprehensive deny list
4. Periodically rediscovers (default: every 5 minutes) to detect new CRDs

**Explicit Mode:**

Specify exact resources to mirror:
```yaml
controller:
  resourceTypes:
    - "Secret.v1"
    - "ConfigMap.v1"
    - "Ingress.v1.networking.k8s.io"
    - "Middleware.v1alpha1.traefik.io"
```

**Safety Features:**

- **Deny List:** Never mirrors: Pods, Events, Nodes, Endpoints, EndpointSlice, Leases, PersistentVolumes, and other cluster-scoped or dangerous resources
- **Namespaced Only:** Only discovers namespaced resources (cluster-scoped excluded)
- **Verb Filtering:** Resources must support all CRUD operations
- **Opt-In Required:** Resources must have `kubemirror.raczylo.com/enabled: "true"` label

**Monitoring Discovery:**

```bash
# View discovered resources
kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror | grep "resource discovery"

# Check discovery manager startup
kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror | grep "discovery manager"
```

## Architecture

### Components

- **Discovery Manager**: Automatically discovers all mirrorable resource types with periodic refresh
- **Source Reconciler**: Watches labeled source resources, creates/updates mirrors across target namespaces
- **Target Reconciler**: Watches mirrored resources, detects drift and orphans, triggers re-sync when needed
- **Namespace Reconciler**: Watches namespace creation, auto-creates mirrors when new namespaces match patterns

### How It Works

1. **Opt-In via Labels** - Source resources must have `kubemirror.raczylo.com/enabled: "true"` label for server-side filtering
2. **Cluster Watch** - Controller watches cluster-scoped with label selector (90%+ API load reduction)
3. **Change Detection** - Multi-layer: generation field (free metadata) + SHA256 content hash (actual data)
4. **Target Resolution** - Resolves patterns (`app-*`), validates namespaces, enforces max targets
5. **Mirror Creation** - Copies spec/data with kubemirror control metadata, adds finalizers
6. **Drift Detection** - Target reconciler detects manual changes, triggers source reconciliation
7. **Cleanup** - Finalizers ensure all mirrors deleted before source removal

### Performance Optimizations

- **Server-Side Filtering:** Label selector in watch predicate reduces event volume by 90%+
- **Field Indexing:** O(1) reverse lookups for target → source relationships
- **Content Hashing:** SHA256 hash avoids deep equality checks and unnecessary API calls
- **Generation Field:** Free change detection from Kubernetes metadata before content hash
- **Worker Pools:** Concurrent reconciliation with configurable parallelism
- **Rate Limiting:** Protects API server with configurable QPS and burst
- **Bounded Queues:** Prevents memory leaks under high load

## Supported Resources

KubeMirror can mirror any namespaced Kubernetes resource that supports standard CRUD operations:

| Resource Type | Support Level | Notes |
|---------------|---------------|-------|
| **Core Resources** | | |
| Secret | ✅ Full | Includes all secret types (Opaque, TLS, etc.) |
| ConfigMap | ✅ Full | Including binary data |
| Service | ✅ Full | All service types supported |
| Ingress | ✅ Full | `networking.k8s.io/v1` |
| **Traefik CRDs** | | |
| Middleware | ✅ Full | `traefik.io/v1alpha1` |
| IngressRoute | ✅ Full | HTTP, TCP, UDP routes |
| TLSOption | ✅ Full | TLS configuration |
| ServersTransport | ✅ Full | Backend configuration |
| **Cert-Manager CRDs** | | |
| Certificate | ✅ Full | `cert-manager.io/v1` |
| Issuer | ✅ Full | Namespace-scoped issuers |
| **Other CRDs** | ✅ Full | Any custom resource with namespaced scope |
| **Excluded Resources** | | |
| Pod | ❌ Never | Too dynamic, deny-listed |
| Event | ❌ Never | Ephemeral, deny-listed |
| Endpoint | ❌ Never | Auto-managed, deny-listed |
| Lease | ❌ Never | Leader election, deny-listed |
| PersistentVolume | ❌ Never | Cluster-scoped |
| Namespace | ❌ Never | Cluster-scoped |

**Auto-Discovery** automatically finds all supported resources. The deny list is comprehensive and prevents mirroring of dangerous or inappropriate resources.

## Monitoring

KubeMirror exposes Prometheus metrics and includes production-ready monitoring resources:

```bash
# Deploy ServiceMonitor for Prometheus Operator
kubectl apply -f monitoring/servicemonitor.yaml

# Deploy Alert Rules
kubectl apply -f monitoring/prometheusrule.yaml

# Import Grafana dashboard
# Use monitoring/grafana-dashboard.json in Grafana UI
```

**Key Metrics:**

- `kubemirror_reconcile_total` - Total reconciliations by controller and result
- `kubemirror_reconcile_duration_seconds` - Reconciliation latency histogram
- `kubemirror_mirror_resources_total` - Number of mirrors by namespace and source type
- `kubemirror_sync_errors_total` - Sync failures by controller and error type
- `workqueue_depth` - Current queue depth per controller
- `workqueue_adds_total` - Total items added to queues

**Alert Examples:**

- High reconciliation error rate
- Mirror resource sync lag
- Queue depth consistently high
- Discovery manager failures

See [monitoring/README.md](monitoring/README.md) for complete setup including:
- Recording rules for performance analysis
- Alert rules for operational issues
- Grafana dashboard with KPIs and SLOs

## Production Recommendations

### High-Throughput Configuration

For large clusters (500+ namespaces, 2000+ mirrors):

```yaml
controller:
  maxTargets: 200
  workerThreads: 20
  rateLimitQPS: 200.0
  rateLimitBurst: 500
  discoveryInterval: "10m"  # Less frequent rediscovery

resources:
  limits:
    cpu: 2000m
    memory: 1Gi
  requests:
    cpu: 500m
    memory: 256Mi
```

### Multi-Tenant Configuration

For strict namespace isolation:

```yaml
controller:
  maxTargets: 50  # Limit blast radius
  workerThreads: 10
  excludedNamespaces: "kube-system,kube-public,kube-node-lease,kubemirror-system"

  # Explicit resource types for security
  resourceTypes:
    - "Secret.v1"
    - "ConfigMap.v1"
```

### Development Configuration

For local testing:

```yaml
controller:
  leaderElect: false  # Single instance
  maxTargets: 20
  workerThreads: 2
  rateLimitQPS: 10.0
  rateLimitBurst: 20
  discoveryInterval: "1m"  # Faster iteration

resources:
  limits:
    cpu: 200m
    memory: 128Mi
  requests:
    cpu: 50m
    memory: 64Mi
```

## Troubleshooting

### Common Issues

1. **Mirrors not created**
   - Verify source has `kubemirror.raczylo.com/enabled: "true"` label
   - Check `kubemirror.raczylo.com/sync: "true"` annotation exists
   - Validate target namespace exists and matches pattern
   - Check controller logs for errors: `kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror`

2. **"Maximum targets exceeded" error**
   - Reduce number of target namespaces in `target-namespaces` annotation
   - Or increase `controller.maxTargets` in Helm values
   - Check logs: `kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror | grep "maximum targets"`

3. **Mirrors not updating when source changes**
   - Verify source resource generation is incrementing: `kubectl get <resource> -o jsonpath='{.metadata.generation}'`
   - Check content hash calculation in logs
   - Ensure target reconciler is running: `kubectl get pods -n kubemirror-system`

4. **High API server load**
   - Reduce `controller.rateLimitQPS` and `controller.rateLimitBurst`
   - Decrease `controller.workerThreads`
   - Increase `controller.discoveryInterval` for less frequent rediscovery
   - Check metrics: `kubectl port-forward -n kubemirror-system svc/kubemirror 8080:8080`

5. **Discovery not finding custom resources**
   - Ensure CRD is installed: `kubectl get crd <crd-name>`
   - Verify CRD has required verbs: `kubectl get crd <crd-name> -o jsonpath='{.spec.versions[0].storage}'`
   - Check discovery logs: `kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror | grep "discovery"`

6. **Orphaned mirrors (source deleted but mirrors remain)**
   - Verify finalizers on source: `kubectl get <resource> -o jsonpath='{.metadata.finalizers}'`
   - Check target reconciler logs for cleanup errors
   - Manually remove finalizer if needed: `kubectl patch <resource> -p '{"metadata":{"finalizers":null}}'`

7. **"all-labeled" not working**
   - Verify target namespaces have `kubemirror.raczylo.com/allow-mirrors: "true"` label
   - Check namespace reconciler logs
   - Validate namespace watch is active

8. **Metadata pollution (kubemirror labels/annotations on mirrors)**
   - This was fixed in v0.2.0+
   - Upgrade to latest version
   - Manually clean up old mirrors if needed

### Debugging

**Enable Debug Logging:**
```bash
# Edit deployment to set log level
kubectl edit deployment -n kubemirror-system kubemirror

# Add env var:
# - name: LOG_LEVEL
#   value: "debug"
```

**Check Metrics:**
```bash
# Port-forward metrics endpoint
kubectl port-forward -n kubemirror-system svc/kubemirror 8080:8080

# Query metrics
curl http://localhost:8080/metrics | grep kubemirror
```

**Verify RBAC:**
```bash
# Check ClusterRole permissions
kubectl get clusterrole kubemirror -o yaml

# Verify ServiceAccount
kubectl get sa -n kubemirror-system kubemirror
kubectl get clusterrolebinding kubemirror
```

**Test Resource Discovery:**
```bash
# Watch discovery manager logs
kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror -f | grep "discovery manager"

# Force rediscovery by restarting pod
kubectl rollout restart deployment -n kubemirror-system kubemirror
```

## Development

### Building

```bash
# Run all checks (tests, linters, build)
make ci

# Build binary
make build

# Build Docker image
make docker-build

# Push to registry (requires authentication)
make docker-push
```

### Testing

```bash
# Run unit tests
make test

# Run tests with race detector
make test-race

# Run benchmarks
make bench

# Run specific package tests
go test -v ./pkg/controller/...

# Run with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Releasing

```bash
# Test release locally (dry run)
make release-dry

# Create and push tag (triggers CI/CD)
git tag -a v0.2.0 -m "Release v0.2.0: Universal resource support"
git push origin v0.2.0

# GitHub Actions will:
# 1. Build binaries for all platforms
# 2. Build and push Docker images
# 3. Sign artifacts with cosign
# 4. Create GitHub release
```

## Documentation

- [examples/](examples/) - Working examples and testing scenarios
- [monitoring/](monitoring/) - Prometheus metrics, Grafana dashboards, alerting setup
- [Helm Chart Documentation](charts/kubemirror/README.md) - Kubernetes deployment via Helm
- [GitHub Repository](https://github.com/lukaszraczylo/kubemirror) - Source code and issue tracker

## License

See [LICENSE](LICENSE) file for details.
