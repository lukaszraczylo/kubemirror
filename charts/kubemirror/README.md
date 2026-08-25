# kubemirror Helm chart

Deploys the [kubemirror](https://github.com/lukaszraczylo/kubemirror) controller,
which mirrors Kubernetes resources (Secrets, ConfigMaps, and other types) across
namespaces.

- Chart version: `0.1.0`
- App version: `0.1.0`
- Image: `ghcr.io/lukaszraczylo/kubemirror`

## Install

```bash
helm install kubemirror ./charts/kubemirror -n kubemirror --create-namespace
```

With a values override:

```bash
helm install kubemirror ./charts/kubemirror -n kubemirror --create-namespace \
  -f my-values.yaml
```

## Uninstall

```bash
helm uninstall kubemirror -n kubemirror
```

## What it creates

A single-replica `Deployment`, a `ServiceAccount`, a cluster-scoped `ClusterRole`
+ `ClusterRoleBinding` (the controller watches resources across all namespaces),
and a `Service` exposing the metrics (`8080`) and health (`8081`) ports. The
container runs as non-root (uid `65532`) with a read-only root filesystem and all
Linux capabilities dropped.

## Values

Defaults are taken from [`values.yaml`](values.yaml).

| Key | Default | Description |
| --- | --- | --- |
| `replicaCount` | `1` | Number of controller replicas. |
| `image.repository` | `ghcr.io/lukaszraczylo/kubemirror` | Controller image. |
| `image.tag` | `"0.1.0"` | Image tag (falls back to chart `appVersion` if empty). |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `serviceAccount.create` | `true` | Create a ServiceAccount. |
| `serviceAccount.name` | `""` | Override the generated ServiceAccount name. |
| `controller.metricsBindAddress` | `":8080"` | `--metrics-bind-address`. |
| `controller.healthProbeBindAddress` | `":8081"` | `--health-probe-bind-address`. |
| `controller.leaderElect` | `true` | Enable leader election (`--leader-elect`). |
| `controller.leaderElectionID` | `"kubemirror-controller-leader"` | Lease name (`--leader-election-id`). |
| `controller.resourceTypes` | `[]` | Resource types to mirror (`--resource-types`). Empty enables auto-discovery. |
| `controller.discoveryInterval` | `"5m"` | Auto-discovery interval (`--discovery-interval`). |
| `controller.resyncPeriod` | `"1h"` | Cache resync period, safety net only (`--resync-period`). |
| `controller.maxTargets` | `100` | Max target namespaces per resource (`--max-targets`). |
| `controller.workerThreads` | `5` | Concurrent reconcile workers (`--worker-threads`). |
| `controller.rateLimitQPS` | `50.0` | API QPS limit (`--rate-limit-qps`). |
| `controller.rateLimitBurst` | `100` | API burst limit (`--rate-limit-burst`). |
| `controller.verifySourceFreshness` | `false` | Verify cache against a direct API read (`--verify-source-freshness`). |
| `controller.lazyWatcherInit` | `false` | Only watch resource types actually in use (`--lazy-watcher-init`). Lowers memory; recommended for production. |
| `controller.watcherScanInterval` | `"5m"` | Scan interval for new types in lazy mode (`--watcher-scan-interval`). |
| `controller.excludedNamespaces` | `""` | Comma-separated namespaces never mirrored to (`--excluded-namespaces`). |
| `controller.includedNamespaces` | `""` | Comma-separated namespace patterns to include (`--included-namespaces`). |
| `service.type` | `ClusterIP` | Service type. |
| `service.metricsPort` | `8080` | Metrics port. |
| `service.healthPort` | `8081` | Health-probe port. |
| `resources.requests` | `100m` CPU / `128Mi` | Container requests. |
| `resources.limits` | `500m` CPU / `512Mi` | Container limits. |
| `nodeSelector` / `tolerations` / `affinity` | `{}` / `[]` / `{}` | Standard scheduling controls. |
| `priorityClassName` | `""` | Pod priority class. |

> **Memory tip:** setting `controller.resourceTypes` to the exact types you mirror
> (e.g. `["Secret.v1", "ConfigMap.v1"]`), or enabling `controller.lazyWatcherInit`,
> avoids creating watchers for unused resource types and cuts memory use
> substantially versus full auto-discovery.

## Usage

After install, mark a source resource for mirroring and opt a target namespace
in. See the [project README](../../README.md) and [examples](../../examples/) for
the full annotation/label reference and worked scenarios.
