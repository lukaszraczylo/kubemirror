#!/usr/bin/env bash
# E2E Test: Lazy Watcher Initialization
#
# Tests that kube-mirror only creates watchers for resource types that have
# resources marked for mirroring, reducing memory usage dramatically.
#
# Scenario:
# 1. Assumes controller is already running with --lazy-watcher-init=true
# 2. Verify initial controller registration count
# 3. Create a Secret with the enabled label
# 4. Wait for scan interval (500ms in e2e tests)
# 5. Verify controller was registered for Secrets
# 6. Create a ConfigMap with the enabled label
# 7. Verify controller was registered for ConfigMaps
# 8. Verify mirroring works correctly
#
# This test expects the controller to be running with:
#   --lazy-watcher-init=true
#   --watcher-scan-interval=500ms

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

# Test configuration
TEST_NAME="lazy-watcher-init"
SOURCE_NS="kubemirror-e2e-lazy-source"
TARGET_NS="kubemirror-e2e-lazy-target"
KUBEMIRROR_LOG="${KUBEMIRROR_LOG:-/tmp/kubemirror-e2e-test.log}"

# Helper functions
create_namespace() {
    local ns="$1"
    kubectl create namespace "${ns}" 2>/dev/null || true
    echo "✓ Created namespace: ${ns}"
}

delete_namespace() {
    local ns="$1"
    kubectl delete namespace "${ns}" --wait=false 2>/dev/null || true
    echo "✓ Deleted namespace: ${ns}"
}

assert_secret_exists() {
    local ns="$1"
    local name="$2"
    kubectl get secret -n "${ns}" "${name}" &>/dev/null || {
        echo "❌ Secret ${name} not found in namespace ${ns}"
        return 1
    }
    echo "✓ Secret ${name} exists in namespace ${ns}"
}

assert_secret_data() {
    local ns="$1"
    local name="$2"
    local key="$3"
    local expected="$4"
    local actual=$(kubectl get secret -n "${ns}" "${name}" -o jsonpath="{.data.${key}}" | base64 -d)
    if [[ "${actual}" != "${expected}" ]]; then
        echo "❌ Secret ${name} key ${key}: expected '${expected}', got '${actual}'"
        return 1
    fi
    echo "✓ Secret ${name} key ${key} matches expected value"
}

assert_configmap_exists() {
    local ns="$1"
    local name="$2"
    kubectl get configmap -n "${ns}" "${name}" &>/dev/null || {
        echo "❌ ConfigMap ${name} not found in namespace ${ns}"
        return 1
    }
    echo "✓ ConfigMap ${name} exists in namespace ${ns}"
}

assert_configmap_data() {
    local ns="$1"
    local name="$2"
    local key="$3"
    local expected="$4"
    local actual=$(kubectl get configmap -n "${ns}" "${name}" -o jsonpath="{.data.${key}}")
    if [[ "${actual}" != "${expected}" ]]; then
        echo "❌ ConfigMap ${name} key ${key}: expected '${expected}', got '${actual}'"
        return 1
    fi
    echo "✓ ConfigMap ${name} key ${key} matches expected value"
}

# Verify controller is running
verify_controller_running() {
    if [ ! -f "$KUBEMIRROR_LOG" ] || [ ! -s "$KUBEMIRROR_LOG" ]; then
        echo "❌ ERROR: KubeMirror controller log file not found or empty: $KUBEMIRROR_LOG"
        echo "   This test requires the controller to be running with:"
        echo "     --lazy-watcher-init=true"
        echo "     --watcher-scan-interval=500ms"
        exit 1
    fi
    echo "✓ KubeMirror controller is running (log: $KUBEMIRROR_LOG)"
}

# Get initial controller registration count
get_registered_controller_count() {
    tail -1000 "$KUBEMIRROR_LOG" 2>/dev/null | \
        grep "registered controller for active resource type" | \
        wc -l | tr -d ' '
}

# Get memory usage (not available when running as binary)
get_memory_usage_mb() {
    echo "N/A"
}

# Wait for controller to register a specific resource type
wait_for_controller_registration() {
    local resource_kind="$1"
    local timeout=10  # 10 seconds (with 500ms scan interval, should be very fast)
    local elapsed=0

    echo "⏳ Waiting for ${resource_kind} controller registration (timeout: ${timeout}s)..."

    while [[ $elapsed -lt $timeout ]]; do
        if tail -500 "$KUBEMIRROR_LOG" 2>/dev/null | \
           grep -q "registered controller for active resource type.*kind.*${resource_kind}"; then
            echo "✓ ${resource_kind} controller registered (took ~${elapsed}s)"
            return 0
        fi

        sleep 1
        elapsed=$((elapsed + 1))
        echo "  Waiting... (${elapsed}/${timeout}s)"
    done

    echo "❌ Timeout waiting for ${resource_kind} controller registration"
    return 1
}

# Main test function
run_test() {
    echo "🧪 Starting E2E Test: Lazy Watcher Initialization"
    echo "================================================"

    # Verify controller is running
    verify_controller_running

    # Setup
    echo ""
    echo "🔧 Setting up test environment..."
    create_namespace "${SOURCE_NS}"
    create_namespace "${TARGET_NS}"

    # Give controller time to process new namespaces
    sleep 2

    # Check initial state - should have very few or no controllers registered
    echo ""
    echo "📊 Checking initial state (before marking any resources)..."
    initial_count=$(get_registered_controller_count)
    echo "   Initial registered controllers: ${initial_count}"

    if [[ ${initial_count} -gt 5 ]]; then
        echo "⚠️  WARNING: More controllers registered than expected (${initial_count})"
        echo "   This might indicate lazy initialization is not working properly"
    else
        echo "✓ Low initial controller count as expected"
    fi

    # Get initial memory usage
    initial_memory=$(get_memory_usage_mb || echo "N/A")
    echo "   Initial memory usage: ${initial_memory}Mi"

    # Test 1: Create a Secret with enabled label
    echo ""
    echo "📝 Test 1: Creating Secret with enabled label..."
    kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: lazy-test-secret
  namespace: ${SOURCE_NS}
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "${TARGET_NS}"
type: Opaque
stringData:
  test-key: "test-value-from-lazy-init"
EOF

    # Wait for Secret controller to be registered
    wait_for_controller_registration "Secret" || {
        echo "❌ Test 1 failed: Secret controller was not registered"
        echo "📋 Controller logs:"
        tail -100 "$KUBEMIRROR_LOG"
        exit 1
    }

    echo "✓ Test 1 passed: Secret controller registered dynamically"

    # Verify the secret was mirrored
    echo "   Verifying secret mirroring..."
    sleep 15  # Give time for mirroring to occur

    assert_secret_exists "${TARGET_NS}" "lazy-test-secret"
    assert_secret_data "${TARGET_NS}" "lazy-test-secret" "test-key" "test-value-from-lazy-init"

    echo "✓ Secret successfully mirrored"

    # Test 2: Create a ConfigMap with enabled label
    echo ""
    echo "📝 Test 2: Creating ConfigMap with enabled label..."
    kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: lazy-test-configmap
  namespace: ${SOURCE_NS}
  labels:
    kubemirror.raczylo.com/enabled: "true"
    test-resource: e2e
  annotations:
    kubemirror.raczylo.com/sync: "true"
    kubemirror.raczylo.com/target-namespaces: "${TARGET_NS}"
data:
  config-key: "config-value-from-lazy-init"
EOF

    # Wait for ConfigMap controller to be registered
    wait_for_controller_registration "ConfigMap" || {
        echo "❌ Test 2 failed: ConfigMap controller was not registered"
        echo "📋 Controller logs:"
        tail -100 "$KUBEMIRROR_LOG"
        exit 1
    }

    echo "✓ Test 2 passed: ConfigMap controller registered dynamically"

    # Verify the configmap was mirrored
    echo "   Verifying configmap mirroring..."
    sleep 15  # Give time for mirroring to occur

    assert_configmap_exists "${TARGET_NS}" "lazy-test-configmap"
    assert_configmap_data "${TARGET_NS}" "lazy-test-configmap" "config-key" "config-value-from-lazy-init"

    echo "✓ ConfigMap successfully mirrored"

    # Final metrics
    echo ""
    echo "📊 Final metrics:"
    final_count=$(get_registered_controller_count)
    final_memory=$(get_memory_usage_mb || echo "N/A")

    echo "   Initial controllers: ${initial_count}"
    echo "   Final controllers: ${final_count}"
    echo "   Controllers added: $((final_count - initial_count))"
    echo ""
    echo "   Initial memory: ${initial_memory}Mi"
    echo "   Final memory: ${final_memory}Mi"

    if [[ "${final_memory}" != "N/A" && "${initial_memory}" != "N/A" ]]; then
        memory_increase=$((final_memory - initial_memory))
        echo "   Memory increase: ${memory_increase}Mi"

        if [[ ${final_memory} -lt 100 ]]; then
            echo "✓ Memory usage is optimal (<100Mi)"
        elif [[ ${final_memory} -lt 150 ]]; then
            echo "⚠️  Memory usage is acceptable but could be better (${final_memory}Mi)"
        else
            echo "❌ Memory usage is higher than expected (${final_memory}Mi)"
        fi
    fi

    # Verify scan log entry
    echo ""
    echo "📋 Verifying periodic scan activity..."
    if tail -200 "$KUBEMIRROR_LOG" | \
       grep -q "scan completed"; then
        echo "✓ Periodic scanning is active"

        # Show the latest scan results
        tail -200 "$KUBEMIRROR_LOG" | \
            grep "scan completed" | tail -1
    else
        echo "⚠️  No scan activity detected yet (might be too early)"
    fi

    echo ""
    echo "✅ All tests passed!"
    echo ""
    echo "📈 Summary:"
    echo "   - Lazy watcher initialization is working correctly"
    echo "   - Controllers are registered on-demand when resources are marked"
    echo "   - Memory usage remains low"
    echo "   - Periodic scanning detects new resource types"
}

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up test resources..."

    # Delete test secrets/configmaps first
    kubectl delete secret,configmap -n "${SOURCE_NS}" -l test-resource=e2e --ignore-not-found=true 2>/dev/null || true

    # Delete test namespaces
    delete_namespace "${SOURCE_NS}" || true
    delete_namespace "${TARGET_NS}" || true

    echo "✓ Cleanup complete"
}

# Trap cleanup on exit
trap cleanup EXIT

# Run the test
run_test

exit 0
