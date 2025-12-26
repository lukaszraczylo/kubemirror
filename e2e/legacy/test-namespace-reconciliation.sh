#!/bin/bash

# E2E Test: Namespace Reconciliation
# Tests new namespace reconciliation features including CREATE/UPDATE events and orphaned mirror cleanup

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

TEST_NAME="Namespace Reconciliation"

log_info "Starting $TEST_NAME tests"

# Cleanup function for this test
cleanup() {
    log_info "Cleaning up test resources"
    cleanup_resource secret test-ns-recon-pattern-secret default
    cleanup_resource secret test-ns-recon-all-secret default
    cleanup_resource secret test-orphan-cleanup-secret default
    cleanup_resource configmap test-label-change-cm default
    cleanup_namespace e2e-recon-app-1
    cleanup_namespace e2e-recon-app-2
    cleanup_namespace e2e-recon-app-3
    cleanup_namespace e2e-recon-new
    cleanup_namespace e2e-label-test
    cleanup_namespace e2e-no-label
    cleanup_namespace e2e-orphan-1
    cleanup_namespace e2e-orphan-2
    cleanup_namespace e2e-orphan-3
    sleep 5
}

# Trap cleanup on exit
trap cleanup EXIT

# Clean up any existing resources
cleanup

# Wait for cleanup to complete
sleep 3

# Test 1: Create source with pattern, then create matching namespace
log_info "Test 1: Create namespace matching existing pattern"

# Create initial namespaces
kubectl create namespace e2e-recon-app-1
kubectl create namespace e2e-recon-app-2

# Create secret with pattern
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: test-ns-recon-pattern-secret
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "e2e-recon-app-*"
type: Opaque
stringData:
  token: pattern-token-123
EOF

wait_for_resource secret test-ns-recon-pattern-secret e2e-recon-app-1
wait_for_resource secret test-ns-recon-pattern-secret e2e-recon-app-2

assert_resource_exists secret test-ns-recon-pattern-secret e2e-recon-app-1
assert_resource_exists secret test-ns-recon-pattern-secret e2e-recon-app-2

# Now create a new namespace that matches the pattern
log_info "Creating new namespace e2e-recon-app-3 (matches pattern)"
kubectl create namespace e2e-recon-app-3

# Namespace reconciler should automatically create mirror in new namespace
wait_for_resource secret test-ns-recon-pattern-secret e2e-recon-app-3 30

assert_resource_exists secret test-ns-recon-pattern-secret e2e-recon-app-3
assert_data_matches secret test-ns-recon-pattern-secret default test-ns-recon-pattern-secret e2e-recon-app-3 token

# Test 2: Create source with 'all', then create namespace without label
log_info "Test 2: Create namespace without allow-mirrors label (source has 'all')"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: test-ns-recon-all-secret
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all"
type: Opaque
stringData:
  shared-key: all-secret-456
EOF

# Create namespace without label
log_info "Creating namespace e2e-no-label (no allow-mirrors label)"
kubectl create namespace e2e-no-label

sleep 5

# Mirror should NOT be created (namespace not opted-in)
assert_resource_not_exists secret test-ns-recon-all-secret e2e-no-label

# Test 3: Add allow-mirrors label to namespace (should trigger mirror creation)
log_info "Test 3: Add allow-mirrors label to namespace (should create mirror)"

kubectl label namespace e2e-no-label kubemirror.raczylo.com/allow-mirrors=true

# Namespace reconciler should detect label change and create mirror
wait_for_resource secret test-ns-recon-all-secret e2e-no-label 30

assert_resource_exists secret test-ns-recon-all-secret e2e-no-label
assert_data_matches secret test-ns-recon-all-secret default test-ns-recon-all-secret e2e-no-label shared-key

# Test 4: Remove allow-mirrors label from namespace (should trigger cleanup)
log_info "Test 4: Remove allow-mirrors label from namespace (should delete mirror)"

kubectl label namespace e2e-no-label kubemirror.raczylo.com/allow-mirrors-

# Namespace reconciler should detect label removal and cleanup mirror
wait_for_resource_deletion secret test-ns-recon-all-secret e2e-no-label 30

assert_resource_not_exists secret test-ns-recon-all-secret e2e-no-label

# Test 5: Change allow-mirrors label from true to false (should trigger cleanup)
log_info "Test 5: Change allow-mirrors label from true to false"

kubectl create namespace e2e-label-test
kubectl label namespace e2e-label-test kubemirror.raczylo.com/allow-mirrors=true

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-label-change-cm
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all"
data:
  config: "test-data"
EOF

wait_for_resource configmap test-label-change-cm e2e-label-test

assert_resource_exists configmap test-label-change-cm e2e-label-test

# Now change label to false
log_info "Changing label to false"
kubectl label namespace e2e-label-test kubemirror.raczylo.com/allow-mirrors=false --overwrite

# Should trigger cleanup
wait_for_resource_deletion configmap test-label-change-cm e2e-label-test 30

assert_resource_not_exists configmap test-label-change-cm e2e-label-test

# Test 6: Orphaned mirror cleanup when source target pattern changes
log_info "Test 6: Orphaned mirror cleanup (pattern changed to explicit list)"

# Create namespaces
kubectl create namespace e2e-orphan-1
kubectl create namespace e2e-orphan-2
kubectl create namespace e2e-orphan-3

# Create secret with pattern matching all orphan namespaces
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: test-orphan-cleanup-secret
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "e2e-orphan-*"
type: Opaque
stringData:
  data: "orphan-test"
EOF

wait_for_resource secret test-orphan-cleanup-secret e2e-orphan-1
wait_for_resource secret test-orphan-cleanup-secret e2e-orphan-2
wait_for_resource secret test-orphan-cleanup-secret e2e-orphan-3

assert_resource_exists secret test-orphan-cleanup-secret e2e-orphan-1
assert_resource_exists secret test-orphan-cleanup-secret e2e-orphan-2
assert_resource_exists secret test-orphan-cleanup-secret e2e-orphan-3

# Now change pattern to explicit list (only orphan-1 and orphan-2)
log_info "Changing target-namespaces from pattern to explicit list"
kubectl annotate secret test-orphan-cleanup-secret -n default \
    kubemirror.raczylo.com/target-namespaces="e2e-orphan-1,e2e-orphan-2" --overwrite

# Orphan cleanup should remove mirror from e2e-orphan-3
wait_for_resource_deletion secret test-orphan-cleanup-secret e2e-orphan-3 30

assert_resource_exists secret test-orphan-cleanup-secret e2e-orphan-1
assert_resource_exists secret test-orphan-cleanup-secret e2e-orphan-2
assert_resource_not_exists secret test-orphan-cleanup-secret e2e-orphan-3

# Test 7: Change from explicit list to different explicit list
log_info "Test 7: Change explicit list (add e2e-orphan-3, remove e2e-orphan-1)"

kubectl annotate secret test-orphan-cleanup-secret -n default \
    kubemirror.raczylo.com/target-namespaces="e2e-orphan-2,e2e-orphan-3" --overwrite

# Should remove from orphan-1 and create in orphan-3
wait_for_resource secret test-orphan-cleanup-secret e2e-orphan-3 30
wait_for_resource_deletion secret test-orphan-cleanup-secret e2e-orphan-1 30

assert_resource_not_exists secret test-orphan-cleanup-secret e2e-orphan-1
assert_resource_exists secret test-orphan-cleanup-secret e2e-orphan-2
assert_resource_exists secret test-orphan-cleanup-secret e2e-orphan-3

# Test 8: Create namespace with label already set (for 'all' source)
log_info "Test 8: Create namespace with allow-mirrors label already set"

kubectl create namespace e2e-recon-new
kubectl label namespace e2e-recon-new kubemirror.raczylo.com/allow-mirrors=true

# Should automatically get mirrors from sources with 'all'
wait_for_resource secret test-ns-recon-all-secret e2e-recon-new 30
wait_for_resource configmap test-label-change-cm e2e-recon-new 30

assert_resource_exists secret test-ns-recon-all-secret e2e-recon-new
assert_resource_exists configmap test-label-change-cm e2e-recon-new

print_summary
