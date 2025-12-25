# KubeMirror

A Kubernetes controller for automatically mirroring any resource type (Secrets, ConfigMaps, Ingresses, CRDs, etc.) across namespaces with intelligent synchronization.

## Features

- **Universal Resource Support**: Mirror any Kubernetes resource type - Secrets, ConfigMaps, Ingresses, Services, CRDs, and more
- **Auto-Discovery**: Automatically discovers all mirrorable resources in the cluster with periodic refresh
- **Efficient Mirroring**: Mirror resources to specific namespaces, pattern-matched namespaces, or all namespaces
- **Content Change Detection**: Multi-layer strategy (generation field + content hash) to avoid unnecessary syncs
- **API-Friendly**: Cluster-scoped watches with server-side filtering reduce API server load by 90%+
- **Production-Ready**: Leader election, health checks, metrics, graceful shutdown
- **Drift Detection**: Automatically fixes manually modified target resources
- **Pattern Matching**: Support glob patterns like `app-*`, `prod-*`
- **Safety Limits**: Configurable maximum targets, namespace opt-in for "all" mirrors
- **Finalizer-based Cleanup**: Ensures all mirrors are deleted when source is removed

## Quick Start

### Prerequisites

- Kubernetes 1.28+
- kubectl configured

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

# Or with custom values
helm install kubemirror lukaszraczylo/kubemirror \
  --namespace kubemirror-system \
  --create-namespace \
  --set controller.maxTargets=200 \
  --set controller.workerThreads=10

# Verify installation
helm status kubemirror -n kubemirror-system
kubectl -n kubemirror-system get pods
```

**Development:** To test the local chart during development:
```bash
helm install kubemirror ./charts/kubemirror -n kubemirror-system --create-namespace
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

### Usage

#### Mirror a Secret to specific namespaces

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"  # Required for filtering
  annotations:
    kubemirror.raczylo.com/sync: "true"     # Enable mirroring
    kubemirror.raczylo.com/target-namespaces: "app1,app2,app3"
type: Opaque
data:
  password: cGFzc3dvcmQ=
```

#### Mirror to pattern-matched namespaces

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
  setting: value
```

#### Mirror to all labeled namespaces

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: shared-tls
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all-labeled"
```

Namespaces must opt-in:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-app
  labels:
    kubemirror.raczylo.com/allow-mirrors: "true"
```

## Architecture

- **Discovery Manager**: Auto-discovers all mirrorable resource types with periodic refresh
- **Source Reconciler**: Watches labeled resources, creates/updates mirrors
- **Target Reconciler**: Watches mirrored resources, detects drift and orphans
- **Namespace Reconciler**: Watches namespace creation, auto-creates mirrors for patterns
- **Content Hash**: SHA256 of actual content (excludes Kubernetes metadata)
- **Field Indexing**: O(1) lookups for reverse references (target → source)
- **Safety Filtering**: Deny list prevents mirroring dangerous resources (Pods, Events, etc.)

## Configuration

### Helm Chart Values

Key configuration options in `values.yaml`:

```yaml
controller:
  # Resource Discovery
  resourceTypes: []          # Explicit list (e.g., ["Secret.v1", "ConfigMap.v1"])
                            # If empty, auto-discovers all mirrorable resources
  discoveryInterval: "5m"    # How often to rediscover resources (auto-discovery mode)

  # Performance & Limits
  leaderElect: true          # Enable leader election for HA
  maxTargets: 100            # Max mirrors per source resource
  workerThreads: 5           # Concurrent reconciliation workers
  rateLimitQPS: 50.0         # API rate limit (queries per second)
  rateLimitBurst: 100        # API burst allowance

  # Namespace Filtering
  excludedNamespaces: ""     # Comma-separated exclusion list
  includedNamespaces: ""     # Comma-separated inclusion list

resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

### Command-line Flags

Key flags when running the binary directly:

**Resource Discovery:**
- `--resource-types`: Comma-separated list of resource types (e.g., `Secret.v1,ConfigMap.v1,Ingress.v1.networking.k8s.io`)
  - If empty, auto-discovers all mirrorable resources
- `--discovery-interval`: Rediscovery interval for auto-discovery mode (default: 5m)

**Performance & Limits:**
- `--leader-elect`: Enable leader election (default: true)
- `--max-targets`: Limit mirrors per source (default: 100)
- `--worker-threads`: Concurrent workers (default: 5)
- `--rate-limit-qps`: API rate limit (default: 50.0)
- `--rate-limit-burst`: API burst limit (default: 100)

**Namespace Filtering:**
- `--excluded-namespaces`: Comma-separated namespace exclusion list
- `--included-namespaces`: Comma-separated namespace inclusion list

## Resource Auto-Discovery

KubeMirror can automatically discover all mirrorable resources in your cluster, eliminating the need to manually specify resource types.

### How it works

**Auto-Discovery Mode (Default):**
When `resourceTypes` is empty (default), KubeMirror:
1. Scans all available API resources in the cluster
2. Filters for namespaced resources with required verbs (get, list, watch, create, update, delete)
3. Excludes dangerous resources (Pods, Events, Nodes, etc.) using a deny list
4. Periodically rediscovers resources (default: every 5 minutes) to detect new CRDs or resource types

**Explicit Mode:**
Specify exactly which resources to mirror:
```yaml
controller:
  resourceTypes:
    - "Secret.v1"
    - "ConfigMap.v1"
    - "Ingress.v1.networking.k8s.io"
    - "Middleware.v1alpha1.traefik.io"
```

### Safety Features

Auto-discovery includes built-in safety:
- **Deny List**: Never mirrors Pods, Events, Nodes, Endpoints, Leases, etc.
- **Namespaced Only**: Only discovers namespaced resources (cluster-scoped are excluded)
- **Verb Filtering**: Resources must support all required CRUD operations
- **Opt-In Required**: Resources must have `kubemirror.raczylo.com/enabled: "true"` label

### Monitoring Discovery

View discovered resources in the logs:
```bash
kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror | grep "resource discovery"
```

## Examples

See the [examples/](examples/) directory for complete working examples including:
- Secrets mirrored to all namespaces
- ConfigMaps mirrored to specific namespaces
- Traefik Middlewares (custom resources) mirroring
- Comprehensive testing scenarios

```bash
# Apply examples
kubectl apply -k examples/

# Watch mirroring in action
kubectl logs -n kubemirror-system -l app.kubernetes.io/name=kubemirror -f
```

## Development

### Using Makefile

```bash
# Run tests
make test

# Run tests with race detector
make test-race

# Run benchmarks
make bench

# Build binary
make build

# Run locally
make run

# Build Docker image
make docker-build

# Run linters
make lint

# Full CI checks
make ci
```

### Manual Commands

```bash
# Run tests
go test ./...

# Run with race detector
go test -race ./...

# Run benchmarks
go test -race -bench=. ./...

# Build binary
go build -o kubemirror ./cmd/kubemirror

# Run locally (against current kubeconfig)
./kubemirror

# Build Docker image
docker build -t ghcr.io/lukaszraczylo/kubemirror:latest .

# Push to registry (requires authentication)
docker push ghcr.io/lukaszraczylo/kubemirror:latest
```

### Release

```bash
# Dry run (test release locally)
make release-dry

# Create release (requires git tag)
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
# GitHub Actions will automatically build and release
```

## Roadmap

- **Phase 1 (MVP)**: Secrets & ConfigMaps, basic mirroring ✅ **Complete**
  - Core reconciliation logic ✅
  - Content hash-based change detection ✅
  - Pattern matching for namespaces ✅
  - Helm chart & deployment manifests ✅
  - Comprehensive test suite ✅
  - CI/CD with GitHub Actions ✅
- **Phase 2**: Production hardening & observability ✅ **Complete**
  - Prometheus metrics dashboard ✅
  - Alert rules for common issues ✅
  - Recording rules for performance monitoring ✅
  - Grafana dashboard with KPIs ✅
  - Performance optimization for large clusters (covered by rate limiting & worker threads) ✅
- **Phase 3**: Universal resource support ✅ **Complete**
  - Auto-discovery of all resource types ✅
  - Support for CRDs, Ingresses, Services, and more ✅
  - Periodic rediscovery for dynamic clusters ✅
  - Safety filtering and deny lists ✅
- **Phase 4**: Advanced features (Future)
  - Cross-namespace reference rewriting
  - kubectl plugin for easy management
  - Advanced transformation rules

## Monitoring

KubeMirror exposes Prometheus metrics and includes production-ready monitoring resources:

```bash
# Deploy ServiceMonitor and Alert Rules
kubectl apply -f monitoring/servicemonitor.yaml
kubectl apply -f monitoring/prometheusrule.yaml

# Import Grafana dashboard from monitoring/grafana-dashboard.json
```

See [monitoring/README.md](monitoring/README.md) for complete observability setup including:
- Prometheus metrics and recording rules
- Alert rules for operational issues
- Grafana dashboard with key performance indicators

## Documentation

- [CLAUDE.md](CLAUDE.md) - Project specification and requirements
- [examples/](examples/) - Working examples and testing scenarios
- [monitoring/](monitoring/) - Prometheus, Grafana, and alerting setup
- [Helm Chart](charts/kubemirror/) - Kubernetes deployment via Helm
- [Project Repository](https://github.com/lukaszraczylo/kubemirror)

## License

See LICENSE file.
