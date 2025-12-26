#!/bin/bash

# Common utilities for E2E tests

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Test assertion functions
assert_resource_exists() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3

    ((TESTS_RUN++))

    if kubectl get "$resource_type" "$resource_name" -n "$namespace" &>/dev/null; then
        log_success "Resource $resource_type/$resource_name exists in namespace $namespace"
        return 0
    else
        log_fail "Resource $resource_type/$resource_name does NOT exist in namespace $namespace"
        return 1
    fi
}

assert_resource_not_exists() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3

    ((TESTS_RUN++))

    if kubectl get "$resource_type" "$resource_name" -n "$namespace" &>/dev/null; then
        log_fail "Resource $resource_type/$resource_name EXISTS in namespace $namespace (should not exist)"
        return 1
    else
        log_success "Resource $resource_type/$resource_name does not exist in namespace $namespace"
        return 0
    fi
}

assert_annotation_exists() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    local annotation_key=$4

    ((TESTS_RUN++))

    # Escape dots in annotation key for jsonpath (dots need to be escaped, but not slashes)
    local escaped_key="${annotation_key//./\\.}"

    local annotation_value
    annotation_value=$(kubectl get "$resource_type" "$resource_name" -n "$namespace" -o jsonpath="{.metadata.annotations.$escaped_key}" 2>/dev/null || echo "")

    if [ -n "$annotation_value" ]; then
        log_success "Annotation $annotation_key exists on $resource_type/$resource_name in namespace $namespace (value: $annotation_value)"
        return 0
    else
        log_fail "Annotation $annotation_key does NOT exist on $resource_type/$resource_name in namespace $namespace"
        return 1
    fi
}

assert_label_exists() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    local label_key=$4
    local expected_value=$5

    ((TESTS_RUN++))

    # Escape dots in label key for jsonpath (dots need to be escaped, but not slashes)
    local escaped_key="${label_key//./\\.}"

    local actual_value
    actual_value=$(kubectl get "$resource_type" "$resource_name" -n "$namespace" -o jsonpath="{.metadata.labels.$escaped_key}" 2>/dev/null || echo "")

    if [ "$actual_value" = "$expected_value" ]; then
        log_success "Label $label_key=$expected_value on $resource_type/$resource_name in namespace $namespace"
        return 0
    else
        log_fail "Label $label_key has value '$actual_value', expected '$expected_value' on $resource_type/$resource_name in namespace $namespace"
        return 1
    fi
}

assert_data_matches() {
    local resource_type=$1
    local source_name=$2
    local source_ns=$3
    local target_name=$4
    local target_ns=$5
    local data_key=$6

    ((TESTS_RUN++))

    local source_value target_value

    if [ "$resource_type" = "secret" ]; then
        source_value=$(kubectl get secret "$source_name" -n "$source_ns" -o jsonpath="{.data['$data_key']}" 2>/dev/null || echo "")
        target_value=$(kubectl get secret "$target_name" -n "$target_ns" -o jsonpath="{.data['$data_key']}" 2>/dev/null || echo "")
    else
        source_value=$(kubectl get "$resource_type" "$source_name" -n "$source_ns" -o jsonpath="{.data['$data_key']}" 2>/dev/null || echo "")
        target_value=$(kubectl get "$resource_type" "$target_name" -n "$target_ns" -o jsonpath="{.data['$data_key']}" 2>/dev/null || echo "")
    fi

    if [ "$source_value" = "$target_value" ] && [ -n "$source_value" ]; then
        log_success "Data key '$data_key' matches between source and target"
        return 0
    else
        log_fail "Data key '$data_key' does NOT match (source: ${source_value:0:20}..., target: ${target_value:0:20}...)"
        return 1
    fi
}

# Wait for resource to appear
wait_for_resource() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    local timeout=${4:-30}

    log_info "Waiting for $resource_type/$resource_name in namespace $namespace (timeout: ${timeout}s)"

    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        if kubectl get "$resource_type" "$resource_name" -n "$namespace" &>/dev/null; then
            log_info "Resource appeared after ${elapsed}s"
            return 0
        fi
        sleep 1
        ((elapsed++))
    done

    log_warn "Timeout waiting for $resource_type/$resource_name in namespace $namespace"
    return 1
}

# Wait for resource to disappear
wait_for_resource_deletion() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    local timeout=${4:-30}

    log_info "Waiting for $resource_type/$resource_name to be deleted from namespace $namespace (timeout: ${timeout}s)"

    local elapsed=0
    while [ $elapsed -lt $timeout ]; do
        if ! kubectl get "$resource_type" "$resource_name" -n "$namespace" &>/dev/null; then
            log_info "Resource deleted after ${elapsed}s"
            return 0
        fi
        sleep 1
        ((elapsed++))
    done

    log_warn "Timeout waiting for $resource_type/$resource_name deletion in namespace $namespace"
    return 1
}

# Check context is docker-desktop
check_context() {
    local current_context
    current_context=$(kubectl config current-context)

    if [ "$current_context" != "docker-desktop" ]; then
        log_fail "Current context is '$current_context', expected 'docker-desktop'"
        log_info "Please switch context: kubectl config use-context docker-desktop"
        exit 1
    fi

    log_success "Running on docker-desktop context"
}

# Print test summary
print_summary() {
    echo ""
    echo "======================================"
    echo "Test Summary"
    echo "======================================"
    echo -e "Total Tests: ${BLUE}$TESTS_RUN${NC}"
    echo -e "Passed:      ${GREEN}$TESTS_PASSED${NC}"
    echo -e "Failed:      ${RED}$TESTS_FAILED${NC}"
    echo "======================================"

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
        return 0
    else
        echo -e "${RED}Some tests failed!${NC}"
        return 1
    fi
}

# Cleanup function
cleanup_namespace() {
    local namespace=$1
    if kubectl get namespace "$namespace" &>/dev/null; then
        log_info "Cleaning up namespace $namespace"
        kubectl delete namespace "$namespace" --ignore-not-found=true --wait=false &>/dev/null || true
    fi
}

cleanup_resource() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    if kubectl get "$resource_type" "$resource_name" -n "$namespace" &>/dev/null; then
        log_info "Cleaning up $resource_type/$resource_name in namespace $namespace"
        kubectl delete "$resource_type" "$resource_name" -n "$namespace" --ignore-not-found=true &>/dev/null || true
    fi
}
