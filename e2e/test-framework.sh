#!/bin/bash

# KubeMirror E2E Test Framework
# Provides reusable test functions for comprehensive scenario testing

source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

# Test scenario execution framework

# Create a source resource (Secret or ConfigMap)
create_source() {
    local resource_type=$1
    local resource_name=$2
    local namespace=${3:-"default"}
    local has_enabled_label=${4:-"false"}
    local has_sync_annotation=${5:-"false"}
    local target_namespaces=${6:-""}
    local data_content=${7:-"test-data-123"}

    log_info "Creating $resource_type/$resource_name in namespace $namespace"
    log_info "  enabled_label=$has_enabled_label, sync_annotation=$has_sync_annotation"
    log_info "  target_namespaces='$target_namespaces'"

    local labels=""
    if [ "$has_enabled_label" = "true" ]; then
        labels='kubemirror.raczylo.com/enabled: "true"'
    fi

    local annotations=""
    if [ "$has_sync_annotation" = "true" ]; then
        annotations='kubemirror.raczylo.com/sync: "true"'
        if [ -n "$target_namespaces" ]; then
            annotations="$annotations
    kubemirror.raczylo.com/target-namespaces: \"$target_namespaces\""
        fi
    fi

    if [ "$resource_type" = "secret" ]; then
        cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: $resource_name
  namespace: $namespace
  ${labels:+labels:}
  ${labels:+  $labels}
  ${annotations:+annotations:}
  ${annotations:+  $annotations}
type: Opaque
stringData:
  testkey: "$data_content"
EOF
    else  # configmap
        cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: $resource_name
  namespace: $namespace
  ${labels:+labels:}
  ${labels:+  $labels}
  ${annotations:+annotations:}
  ${annotations:+  $annotations}
data:
  testkey: "$data_content"
EOF
    fi

    sleep 2
}

# Update source labels
update_source_labels() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    local enabled_label=$4

    log_info "Updating $resource_type/$resource_name labels: enabled=$enabled_label"

    if [ "$enabled_label" = "true" ]; then
        kubectl label "$resource_type" "$resource_name" -n "$namespace" \
            kubemirror.raczylo.com/enabled=true --overwrite
    elif [ "$enabled_label" = "false" ]; then
        kubectl label "$resource_type" "$resource_name" -n "$namespace" \
            kubemirror.raczylo.com/enabled=false --overwrite
    else
        kubectl label "$resource_type" "$resource_name" -n "$namespace" \
            kubemirror.raczylo.com/enabled- 2>/dev/null || true
    fi

    sleep 2
}

# Update source annotations
update_source_annotations() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    local sync_annotation=$4
    local target_namespaces=$5

    log_info "Updating $resource_type/$resource_name annotations: sync=$sync_annotation"
    log_info "  target_namespaces='$target_namespaces'"

    if [ "$sync_annotation" = "true" ]; then
        kubectl annotate "$resource_type" "$resource_name" -n "$namespace" \
            kubemirror.raczylo.com/sync=true --overwrite

        if [ -n "$target_namespaces" ]; then
            kubectl annotate "$resource_type" "$resource_name" -n "$namespace" \
                kubemirror.raczylo.com/target-namespaces="$target_namespaces" --overwrite
        fi
    elif [ "$sync_annotation" = "false" ]; then
        kubectl annotate "$resource_type" "$resource_name" -n "$namespace" \
            kubemirror.raczylo.com/sync=false --overwrite
    else
        kubectl annotate "$resource_type" "$resource_name" -n "$namespace" \
            kubemirror.raczylo.com/sync- 2>/dev/null || true
        kubectl annotate "$resource_type" "$resource_name" -n "$namespace" \
            kubemirror.raczylo.com/target-namespaces- 2>/dev/null || true
    fi

    sleep 2
}

# Update source data content
update_source_data() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    local new_data=$4

    log_info "Updating $resource_type/$resource_name data content"

    if [ "$resource_type" = "secret" ]; then
        kubectl patch secret "$resource_name" -n "$namespace" --type merge \
            -p "{\"stringData\":{\"testkey\":\"$new_data\"}}"
    else
        kubectl patch configmap "$resource_name" -n "$namespace" --type merge \
            -p "{\"data\":{\"testkey\":\"$new_data\"}}"
    fi

    sleep 3
}

# Create namespace with optional label
create_test_namespace() {
    local namespace=$1
    local allow_mirrors_label=${2:-""}

    log_info "Creating namespace $namespace (allow_mirrors=$allow_mirrors_label)"

    kubectl create namespace "$namespace" 2>/dev/null || true

    if [ "$allow_mirrors_label" = "true" ]; then
        kubectl label namespace "$namespace" kubemirror.raczylo.com/allow-mirrors=true --overwrite
    elif [ "$allow_mirrors_label" = "false" ]; then
        kubectl label namespace "$namespace" kubemirror.raczylo.com/allow-mirrors=false --overwrite
    fi

    sleep 1
}

# Update namespace labels
update_namespace_labels() {
    local namespace=$1
    local allow_mirrors_label=$2

    log_info "Updating namespace $namespace labels: allow_mirrors=$allow_mirrors_label"

    if [ "$allow_mirrors_label" = "true" ]; then
        kubectl label namespace "$namespace" kubemirror.raczylo.com/allow-mirrors=true --overwrite
    elif [ "$allow_mirrors_label" = "false" ]; then
        kubectl label namespace "$namespace" kubemirror.raczylo.com/allow-mirrors=false --overwrite
    else
        kubectl label namespace "$namespace" kubemirror.raczylo.com/allow-mirrors- 2>/dev/null || true
    fi

    sleep 2
}

# Verify mirror exists in expected namespaces
verify_mirrors_exist() {
    local resource_type=$1
    local resource_name=$2
    shift 2
    local namespaces=("$@")

    log_info "Verifying mirrors exist in ${#namespaces[@]} namespaces"

    local all_ok=true
    for ns in "${namespaces[@]}"; do
        if wait_for_resource "$resource_type" "$resource_name" "$ns" 30; then
            assert_resource_exists "$resource_type" "$resource_name" "$ns" || all_ok=false
            assert_label_exists "$resource_type" "$resource_name" "$ns" "kubemirror.raczylo.com/managed-by" "kubemirror" || all_ok=false
        else
            log_fail "Mirror not created in $ns within timeout"
            ((TESTS_RUN++))
            ((TESTS_FAILED++))
            all_ok=false
        fi
    done

    $all_ok
}

# Verify mirror does NOT exist in specified namespaces
verify_mirrors_not_exist() {
    local resource_type=$1
    local resource_name=$2
    shift 2
    local namespaces=("$@")

    log_info "Verifying mirrors DO NOT exist in ${#namespaces[@]} namespaces"

    local all_ok=true
    for ns in "${namespaces[@]}"; do
        assert_resource_not_exists "$resource_type" "$resource_name" "$ns" || all_ok=false
    done

    $all_ok
}

# Verify mirror data matches source
verify_mirror_data() {
    local resource_type=$1
    local source_name=$2
    local source_ns=$3
    local target_ns=$4
    local expected_data=$5

    local actual_data
    if [ "$resource_type" = "secret" ]; then
        actual_data=$(kubectl get secret "$source_name" -n "$target_ns" -o jsonpath='{.data.testkey}' 2>/dev/null | base64 -d || echo "")
    else
        actual_data=$(kubectl get configmap "$source_name" -n "$target_ns" -o jsonpath='{.data.testkey}' 2>/dev/null || echo "")
    fi

    ((TESTS_RUN++))
    if [ "$actual_data" = "$expected_data" ]; then
        log_success "Mirror data in $target_ns matches expected: $expected_data"
        ((TESTS_PASSED++))
        return 0
    else
        log_fail "Mirror data in $target_ns does NOT match (expected: $expected_data, actual: $actual_data)"
        ((TESTS_FAILED++))
        return 1
    fi
}

# Verify orphaned mirrors are cleaned up
verify_orphan_cleanup() {
    local resource_type=$1
    local resource_name=$2
    shift 2
    local orphaned_namespaces=("$@")

    log_info "Verifying orphaned mirrors cleaned up in ${#orphaned_namespaces[@]} namespaces"

    local all_ok=true
    for ns in "${orphaned_namespaces[@]}"; do
        if wait_for_resource_deletion "$resource_type" "$resource_name" "$ns" 30; then
            assert_resource_not_exists "$resource_type" "$resource_name" "$ns" || all_ok=false
        else
            log_fail "Orphaned mirror in $ns not deleted within timeout"
            ((TESTS_RUN++))
            ((TESTS_FAILED++))
            all_ok=false
        fi
    done

    $all_ok
}

# Delete namespace
delete_namespace() {
    local namespace=$1

    log_info "Deleting namespace $namespace"
    kubectl delete namespace "$namespace" --ignore-not-found=true &
    sleep 2
}

# Test scenario runner
run_test_scenario() {
    local scenario_name=$1

    echo ""
    echo "======================================"
    echo "Scenario: $scenario_name"
    echo "======================================"
}

# Complete scenario runner
complete_test_scenario() {
    local scenario_name=$1
    local result=$2

    if [ "$result" = "pass" ]; then
        log_success "Scenario '$scenario_name' completed successfully"
    else
        log_fail "Scenario '$scenario_name' failed"
    fi
}
