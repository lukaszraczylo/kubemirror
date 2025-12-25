# KubeMirror Monitoring

This directory contains observability resources for monitoring KubeMirror in production.

## Overview

KubeMirror exposes Prometheus metrics on port 8080 at `/metrics`. The monitoring stack includes:

- **ServiceMonitor**: Prometheus Operator resource for automatic metric scraping
- **PrometheusRule**: Alert rules for common operational issues
- **Grafana Dashboard**: Comprehensive visualization of controller metrics

## Prerequisites

- Prometheus Operator installed in your cluster
- Grafana (optional, for dashboards)

```bash
# Install Prometheus Operator (if not already installed)
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace
```

## Quick Start

### Deploy Monitoring Resources

```bash
# Apply ServiceMonitor and PrometheusRule
kubectl apply -f monitoring/servicemonitor.yaml
kubectl apply -f monitoring/prometheusrule.yaml
```

### Import Grafana Dashboard

1. **Via UI:**
   - Open Grafana
   - Go to Dashboards → Import
   - Upload `grafana-dashboard.json`
   - Select your Prometheus datasource

2. **Via ConfigMap (GitOps):**
   ```bash
   kubectl create configmap kubemirror-dashboard \
     --from-file=dashboard.json=monitoring/grafana-dashboard.json \
     -n monitoring \
     --dry-run=client -o yaml | kubectl apply -f -

   # Label for automatic discovery by Grafana
   kubectl label configmap kubemirror-dashboard \
     grafana_dashboard=1 \
     -n monitoring
   ```

## Available Metrics

### Controller Runtime Metrics

These metrics are provided by the controller-runtime framework:

- `controller_runtime_reconcile_total` - Total reconciliations (by controller, result)
- `controller_runtime_reconcile_errors_total` - Failed reconciliations
- `controller_runtime_reconcile_time_seconds` - Reconciliation duration histogram
- `workqueue_depth` - Current workqueue depth
- `workqueue_adds_total` - Total items added to workqueue
- `workqueue_retries_total` - Workqueue retry count

### Leader Election Metrics

- `leader_election_master_status` - Leader election status (1 = leader, 0 = follower)

### Go Runtime Metrics

- `go_goroutines` - Current goroutine count
- `go_memstats_alloc_bytes` - Allocated memory
- `process_open_fds` - Open file descriptors
- `process_cpu_seconds_total` - CPU time

## Alert Rules

The PrometheusRule defines alerts for:

### Critical Alerts

- **KubeMirrorControllerDown**: Controller pod is not running
  - Severity: `critical`
  - Fires after: 5 minutes

### Warning Alerts

- **KubeMirrorHighReconcileErrors**: High error rate in reconciliation
  - Threshold: >10% error rate
  - Fires after: 10 minutes

- **KubeMirrorReconcileLatencyHigh**: Slow reconciliation loops
  - Threshold: p99 latency > 5 seconds
  - Fires after: 10 minutes

- **KubeMirrorWorkqueueDepthHigh**: Work items piling up
  - Threshold: >100 items in queue
  - Fires after: 15 minutes

- **KubeMirrorLeaderElectionLost**: Controller is not the leader
  - Fires after: 2 minutes

- **KubeMirrorHighFailureRate**: Overall operation failure rate high
  - Threshold: >5% failure rate
  - Fires after: 10 minutes

- **KubeMirrorMemoryHigh**: High memory usage
  - Threshold: >90% of memory limit
  - Fires after: 5 minutes

- **KubeMirrorCPUThrottling**: CPU throttling detected
  - Fires after: 10 minutes

## Recording Rules

Recording rules pre-compute expensive queries for better dashboard performance:

- `kubemirror:reconcile_duration_seconds:p99` - P99 reconciliation latency
- `kubemirror:reconcile_duration_seconds:p95` - P95 reconciliation latency
- `kubemirror:reconcile_duration_seconds:p50` - P50 reconciliation latency
- `kubemirror:reconcile_rate:5m` - Reconciliation rate (5m window)
- `kubemirror:reconcile_errors:rate5m` - Error rate (5m window)
- `kubemirror:workqueue_depth:max` - Max workqueue depth

## Grafana Dashboard

The dashboard includes the following panels:

1. **Controller Status** - Up/down status
2. **Reconciliation Rate** - Operations per second by type and result
3. **Total Workqueue Depth** - Combined queue depth across controllers
4. **Reconciliation Latency** - P99 and P95 latency trends
5. **Workqueue Depth** - Per-controller queue depth
6. **Memory Usage** - Working set vs limits
7. **CPU Usage** - CPU utilization percentage
8. **Error Rate** - Percentage of failed reconciliations
9. **Process Stats** - Goroutines and file descriptors

## Querying Metrics

### Using Prometheus UI

```promql
# Total reconciliation rate
sum(rate(controller_runtime_reconcile_total[5m])) by (controller, result)

# Error rate
sum(rate(controller_runtime_reconcile_errors_total[5m])) by (controller)

# P99 latency
histogram_quantile(0.99,
  sum(rate(controller_runtime_reconcile_time_seconds_bucket[5m])) by (le, controller)
)

# Current workqueue depth
workqueue_depth{name=~"secret|configmap"}
```

### Using kubectl

```bash
# Port-forward to metrics endpoint
kubectl port-forward -n kubemirror-system svc/kubemirror-controller-metrics 8080:8080

# Curl metrics (raw Prometheus format)
curl http://localhost:8080/metrics
```

## Troubleshooting

### ServiceMonitor Not Scraping

Check if Prometheus Operator is configured to discover ServiceMonitors in the kubemirror-system namespace:

```bash
# Check ServiceMonitor status
kubectl get servicemonitor -n kubemirror-system

# Check Prometheus targets
kubectl port-forward -n monitoring svc/prometheus-operated 9090:9090
# Open http://localhost:9090/targets
```

### Alerts Not Firing

Verify PrometheusRule is loaded:

```bash
# Check PrometheusRule
kubectl get prometheusrule -n kubemirror-system

# Check Prometheus rules
kubectl port-forward -n monitoring svc/prometheus-operated 9090:9090
# Open http://localhost:9090/rules
```

### High Memory Usage

If alerts fire for high memory:

1. Check for memory leaks in controller logs
2. Increase memory limits in Helm values:
   ```yaml
   resources:
     limits:
       memory: 1Gi
   ```
3. Reduce worker threads or max targets if necessary

### High Reconciliation Latency

If reconciliation is slow:

1. Check API server latency: `kubectl get --raw /metrics | grep apiserver_request_duration`
2. Increase worker threads in Helm values:
   ```yaml
   controller:
     workerThreads: 10
   ```
3. Review rate limiting settings if hitting API limits

## Integration with Alertmanager

To route KubeMirror alerts to specific channels:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: alertmanager-config
  namespace: monitoring
data:
  alertmanager.yml: |
    route:
      routes:
        - match:
            component: kubemirror
          receiver: kubemirror-team
          continue: true

    receivers:
      - name: kubemirror-team
        slack_configs:
          - channel: '#kubemirror-alerts'
            api_url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'
```

## Best Practices

1. **Set up alerts** - Deploy PrometheusRule to catch issues early
2. **Monitor trends** - Use Grafana dashboard to spot degradation over time
3. **Baseline metrics** - Understand normal behavior during low/high load
4. **Tune resources** - Adjust CPU/memory based on actual usage patterns
5. **Alert fatigue** - Tune alert thresholds to reduce false positives
6. **Retention** - Ensure Prometheus retains metrics for at least 7 days

## Further Reading

- [Prometheus Operator Documentation](https://prometheus-operator.dev/)
- [Grafana Dashboard Best Practices](https://grafana.com/docs/grafana/latest/best-practices/best-practices-for-creating-dashboards/)
- [Controller Runtime Metrics](https://book.kubebuilder.io/reference/metrics.html)
