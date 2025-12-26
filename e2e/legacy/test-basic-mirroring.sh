#!/bin/bash

# E2E Test: Basic Mirroring Functionality
# Tests existing mirror functionality with explicit lists, patterns, and 'all' keyword

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

TEST_NAME="Basic Mirroring"

log_info "Starting $TEST_NAME tests"

# Cleanup function for this test
cleanup() {
    log_info "Cleaning up test resources"
    cleanup_resource secret test-explicit-list-secret default
    cleanup_resource configmap test-explicit-list-cm default
    cleanup_resource secret test-pattern-secret default
    cleanup_resource secret test-all-keyword-secret default
    cleanup_namespace e2e-target-1
    cleanup_namespace e2e-target-2
    cleanup_namespace e2e-target-3
    cleanup_namespace e2e-app-1
    cleanup_namespace e2e-app-2
    cleanup_namespace e2e-app-3
    cleanup_namespace e2e-labeled-ns
    sleep 5
}

# Trap cleanup on exit
trap cleanup EXIT

# Clean up any existing resources
cleanup

# Wait for cleanup to complete
sleep 3

log_info "Creating test namespaces"
kubectl create namespace e2e-target-1
kubectl create namespace e2e-target-2
kubectl create namespace e2e-target-3
kubectl create namespace e2e-app-1
kubectl create namespace e2e-app-2
kubectl create namespace e2e-app-3

# Test 1: Explicit namespace list
log_info "Test 1: Mirror Secret to explicit namespace list"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: test-explicit-list-secret
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "e2e-target-1,e2e-target-2"
type: Opaque
stringData:
  username: admin
  password: secret123
EOF

wait_for_resource secret test-explicit-list-secret e2e-target-1
wait_for_resource secret test-explicit-list-secret e2e-target-2

assert_resource_exists secret test-explicit-list-secret e2e-target-1
assert_resource_exists secret test-explicit-list-secret e2e-target-2
assert_resource_not_exists secret test-explicit-list-secret e2e-target-3

assert_label_exists secret test-explicit-list-secret e2e-target-1 "kubemirror.raczylo.com/managed-by" "kubemirror"
assert_label_exists secret test-explicit-list-secret e2e-target-1 "kubemirror.raczylo.com/mirror" "true"

assert_annotation_exists secret test-explicit-list-secret e2e-target-1 "kubemirror.raczylo.com/source-namespace"
assert_annotation_exists secret test-explicit-list-secret e2e-target-1 "kubemirror.raczylo.com/source-name"

assert_data_matches secret test-explicit-list-secret default test-explicit-list-secret e2e-target-1 username
assert_data_matches secret test-explicit-list-secret default test-explicit-list-secret e2e-target-1 password

# Test 2: ConfigMap with explicit list
log_info "Test 2: Mirror ConfigMap to explicit namespace list"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-explicit-list-cm
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "e2e-target-1,e2e-target-2,e2e-target-3"
data:
  config.yaml: |
    app: myapp
    version: 1.0
EOF

wait_for_resource configmap test-explicit-list-cm e2e-target-1
wait_for_resource configmap test-explicit-list-cm e2e-target-2
wait_for_resource configmap test-explicit-list-cm e2e-target-3

assert_resource_exists configmap test-explicit-list-cm e2e-target-1
assert_resource_exists configmap test-explicit-list-cm e2e-target-2
assert_resource_exists configmap test-explicit-list-cm e2e-target-3

assert_data_matches configmap test-explicit-list-cm default test-explicit-list-cm e2e-target-1 config.yaml

# Test 3: Pattern matching
log_info "Test 3: Mirror Secret with pattern matching (e2e-app-*)"
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: test-pattern-secret
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "e2e-app-*"
type: Opaque
stringData:
  api-key: abc123xyz
EOF

wait_for_resource secret test-pattern-secret e2e-app-1
wait_for_resource secret test-pattern-secret e2e-app-2
wait_for_resource secret test-pattern-secret e2e-app-3

assert_resource_exists secret test-pattern-secret e2e-app-1
assert_resource_exists secret test-pattern-secret e2e-app-2
assert_resource_exists secret test-pattern-secret e2e-app-3
assert_resource_not_exists secret test-pattern-secret e2e-target-1

assert_data_matches secret test-pattern-secret default test-pattern-secret e2e-app-1 api-key

# Test 4: 'all' keyword with labeled namespace
log_info "Test 4: Mirror Secret with 'all' keyword (requires namespace label)"
kubectl create namespace e2e-labeled-ns
kubectl label namespace e2e-labeled-ns kubemirror.raczylo.com/allow-mirrors=true

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: test-all-keyword-secret
  namespace: default
  labels:
    kubemirror.raczylo.com/enabled: "true"
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "all"
type: Opaque
stringData:
  shared-token: token123
EOF

wait_for_resource secret test-all-keyword-secret e2e-labeled-ns

assert_resource_exists secret test-all-keyword-secret e2e-labeled-ns

# Test 5: Source update propagates to targets
log_info "Test 5: Update source and verify targets updated"
kubectl patch secret test-explicit-list-secret -n default --type merge -p '{"stringData":{"password":"newsecret456"}}'

sleep 5

target_password=$(kubectl get secret test-explicit-list-secret -n e2e-target-1 -o jsonpath='{.data.password}' | base64 -d)
if [ "$target_password" = "newsecret456" ]; then
    log_success "Target secret updated with new password"
    ((TESTS_RUN++))
    ((TESTS_PASSED++))
else
    log_fail "Target secret NOT updated (password: $target_password)"
    ((TESTS_RUN++))
    ((TESTS_FAILED++))
fi

# Test 6: Source deletion cascades to targets
log_info "Test 6: Delete source and verify targets deleted"
kubectl delete secret test-explicit-list-secret -n default

wait_for_resource_deletion secret test-explicit-list-secret e2e-target-1
wait_for_resource_deletion secret test-explicit-list-secret e2e-target-2

assert_resource_not_exists secret test-explicit-list-secret e2e-target-1
assert_resource_not_exists secret test-explicit-list-secret e2e-target-2

# Test 7: Target deletion triggers recreation
log_info "Test 7: Delete target and verify it's recreated"
kubectl delete configmap test-explicit-list-cm -n e2e-target-2

wait_for_resource configmap test-explicit-list-cm e2e-target-2 15

assert_resource_exists configmap test-explicit-list-cm e2e-target-2

print_summary
